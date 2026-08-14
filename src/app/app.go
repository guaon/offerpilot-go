package app

import (
	"MyOfferPilot/src/agent"
	appcontext "MyOfferPilot/src/context"
	hooks "MyOfferPilot/src/hook"
	"MyOfferPilot/src/knowledge"
	"MyOfferPilot/src/logger"
	"MyOfferPilot/src/memory"
	"MyOfferPilot/src/permission"
	queryengine "MyOfferPilot/src/query-engine"
	"MyOfferPilot/src/query-engine/provider"
	"MyOfferPilot/src/session"
	subagent "MyOfferPilot/src/sub-agent"
	tool "MyOfferPilot/src/tools"
	"context"
	"os"
	"path/filepath"
)

const SystemPrompt = `你是 OfferPilot，一个全链路求职辅导 Agent，专注于 AI Agent / LLM 工程方向。

你的核心能力：

【面试诊断】
1. 搜索知识库中的 385+ 道真实面试题及高手答案
2. 对用户的回答进行结构化诊断（评分 + 差距分析 + 改进建议）
3. 模拟面试官追问，检验理解深度
4. 对比用户答案与专家答案的差距

【JD 分析】
5. 解析职位描述，提取技术栈要求、职级信号、团队信息
6. 生成针对该 JD 的面试准备重点

【简历优化】
7. 分析简历与 JD 的匹配度，找出差距项
8. 对简历段落提出优化建议（量化、STAR、关键词）
9. 根据目标 JD 定向包装简历亮点

【模拟面试】
10. 根据 JD + 简历生成个性化面试题序列
11. 推荐个性化学习路径

【实时面试模拟】
12. 面试官提问 → TTS 语音播报
13. 候选人作答 → 实时缺陷检测（结构/深度/案例/口头禅/偏题/模糊等）
14. 每题即时反馈 + 改进建议
15. 全场总结报告（评分 + 缺陷分布 + 高频问题）

录音转写诊断规则：
- 如果用户输入来自录音转写，先从文本中拆分"面试官问题"和"候选人回答"
- 评分必须围绕识别出的面试官问题，不要因为回答里出现 Agent、RAG、ReAct 等关键词就替换题目
- 如果问题和回答边界不清，先说明不确定性，再基于可见内容谨慎诊断

工作方式：
- 用户贴入 JD → **必须调用 analyze_jd 工具**，不要自己分析
- 用户说"模拟面试"/"生成面试题" → **必须调用 mock_interview 工具**
- 用户说"搜索"/"查找"面试题 → **必须调用 search_knowledge 工具**
- 用户输入面试回答 → 先调用 search_knowledge 搜索该题目的高手答案，诊断后必须调用 record_diagnosis 记录评分

**强制规则：**
1. 你没有实时更新的知识，必须调用工具来获取信息
2. 分析 JD 必须用 analyze_jd 工具，工具会返回结构化数据
3. 搜索面试题必须用 search_knowledge 工具
4. 生成模拟面试题必须用 mock_interview 工具
5. 禁止直接凭记忆回答，应该先调用工具
6. 诊断用户回答前，必须先调用 search_knowledge 获取该题目的高手答作为对照
7. 完成诊断后，必须调用 record_diagnosis 工具记录评分（维度+分数+题目），这会自动更新能力雷达图`

type AppOptions struct {
	Model           string
	SessionManager  *session.SessionManager
	MemoryStore     *memory.MemoryStore
	OnTextDelta     func(text string)
	OnThinkingDelta func(text string)
	OnToolCall      func(name string, input map[string]interface{})
	OnToolResult    func(name string, input string)
	// OnDiagnosisRecord is passed through to the record_diagnosis tool.
	OnDiagnosisRecord func(sessionID string, dimension string, score int, question string)
}

type App struct {
	Agent           *agent.AgentLoop
	SessionManager  *session.SessionManager
	QueryEngine     *queryengine.QueryEngine
	ToolRegistry    *tool.ToolRegistry
	MemoryStore     *memory.MemoryStore
	HookPipeline    *hooks.HookPipeline
	SubAgentRuntime *subagent.SubAgentRuntime
}

