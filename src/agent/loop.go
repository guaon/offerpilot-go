package agent

import (
	"context"
	"strings"

	appcontext "MyOfferPilot/src/context"
	hooks "MyOfferPilot/src/hook"
	"MyOfferPilot/src/logger"
	"MyOfferPilot/src/memory"
	"MyOfferPilot/src/permission"
	queryengine "MyOfferPilot/src/query-engine"
	"MyOfferPilot/src/session"
	tool "MyOfferPilot/src/tools"
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

type AgentConfig struct {
	DefaultModel    string
	SystemPrompt    string
	MaxIterations   int
	MaxBudgetTokens int
	ContextManager  *appcontext.ContextManager
	SessionManager  *session.SessionManager
	MemoryStore     *memory.MemoryStore
	HookPipeline    *hooks.HookPipeline
	ToolRegistry    *tool.ToolRegistry
	PermissionGate  *permission.PermissionGate
	QueryEngine     *queryengine.QueryEngine
	OnTextDelta     func(text string)
	OnThinkingDelta func(text string)
	OnToolCall      func(name string, input map[string]interface{})
	OnToolResult    func(toolName string, result string)
	OnError         func(err error)
}

type AgentLoop struct {
	config        AgentConfig
	maxIterations int
	usage         UsageStats
}

type UsageStats struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Iterations   int
}

func NewAgentLoop(config AgentConfig) *AgentLoop {
	if config.MaxIterations <= 0 {
		config.MaxIterations = 10
	}
	return &AgentLoop{
		config:        config,
		maxIterations: config.MaxIterations,
		usage: UsageStats{
			InputTokens:  0,
			OutputTokens: 0,
			TotalTokens:  0,
			Iterations:   0,
		},
	}
}

func (al *AgentLoop) GetUsage() UsageStats {
	return al.usage
}

func (al *AgentLoop) Run(ctx context.Context, sessionID string, userMessage string) (string, error) {
	config := al.config

	log := logger.DefaultLogger

	sess, err := config.SessionManager.Get(sessionID)
	if err != nil {
		return "", err
	}

	if sess.State == session.SessionStateIdle {
		if err := config.SessionManager.Transition(sessionID, session.SessionStateActive); err != nil {
			return "", err
		}

	}

	log.Info("run started", map[string]interface{}{
		"sessionID":     sessionID,
		"messageLength": len(userMessage),
		"component":     "AgentLoop",
	})

	userMsg := schema.UserMessage(userMessage)
	if err := config.SessionManager.AddMessage(sessionID, userMsg); err != nil {
		return "", err
	}

	memories := config.MemoryStore.Query(memory.MemoryQuery{SessionID: sessionID, Limit: 5})
	if len(memories) > 0 {
		var memoryText strings.Builder
		for _, m := range memories {
			memoryText.WriteString("- [")
			memoryText.WriteString(string(m.Type))
			memoryText.WriteString("] ")
			memoryText.WriteString(m.Content)
			memoryText.WriteString("\n")
		}
		config.ContextManager.SetLayer(appcontext.ContextWindowKeyMemory, "User context:\n"+memoryText.String(), -1)
	}

	systemPrompt := config.ContextManager.BuildSystemPrompt()

	messages, err := config.SessionManager.GetMessages(sessionID)
	if err != nil {
		return "", nil
	}

	queryMessages := al.convertToQueryEngineMessages(messages)
	var finalText string

	for i := 0; i < al.maxIterations; i++ {
		//在每次迭代开始时快速检查是否收到了取消信号，避免在已取消的上下文中继续执行耗时操作
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		if config.MaxBudgetTokens > 0 && al.usage.TotalTokens >= config.MaxBudgetTokens {
			break
		}

		compressed := config.ContextManager.Compress(queryMessages, 0)
		if compressed.Level != appcontext.CompressionLevelNone {
			queryMessages = compressed.Messages
			messages := al.convertFromQueryEngineMessages(queryMessages)
			if err := config.SessionManager.ReplaceMessages(sessionID, messages); err != nil {
				return "", err
			}
		}

		params := queryengine.QueryParams{
			Messages:  queryMessages,
			Model:     &config.DefaultModel,
			Tools:     al.convertToToolSchemaPtr(config.ToolRegistry.ListSchemas()),
			MaxTokens: intPtr(4096),
			// 0 means omit temperature. Claude models through the configured
			// endpoint reject the deprecated temperature parameter.
			Temperature:     float64Ptr(0),
			SystemPrompt:    &systemPrompt,
			OnTextDelta:     config.OnTextDelta,
			OnThinkingDelta: config.OnThinkingDelta,
		}

		response, err := config.QueryEngine.Query(params)
		if err != nil {
			return "", err
		}

		al.trackUsage(response.Usage)
		al.usage.Iterations = i + 1

		if response.Type == "text" {
			if response.Content != nil {
				finalText = *response.Content
			}
			assistantMsg := schema.AssistantMessage(finalText, nil)
			if err := config.SessionManager.AddMessage(sessionID, assistantMsg); err != nil {
				return "", nil
			}
			break
		}

		if response.Type == "tool_use" && response.ToolCalls != nil && len(*response.ToolCalls) > 0 {
			var toolCalls []schema.ToolCall
			for _, tc := range *response.ToolCalls {
				toolCalls = append(toolCalls, schema.ToolCall{
					ID: tc.ID,
					Function: schema.FunctionCall{
						Name:      tc.Name,
						Arguments: "",
					},
				})

			}
			assistantMsg := schema.AssistantMessage("", toolCalls)
			if err := config.SessionManager.AddMessage(sessionID, assistantMsg); err != nil {
				return "", err
			}

			messages = append(messages, assistantMsg)
			queryMessages = append(queryMessages, *queryengine.NewMessageWithContent(queryengine.MessageRoleAssistant, ""))

			for _, toolCall := range *response.ToolCalls {
				result, err := al.executeTool(ctx, toolCall, sessionID)
				if err != nil {
					return "", err
				}

				toolMsg := schema.ToolMessage(result.Output, toolCall.ID)
				if err := config.SessionManager.AddMessage(sessionID, toolMsg); err != nil {
					return "", err
				}
				messages = append(messages, toolMsg)
				queryMessages = append(queryMessages, *queryengine.NewMessageWithContent(queryengine.MessageRoleTool, result.Output))
			}

		}

	}

	if finalText == "" {
		finalText = "抱歉，我在处理您的请求时达到了最大迭代次数，未能生成完整回复，请尝试简化问题或重新提问"
		assistantMsg := schema.AssistantMessage(finalText, nil)
		if err := config.SessionManager.AddMessage(sessionID, assistantMsg); err != nil {
			return "", err
		}

		log.Warn("max iterations reached", map[string]interface{}{
			"sessionID":  sessionID,
			"iterations": al.usage.Iterations,
		})
	}

	log.Info("run completed", map[string]interface{}{
		"sessionID":   sessionID,
		"iterations":  al.usage.Iterations,
		"totalTokens": al.usage.TotalTokens,
	})

	return finalText, nil

}

