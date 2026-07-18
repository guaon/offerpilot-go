package subagent

import (
	"MyOfferPilot/src/logger"
	queryengine "MyOfferPilot/src/query-engine"
	tool "MyOfferPilot/src/tools"

	"time"
)

var RolePrompts = map[SubAgentRole]string{
	SubAgentRoleDiagnostician: `你是诊断子专家agent。你的任务时对面试回答进行深度分析，找出知识盲点、逻辑断层和表达不足。输出结构化的诊断结果`,
	SubAgentRoleInterviewer:   `你是模拟面试官子agent。根据候选人的回答，生成针对性的追问。追问应该逐步深入，检验真实理解深度。`,
	SubAgentRoleResearcher:    `你是知识研究子agent。负责从知识库中查找相关参考资料，整合多个来源的信息，为诊断提供依据`,
	SubAgentRoleReporter:      `你是报告生成子agent。负责将诊断结果、评分和建议整理成结构化的会话报告`,
}

type SubAgentRuntime struct {
	pool         *ConcurrencyPool
	agents       map[string]*SubAgentConfig
	queryEngine  *queryengine.QueryEngine
	toolRegistry *tool.ToolRegistry
}

type SubAgentRuntimeOptions struct {
	MaxConcurrency int
	ToolRegistry   *tool.ToolRegistry
}

func NewSubAgentRuntime(queryEngine *queryengine.QueryEngine, opts *SubAgentRuntimeOptions) *SubAgentRuntime {
	maxConcurrency := 3
	if opts != nil && opts.MaxConcurrency > 0 {
		maxConcurrency = opts.MaxConcurrency
	}

	var toolRegistry *tool.ToolRegistry
	if opts != nil {
		toolRegistry = opts.ToolRegistry
	}
	return &SubAgentRuntime{
		pool:         NewConcurrencyPool(maxConcurrency),
		agents:       make(map[string]*SubAgentConfig),
		queryEngine:  queryEngine,
		toolRegistry: toolRegistry,
	}
}

func (sr *SubAgentRuntime) Register(config SubAgentConfig) {
	if config.SystemPrompt == "" {
		if prompt, ok := RolePrompts[config.Role]; ok {
			config.SystemPrompt = prompt
		}

	}
	sr.agents[config.ID] = &config
}

func (sr *SubAgentRuntime) Dispatch(task SubAgentTask) SubAgentResult {
	config := sr.agents[task.AgentId]
	if config == nil {
		return SubAgentResult{
			AgentId:    task.AgentId,
			Role:       SubAgentRoleResearcher,
			Output:     "",
			TokenUsage: TokenUsage{Input: 0, Output: 0},
			Duration:   0,
			Success:    false,
			Error:      `Agent"` + task.AgentId + `"not registered`,
		}
	}

	result, err := sr.pool.Run(func() (interface{}, error) {
		return sr.execute(config, task), nil
	})

	if err != nil {
		return SubAgentResult{
			AgentId:    task.AgentId,
			Role:       SubAgentRoleResearcher,
			Output:     "",
			TokenUsage: TokenUsage{Input: 0, Output: 0},
			Duration:   0,
			Success:    false,
			Error:      `Agent"` + task.AgentId + `"not registered`,
		}
	}

	return result.(SubAgentResult)

}

func (sr *SubAgentRuntime) DispatchAll(tasks []SubAgentTask) []SubAgentResult {
	results := make([]SubAgentResult, 0, len(tasks))
	for _, task := range tasks {
		results = append(results, sr.Dispatch(task))
	}
	return results

}