func CreateApp(opts *AppOptions) *App {
	logger.DefaultLogger.Info("Creating application...")

	providers := buildProviders()
	queryEngine := queryengine.NewQueryEngine(queryengine.QueryEngineOptions{Providers: providers})

	toolRegistry := tool.NewToolRegistry()

	if kbPath := os.Getenv("KNOWLEDGE_DB_PATH"); kbPath != "" {
		ks := initKnowledgeSearch(kbPath)
		if ks != nil {
			tool.SetKnowledgeSearch(ks)
			logger.DefaultLogger.Info("Knowledge search initialized", map[string]interface{}{"path": kbPath})
		}
	}

	toolRegistry.Registry(tool.SearchKnowledge())
	toolRegistry.Registry(tool.AnalyzeJD())
	toolRegistry.Registry(tool.MockInterview())
	toolRegistry.Registry(tool.RecordDiagnosis())

	logger.DefaultLogger.Info("Tools registered", map[string]interface{}{
		"toolCount": len(toolRegistry.ListSchemas()),
		"tools":     getRegisteredToolNames(toolRegistry),
	})

	permissionGate := permission.NewPermissionGate()

	// Register rate limits to prevent tool-call loops.
	// search_knowledge: max 5 calls/min (enough for multi-question diagnosis)
	// mock_interview: max 3 calls/min (one per interview session)
	// analyze_jd: max 5 calls/min
	permissionGate.RegisterRule(permission.PermissionRule{
		ToolName:           "search_knowledge",
		RateLimitPerMinute: 5,
	})
	permissionGate.RegisterRule(permission.PermissionRule{
		ToolName:           "mock_interview",
		RateLimitPerMinute: 3,
	})
	permissionGate.RegisterRule(permission.PermissionRule{
		ToolName:           "analyze_jd",
		RateLimitPerMinute: 5,
	})

	contextManager := appcontext.NewContextManager(nil)

	var sessionManager *session.SessionManager
	var err error
	if opts != nil && opts.SessionManager != nil {
		sessionManager = opts.SessionManager
	} else {
		dbPath := os.Getenv("SESSION_DB_PATH")
		sessionManager, err = session.NewSessionManager(dbPath)
		if err != nil {
			logger.DefaultLogger.Warn("Failed to create session manager", map[string]interface{}{"error": err.Error()})
			sessionManager = &session.SessionManager{}
		} else if dbPath != "" {
			logger.DefaultLogger.Info("Session manager loaded", map[string]interface{}{"dbPath": dbPath})
		}
	}
	sessionManager.EnsureLoaded()

	var memStore *memory.MemoryStore
	if opts != nil && opts.MemoryStore != nil {
		memStore = opts.MemoryStore
	} else {
		var err error
		memStore, err = memory.NewMemoryStore("")
		if err != nil {
			logger.DefaultLogger.Warn("failed to create memory store", map[string]interface{}{"error": err.Error()})
			memStore = &memory.MemoryStore{}
		}
	}

	subAgentRuntime := subagent.NewSubAgentRuntime(queryEngine, &subagent.SubAgentRuntimeOptions{
		MaxConcurrency: 0,
		ToolRegistry:   toolRegistry,
	})

	subAgentRuntime.Register(subagent.SubAgentConfig{ID: "diagnostician", Role: subagent.SubAgentRoleDiagnostician, MaxIterations: 1})
	subAgentRuntime.Register(subagent.SubAgentConfig{ID: "interviewer", Role: subagent.SubAgentRoleInterviewer})
	subAgentRuntime.Register(subagent.SubAgentConfig{ID: "researcher", Role: subagent.SubAgentRoleResearcher})
	subAgentRuntime.Register(subagent.SubAgentConfig{ID: "reporter", Role: subagent.SubAgentRoleReporter})
	subAgentRuntime.Register(subagent.SubAgentConfig{ID: "jd-analyst", Role: subagent.SubAgentRoleJDAnalyst})
	subAgentRuntime.Register(subagent.SubAgentConfig{ID: "resume-optimizer", Role: subagent.SubAgentRoleResumeOptimizer})
	subAgentRuntime.Register(subagent.SubAgentConfig{ID: "gap-analyzer", Role: subagent.SubAgentRoleGapAnalyzer})

	hookPipeline := hooks.NewHookPipeline()
	hookPipeline.Register(&hooks.InputSanitizerHook{})
	hookPipeline.Register(&hooks.TokenCounterHook{})

	contextManager.SetLayer(appcontext.ContextWindowKeySystem, SystemPrompt, -1)

	defaultModel := ""
	if opts != nil && opts.Model != "" {
		defaultModel = opts.Model
	} else {
		defaultModel = os.Getenv("DEFAULT_MODEL")
	}

	var onTextDelta func(text string)
	var onThinkingDelta func(text string)
	var onToolCall func(name string, input map[string]interface{})
	var onToolResult func(name string, result string)
	var onDiagnosisRecord func(sessionID string, dimension string, score int, question string)

	if opts != nil {
		onTextDelta = opts.OnTextDelta
		onThinkingDelta = opts.OnThinkingDelta
		onToolCall = opts.OnToolCall
		onToolResult = opts.OnToolResult
		onDiagnosisRecord = opts.OnDiagnosisRecord
	}

	agentLoop := agent.NewAgentLoop(agent.AgentConfig{
		QueryEngine:     queryEngine,
		ToolRegistry:    toolRegistry,
		PermissionGate:  permissionGate,
		ContextManager:  contextManager,
		SessionManager:  sessionManager,
		MemoryStore:     memStore,
		HookPipeline:    hookPipeline,
		DefaultModel:    defaultModel,
		MaxIterations:   15,
		MaxBudgetTokens: 50000,
		OnTextDelta:     onTextDelta,
		OnThinkingDelta: onThinkingDelta,
		OnToolCall:      onToolCall,
		OnToolResult:    onToolResult,
		OnDiagnosisRecord: onDiagnosisRecord,
	})

	return &App{
		Agent:           agentLoop,
		SessionManager:  sessionManager,
		QueryEngine:     queryEngine,
		ToolRegistry:    toolRegistry,
		MemoryStore:     memStore,
		HookPipeline:    hookPipeline,
		SubAgentRuntime: subAgentRuntime,
	}

}