func intPtr(value int) *int {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func (al *AgentLoop) trackUsage(usage queryengine.TokenUsage) {
	al.usage.InputTokens += usage.InputTokens
	al.usage.OutputTokens += usage.OutputTokens
	al.usage.TotalTokens += usage.InputTokens + usage.OutputTokens

}

func (al *AgentLoop) executeTool(ctx context.Context, toolCall queryengine.ToolCall, sessionID string) (*tool.ToolResult, error) {
	logger.DefaultLogger.Debug("Executing tool", map[string]interface{}{
		"tool": toolCall.Name,
	})

	toolDef := al.config.ToolRegistry.Get(toolCall.Name)
	if toolDef == nil {
		return nil, nil
	}

	riskLevel := permission.RiskLevel(toolDef.RiskLevel)
	decisionResult := al.config.PermissionGate.Check(toolCall.Name, riskLevel, sessionID)
	if !decisionResult.Allowed {
		return &tool.ToolResult{
			Success: false,
			Output:  "Permission denied",
		}, nil
	}

	inputJSON, _ := json.Marshal(toolCall.Input)

	result, err := al.config.ToolRegistry.ExecuteJSON(ctx, toolCall.Name, string(inputJSON), tool.ToolContext{
		SessionId:   sessionID,
		UserID:      "",
		AgentConfig: al.config,
	})
	if err != nil {
		return &tool.ToolResult{
			Success: false,
			Output:  err.Error(),
		}, nil
	}

	decision := "allowed"
	if decisionResult.RequiredUserConfirm {
		decision = "comfirmed"
	}

	al.config.PermissionGate.RecordAudit(permission.AuditEntry{
		Timestamp: 0,
		SessionID: sessionID,
		ToolName:  toolCall.Name,
		RiskLevel: riskLevel,
		Decision:  decision,
		Input:     toolCall.Input,
	})

	if al.config.OnToolResult != nil {
		al.config.OnToolResult(toolCall.Name, result.Output)
	}

	logger.DefaultLogger.Debug("tool executed", map[string]interface{}{
		"tool":    toolCall.Name,
		"success": result.Success,
	})

	return result, nil

}

func (al *AgentLoop) convertToQueryEngineMessages(messages []*schema.Message) []queryengine.Message {
	result := make([]queryengine.Message, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case schema.System:
			result = append(result, *queryengine.NewMessageWithContent(queryengine.MessageRoleSystem, msg.Content))
		case schema.User:
			result = append(result, *queryengine.NewMessageWithContent(queryengine.MessageRoleUser, msg.Content))
		case schema.Assistant:
			result = append(result, *queryengine.NewMessageWithContent(queryengine.MessageRoleAssistant, msg.Content))
		case schema.Tool:
			result = append(result, *queryengine.NewMessageWithContent(queryengine.MessageRoleTool, msg.Content))
		}
	}

	return result
}

func (al *AgentLoop) convertFromQueryEngineMessages(messages []queryengine.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		var role schema.RoleType
		switch msg.Role {
		case queryengine.MessageRoleSystem:
			role = schema.System
		case queryengine.MessageRoleUser:
			role = schema.User
		case queryengine.MessageRoleAssistant:
			role = schema.Assistant
		case queryengine.MessageRoleTool:
			role = schema.Tool
		default:
			role = schema.User
		}

		result = append(result, &schema.Message{
			Role:    role,
			Content: *msg.Content,
		})

	}
	return result
}

func (al *AgentLoop) convertToToolSchemaPtr(tools []*schema.ToolInfo) *[]queryengine.ToolSchema {
	result := make([]queryengine.ToolSchema, 0, len(tools))
	for _, t := range tools {
		result = append(result, queryengine.ToolSchema{
			Name:        t.Name,
			Description: "",
			Parameters:  make(map[string]interface{}),
		})
	}

	return &result
}