func (sr *SubAgentRuntime) execute(config *SubAgentConfig, task SubAgentTask) SubAgentResult {
	log := logger.DefaultLogger

	log.Info("execute started", map[string]interface{}{
		"component":     "SubAgent",
		"agentId":       config.ID,
		"role":          config.Role,
		"parentSession": task.ParentSessionId,
	})

	start := time.Now()
	maxIterations := config.MaxIterations
	if maxIterations < 0 {
		maxIterations = 5
	}
	totalUsage := TokenUsage{Input: 0, Output: 0}

	messages := make([]queryengine.Message, 0)

	if task.Context != "" {
		messages = append(messages, *queryengine.NewMessageWithContent(queryengine.MessageRoleUser, "[上下文]\n"+task.Context))
		messages = append(messages, *queryengine.NewMessageWithContent(queryengine.MessageRoleAssistant, "已了解上下文，请继续"))

	}

	messages = append(messages, *queryengine.NewMessageWithContent(queryengine.MessageRoleUser, task.Input))

	var tools *[]queryengine.ToolSchema
	if len(config.Tools) > 0 {
		tools = &config.Tools
	} else {
		toolSchema := make([]queryengine.ToolSchema, 0)
		for _, t := range sr.toolRegistry.ListSchemas() {
			toolSchema = append(toolSchema, queryengine.ToolSchema{
				Name:        t.Name,
				Description: "",
				Parameters:  make(map[string]interface{}),
			})
		}

		tools = &toolSchema
	}

	for i := 0; i < maxIterations; i++ {
		params := queryengine.QueryParams{
			Messages:     messages,
			SystemPrompt: &config.SystemPrompt,
			Tools:        tools,
			MaxTokens:    ptr(2048),
		}

		if config.Model != "" {
			params.Model = &config.Model
		}

		response, err := sr.queryEngine.Query(params)
		if err != nil {
			log.Error("execute failed", map[string]interface{}{
				"error":    err.Error(),
				"duration": time.Since(start).Milliseconds(),
			})

			return SubAgentResult{
				AgentId:    config.ID,
				Role:       config.Role,
				Output:     "",
				TokenUsage: totalUsage,
				Duration:   time.Since(start).Milliseconds(),
				Success:    false,
			}
		}

		totalUsage.Input += response.Usage.InputTokens
		totalUsage.Output += response.Usage.OutputTokens

		if response.Type == "text" {
			output := ""
			if response.Content != nil {
				output = *response.Content
			}
			return SubAgentResult{
				AgentId:    config.ID,
				Role:       config.Role,
				Output:     output,
				TokenUsage: totalUsage,
				Duration:   time.Since(start).Microseconds(),
				Success:    true,
			}

		}

		if response.Type == "tool_use" && response.ToolCalls != nil && sr.toolRegistry != nil {
			content := ""
			if response.Content != nil {
				content = *response.Content
			}
			assistantMsg := queryengine.Message{
				Role:      queryengine.MessageRoleAssistant,
				Content:   &content,
				ToolCalls: response.ToolCalls,
			}
			messages = append(messages, assistantMsg)

			for _, toolCall := range *response.ToolCalls {
				output := ""
				isError := false

				toolDef := sr.toolRegistry.Get(toolCall.Name)
				if toolDef == nil {
					output = `Tool"` + toolCall.Name + `" not found`
					isError = true
				} else {
					result := sr.toolRegistry.Execute(toolCall.Name, toolCall.Input, tool.ToolContext{SessionId: task.ParentSessionId})
					output = result.Output
					isError = !result.Success

				}

				toolMsg := queryengine.Message{
					Role:       queryengine.MessageRoleTool,
					ToolCallID: &toolCall.ID,
					Content:    &output,
					IsError:    &isError,
				}
				messages = append(messages, toolMsg)

			}
		} else {
			break
		}
	}

	var lastAssistant *queryengine.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == queryengine.MessageRoleAssistant {
			lastAssistant = &messages[i]
			break
		}

	}
	output := ""
	if lastAssistant != nil && lastAssistant.Content != nil {
		output = *lastAssistant.Content
	}

	return SubAgentResult{
		AgentId:    config.ID,
		Role:       config.Role,
		Output:     output,
		TokenUsage: totalUsage,
		Duration:   time.Since(start).Microseconds(),
		Success:    true,
	}

}

func ptr[T any](v T) *T {
	return &v

}
