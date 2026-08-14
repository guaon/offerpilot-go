package agent

import (
	"context"
	"strings"
	"time"

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
	// OnDiagnosisRecord is passed to tools so record_diagnosis can persist scores.
	OnDiagnosisRecord func(sessionID string, dimension string, score int, question string)
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
		return "", err
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

		log.Info("AI iteration started", map[string]interface{}{
			"sessionID":  sessionID,
			"iteration":  i + 1,
			"totalTokens": al.usage.TotalTokens,
			"component":  "AgentLoop",
		})

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
			log.Info("AI requesting tool calls", map[string]interface{}{
				"sessionID":   sessionID,
				"iteration":   i + 1,
				"toolCount":   len(*response.ToolCalls),
				"toolNames":   getToolNames(*response.ToolCalls),
				"component":   "AgentLoop",
			})

			if config.OnToolCall != nil {
				for _, tc := range *response.ToolCalls {
					config.OnToolCall(tc.Name, tc.Input)
				}
			}

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

			// Build query engine message with tool_calls preserved
			qeToolCalls := make([]queryengine.ToolCall, 0, len(toolCalls))
			for _, tc := range toolCalls {
				var input map[string]interface{}
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}
				qeToolCalls = append(qeToolCalls, queryengine.ToolCall{
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			emptyContent := ""
			queryMessages = append(queryMessages, queryengine.Message{
				Role:      queryengine.MessageRoleAssistant,
				Content:   &emptyContent,
				ToolCalls: &qeToolCalls,
			})

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
				id := toolCall.ID
				queryMessages = append(queryMessages, queryengine.Message{
					Role:       queryengine.MessageRoleTool,
					Content:    &result.Output,
					ToolCallID: &id,
				})
			}

		}

	}

	if finalText == "" {
		finalText = "抱歉，我在处理您的请求时未能生成完整回复，请尝试简化问题或重新提问"
		assistantMsg := schema.AssistantMessage(finalText, nil)
		if err := config.SessionManager.AddMessage(sessionID, assistantMsg); err != nil {
			return "", err
		}

		log.Warn("empty response", map[string]interface{}{
			"sessionID":     sessionID,
			"iterations":    al.usage.Iterations,
			"maxIterations": al.maxIterations,
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
	log := logger.DefaultLogger

	log.Info("Tool call started", map[string]interface{}{
		"sessionID":  sessionID,
		"toolName":   toolCall.Name,
		"toolInput":  formatToolInput(toolCall.Input),
		"component":  "AgentLoop",
	})

	toolDef := al.config.ToolRegistry.Get(toolCall.Name)
	if toolDef == nil {
		log.Warn("Tool not found in registry", map[string]interface{}{
			"sessionID": sessionID,
			"toolName":  toolCall.Name,
			"component": "AgentLoop",
		})
		return nil, nil
	}

	riskLevel := permission.RiskLevel(toolDef.RiskLevel)
	decisionResult := al.config.PermissionGate.Check(toolCall.Name, riskLevel, sessionID)
	if !decisionResult.Allowed {
		log.Warn("Tool permission denied", map[string]interface{}{
			"sessionID":  sessionID,
			"toolName":   toolCall.Name,
			"riskLevel":  riskLevel,
			"reason":     decisionResult.Reason,
			"component":  "AgentLoop",
		})
		return &tool.ToolResult{
			Success: false,
			Output:  "Permission denied",
		}, nil
	}

	if decisionResult.RequiredUserConfirm {
		log.Info("Tool requires user confirmation", map[string]interface{}{
			"sessionID": sessionID,
			"toolName":  toolCall.Name,
			"riskLevel": riskLevel,
			"component": "AgentLoop",
		})
	}

	inputJSON, _ := json.Marshal(toolCall.Input)

	startTime := time.Now()
	result, err := al.config.ToolRegistry.ExecuteJSON(ctx, toolCall.Name, string(inputJSON), tool.ToolContext{
		SessionId:   sessionID,
		UserID:      "",
		AgentConfig: al.config,
		OnDiagnosis: al.config.OnDiagnosisRecord,
	})
	elapsed := time.Since(startTime)

	if err != nil {
		log.Error("Tool execution failed", map[string]interface{}{
			"sessionID": sessionID,
			"toolName":  toolCall.Name,
			"error":     err.Error(),
			"duration":  elapsed.String(),
			"component": "AgentLoop",
		})
		return &tool.ToolResult{
			Success: false,
			Output:  err.Error(),
		}, nil
	}

	decision := "allowed"
	if decisionResult.RequiredUserConfirm {
		decision = "confirmed"
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

	log.Info("Tool execution completed", map[string]interface{}{
		"sessionID": sessionID,
		"toolName":  toolCall.Name,
		"success":   result.Success,
		"duration":  elapsed.String(),
		"outputLen": len(result.Output),
		"component": "AgentLoop",
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
			if msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
				toolCalls := make([]queryengine.ToolCall, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					toolCalls = append(toolCalls, queryengine.ToolCall{
						ID:   tc.ID,
						Name: tc.Function.Name,
						Input: nil,
					})
				}
				result = append(result, queryengine.Message{
					Role:      queryengine.MessageRoleAssistant,
					Content:   &msg.Content,
					ToolCalls: &toolCalls,
				})
			} else {
				result = append(result, *queryengine.NewMessageWithContent(queryengine.MessageRoleAssistant, msg.Content))
			}
		case schema.Tool:
			result = append(result, queryengine.Message{
				Role:       queryengine.MessageRoleTool,
				Content:    &msg.Content,
				ToolCallID: &msg.ToolCallID,
			})
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

		content := ""
		if msg.Content != nil {
			content = *msg.Content
		}

		m := &schema.Message{
			Role:    role,
			Content: content,
		}

		if msg.ToolCallID != nil && *msg.ToolCallID != "" {
			m.ToolCallID = *msg.ToolCallID
		}
		if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			toolCalls := make([]schema.ToolCall, 0, len(*msg.ToolCalls))
			for _, tc := range *msg.ToolCalls {
				toolCalls = append(toolCalls, schema.ToolCall{
					ID: tc.ID,
					Function: schema.FunctionCall{
						Name: tc.Name,
					},
				})
			}
			m.ToolCalls = toolCalls
		}

		result = append(result, m)

	}
	return result
}

func (al *AgentLoop) convertToToolSchemaPtr(tools []*schema.ToolInfo) *[]queryengine.ToolSchema {
	result := make([]queryengine.ToolSchema, 0, len(tools))
	for _, t := range tools {
		params := buildJSONSchemaFromParams(t.ParamsOneOf)
		result = append(result, queryengine.ToolSchema{
			Name:        t.Name,
			Description: t.Desc,
			Parameters:  params,
		})
	}

	return &result
}

func buildJSONSchemaFromParams(p *schema.ParamsOneOf) map[string]interface{} {
	if p == nil {
		return nil
	}

	jsonSchema, err := p.ToJSONSchema()
	if err != nil || jsonSchema == nil {
		return nil
	}

	// Marshal to JSON then unmarshal to map
	data, err := json.Marshal(jsonSchema)
	if err != nil {
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	return result
}

func getToolNames(toolCalls []queryengine.ToolCall) []string {
	names := make([]string, len(toolCalls))
	for i, tc := range toolCalls {
		names[i] = tc.Name
	}
	return names
}

func formatToolInput(input map[string]interface{}) string {
	if input == nil {
		return "{}"
	}
	b, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	s := string(b)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}