func buildProviders() []queryengine.ProviderConfig {
	var providers []queryengine.ProviderConfig
	ctx := context.Background()

	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		defaultModel := os.Getenv("ANTHROPIC_MODEL")
		if defaultModel == "" {
			defaultModel = "claude-sonnet-4-20250514"
		}
		config := &provider.ClaudeConfig{
			APIKey: apiKey,
			Model:  defaultModel,
			// Temperature: 0.7,
		}
		if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
			config.BaseURL = baseURL
		}

		providerInst, err := provider.NewClaudeProvider(ctx, config)
		if err != nil {
			logger.DefaultLogger.Warn("Failed to create Claude provider", map[string]interface{}{"error": err.Error()})
		} else if err := providerInst.Validate(ctx); err != nil {
			logger.DefaultLogger.Warn("Claude provider validation failed", map[string]interface{}{"error": err.Error()})
		} else {
			providers = append(providers, queryengine.ProviderConfig{
				Provider:     providerInst,
				Models:       []string{"claude-sonnet-4-20250514", "claude-opus-4-7", "claude-opus-4-20250514", "claude-sonnet-4-7", "claude-haiku-4-7", defaultModel},
				DefaultModel: defaultModel,
			})
			logger.DefaultLogger.Info("Claude provider registered")
		}
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		defaultModel := os.Getenv("OPENAI_MODEL")
		if defaultModel == "" {
			defaultModel = "gpt5.5"
		}
		config := &provider.OpenAIConfig{
			APIKey:  apiKey,
			Model:   defaultModel,
			BaseURL: os.Getenv("OPENAI_BASE_URL"),
		}

		providerInst, err := provider.NewOpenAIProvider(ctx, config)
		if err != nil {
			logger.DefaultLogger.Warn("Failed to create OpenAI provider", map[string]interface{}{"error": err.Error()})
		} else if err := providerInst.Validate(ctx); err != nil {
			logger.DefaultLogger.Warn("OpenAI provider validation failed", map[string]interface{}{"error": err.Error()})
		} else {
			providers = append(providers, queryengine.ProviderConfig{
				Provider:     providerInst,
				Models:       []string{"gpt-5.5", defaultModel, "gpt-4o", "gpt-4o-mini", "gpt-4-turbo"},
				DefaultModel: defaultModel,
			})
			logger.DefaultLogger.Info("OpenAI provider registered")
		}
	}

	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		defaultModel := os.Getenv("DEEPSEEK_MODEL")
		if defaultModel == "" {
			defaultModel = "deepseek-chat"
		}

		providerInst, err := provider.NewDeepSeekProvider(ctx, apiKey, os.Getenv("DEEPSEEK_BASE_URL"), defaultModel)
		if err != nil {
			logger.DefaultLogger.Warn("Failed to create DeepSeek provider", map[string]interface{}{"error": err.Error()})
		} else if err := providerInst.Validate(ctx); err != nil {
			logger.DefaultLogger.Warn("DeepSeek provider validation failed", map[string]interface{}{"error": err.Error()})
		} else {
			providers = append(providers, queryengine.ProviderConfig{
				Provider:     providerInst,
				Models:       []string{defaultModel, "deepseek-v4-pro-cc", "deepseek-coder"},
				DefaultModel: defaultModel,
			})
			logger.DefaultLogger.Info("DeepSeek provider registered")
		}
	}

	if len(providers) == 0 {
		mockProvider := provider.NewMockProvider()
		providers = append(providers, queryengine.ProviderConfig{
			Provider:     mockProvider,
			Models:       []string{"mock"},
			DefaultModel: "mock",
		})
		logger.DefaultLogger.Warn("No providers configured,using mock provider")

	}

	return providers

}

func getRegisteredToolNames(r *tool.ToolRegistry) []string {
	schemas := r.ListSchemas()
	names := make([]string, len(schemas))
	for i, s := range schemas {
		names[i] = s.Name
	}
	return names
}

func initKnowledgeSearch(dbPath string) *knowledge.KnowledgeSearch {
	logger.DefaultLogger.Info("initKnowledgeSearch called", map[string]interface{}{"dbPath": dbPath})
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		logger.DefaultLogger.Warn("knowledge db path invalid", map[string]interface{}{"dbPath": dbPath, "error": err.Error()})
		return nil
	}
	logger.DefaultLogger.Info("knowledge db resolved to", map[string]interface{}{"absPath": absPath})
	ks := knowledge.NewKnowledgeSearchFromFile(absPath)
	if ks == nil {
		logger.DefaultLogger.Warn("knowledge db failed to open", map[string]interface{}{"absPath": absPath})
		return nil
	}
	cnt, _ := ks.Count()
	logger.DefaultLogger.Info("knowledge db loaded", map[string]interface{}{"absPath": absPath, "entries": cnt})
	return ks
}
