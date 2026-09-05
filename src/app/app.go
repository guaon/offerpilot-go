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
	"database/sql"
	"os"
	"path/filepath"
	"time"
)

const SystemPrompt = `你是 OfferPilot，一个 AI Agent / LLM 工程方向的求职辅导 Agent。你没有实时知识，所有专业信息必须通过工具获取。

## 记忆与冷启动
- 系统会在对话开始时注入用户画像和历史面试总结，这是你的跨会话持久记忆。诊断时主动引用（如"你上次 RAG 偏弱，这次重点看检索链路"）。用户问"你记得我吗"时基于注入的记忆回答，不要直接说"我没有记忆"。新用户无记忆时可诚实说明。
- 发现没有用户画像时，主动询问三个关键信息：求职方向、工作年限、技术栈。收集完后说"画像已就绪"，自然过渡到用户问题。用户说"先帮我分析JD"时可以边分析边了解。

## 工作流（按用户意图触发）

**JD 分析**：用户贴 JD 要求分析（不含简历）→ 调用 analyze_jd。用户同时给 JD + 简历 → 调用 match_jd_resume，输出返回的矩阵后询问下一步（准备面试题/写自我介绍/补短板），用户选择后直接用 LLM 能力生成，不再调工具。

**简历诊断**：用户贴简历 → 调用 diagnose_resume。

**简历生成/优化**：用户要求"生成简历"/"优化简历"/"帮我写简历" → 调用 generate_resume。有 JD 传 JD，有现有简历传 resume。工具返回指令后，先调 search_knowledge(query="简历模板", dimension="coaching") 获取模板规则，再按规则生成完整简历。直接输出 Markdown 简历全文，不要额外解释。用户说"改一下某段"时直接修改，不需要重新调工具。
- **重要**：如果用户画像中缺少工作经历、项目经历、教育背景等关键信息，先引导用户补充（"先聊聊你的经历吧——在哪些公司做过什么项目？"），收集到足够信息后再调用 generate_resume。不要凭空编造。

**模拟面试**：用户首次说"模拟面试"/"生成面试题" → 调用 mock_interview，count 传 5。用户提供了简历时，必须把完整简历传入 resumeText；工具返回 source="resume_evidence" 时，围绕 evidence 核验职责边界、技术决策和量化结果，不得编造简历事实。**每轮只出一道题，等用户回答后再出下一题，禁止一次性列出所有题目。**出题时直接给出题目内容，不要加"第X题"编号。

用户回答后，按以下固定格式点评，然后出下一题：

> **评分**：X/10
> **亮点**：（1-2句，具体指出回答中好的部分）
> **不足**：（1-2句，指出遗漏的关键点或错误）
> **标准答案**：（给出这道题的参考答案，覆盖核心知识点，控制在150字以内）
> **建议**：（1-2句，给出改进方向）

点评后直接出下一题。用户说"下一题"/"继续" → 出下一题，不要调 mock_interview。Agent 方向传 dimension="agent"。其他方向如 Go/Java/C++/Python/前端 等自由传 dimension。工具返回 source="llm" 时按提示自行生成题目，标注"题目来源：LLM 生成"。保持题目顺序连贯，不跳题不换题。

**面试准备计划**：用户要求"面试准备"/"准备计划"/"备战面试"/"面试攻略" → 调用 prepare_interview。有 JD 传 JD，有公司名传 company。工具返回指令后，先调 search_knowledge 获取方法论，再按模板逐项生成完整计划（公司研究→能力对标→STAR故事映射→问题序列→反问建议→短板补救）。直接输出计划内容，不要额外解释。

**面试诊断**：用户输入面试回答 → 先调 search_knowledge 搜该题高手答案。知识库有结果则对照诊断；知识库返回 source="llm" 则用你的训练数据作为对照，标注来源。诊断后调 record_diagnosis 记录评分（维度自由填写，Agent 方向用 architecture/engineering 等，其他方向用 Go/Java/系统设计 等）。

**知识搜索**：用户说"搜索"/"查找"面试题 → 调 search_knowledge。工具返回 source="llm" 表示该方向不在知识库覆盖范围内→使用训练数据生成题目和答案，标注"知识来源：LLM 生成"。

**辅导方法论**（以下场景先调 search_knowledge，dimension="coaching"，获取方法论后再输出）：
- 岗位匹配度评估 → 搜"岗位评估框架"
- 简历/求职信文字优化 → 搜"写作风格指南"
- 性格与岗位匹配 → 搜"行为画像"
- 面试技巧指导（非模拟面试）→ 搜"面试准备"

**结构化输出**：展示表格、矩阵、评分时用 ` + "```rich" + ` JSON（不要用 Markdown 表格）。支持类型：match_matrix（匹配矩阵，🟢🟡🔴）、table、score（{score,total,label}）、cards（[{title,body}]）。

**录音转写**：用户输入来自录音转写时，先拆分"面试官问题"和"候选人回答"，围绕识别出的问题进行评分，不因回答中出现技术关键词就替换题目。

## 诊断输出模板（诊断用户回答时严格遵循，区块标题不可省略或改动）

## 📊 评分
维度：{dimension} ｜ 得分：{score}/10

## ✅ 亮点
- {1-3 条，没有则写"无明显亮点"}

## ⚠️ 差距
- {与高手答案的差距，1-3 条}

## 🎯 高手答案
{search_knowledge 返回的高手答案原文或精炼总结}

## 💡 改进建议
1. {至少 2 条具体可执行建议}

注意：{dimension} 和 {score} 必须与 record_diagnosis 工具记录的维度、分数一致。`

type AppOptions struct {
	Model           string
	DB              *sql.DB
	SessionManager  *session.SessionManager
	MemoryStore     *memory.MemoryStore
	OnTextDelta     func(text string)
	OnThinkingDelta func(text string)
	OnToolCall      func(name string, input map[string]interface{})
	OnToolResult    func(name string, input string)
	// OnDiagnosisRecord is passed through to the record_diagnosis tool.
	OnDiagnosisRecord func(sessionID string, dimension string, score int, question string)
	// OnInterviewQuestions is passed through to the mock_interview tool.
	OnInterviewQuestions func(questions []string)
	OnProgress           func(stage string, detail map[string]interface{})
	OnRetry              func(attempt int, maxRetries int, reason string)
}

type App struct {
	Agent           *agent.AgentLoop
	SessionManager  *session.SessionManager
	QueryEngine     *queryengine.QueryEngine
	ToolRegistry    *tool.ToolRegistry
	MemoryStore     *memory.MemoryStore
	HookPipeline    *hooks.HookPipeline
	SubAgentRuntime *subagent.SubAgentRuntime
	PermissionGate  *permission.PermissionGate
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
	toolRegistry.Registry(tool.DiagnoseResume())
	toolRegistry.Registry(tool.MatchJDResume())
	toolRegistry.Registry(tool.GenerateResume())
	toolRegistry.Registry(tool.PrepareInterview())

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
	permissionGate.RegisterRule(permission.PermissionRule{
		ToolName:           "diagnose_resume",
		RateLimitPerMinute: 5,
	})
	permissionGate.RegisterRule(permission.PermissionRule{
		ToolName:           "match_jd_resume",
		RateLimitPerMinute: 5,
	})
	permissionGate.RegisterRule(permission.PermissionRule{
		ToolName:           "generate_resume",
		RateLimitPerMinute: 3,
	})
	permissionGate.RegisterRule(permission.PermissionRule{
		ToolName:           "prepare_interview",
		RateLimitPerMinute: 3,
	})

	contextManager := appcontext.NewContextManager(nil)

	// 统一使用 MySQL（若不可用则降级为纯内存模式）
	var db *sql.DB
	if opts != nil && opts.DB != nil {
		db = opts.DB
	}

	var sessionManager *session.SessionManager
	var err error
	if opts != nil && opts.SessionManager != nil {
		sessionManager = opts.SessionManager
	} else {
		sessionManager, err = session.NewSessionManager(db)
		if err != nil {
			logger.DefaultLogger.Warn("Failed to create session manager", map[string]interface{}{"error": err.Error()})
			sessionManager = &session.SessionManager{}
		}
	}
	sessionManager.EnsureLoaded()

	var memStore *memory.MemoryStore
	if opts != nil && opts.MemoryStore != nil {
		memStore = opts.MemoryStore
	} else {
		var err error
		memStore, err = memory.NewMemoryStore(db)
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
	var onInterviewQuestions func(questions []string)
	var onProgress func(stage string, detail map[string]interface{})
	var onRetry func(attempt int, maxRetries int, reason string)

	if opts != nil {
		onTextDelta = opts.OnTextDelta
		onThinkingDelta = opts.OnThinkingDelta
		onToolCall = opts.OnToolCall
		onToolResult = opts.OnToolResult
		onDiagnosisRecord = opts.OnDiagnosisRecord
		onInterviewQuestions = opts.OnInterviewQuestions
		onProgress = opts.OnProgress
		onRetry = opts.OnRetry
	}

	agentLoop := agent.NewAgentLoop(agent.AgentConfig{
		QueryEngine:          queryEngine,
		ToolRegistry:         toolRegistry,
		PermissionGate:       permissionGate,
		ContextManager:       contextManager,
		SessionManager:       sessionManager,
		MemoryStore:          memStore,
		HookPipeline:         hookPipeline,
		DefaultModel:         defaultModel,
		MaxIterations:        15,
		MaxBudgetTokens:      50000,
		OnTextDelta:          onTextDelta,
		OnThinkingDelta:      onThinkingDelta,
		OnToolCall:           onToolCall,
		OnToolResult:         onToolResult,
		OnDiagnosisRecord:    onDiagnosisRecord,
		OnInterviewQuestions: onInterviewQuestions,
		OnProgress:           onProgress,
		OnRetry:              onRetry,
	})

	return &App{
		Agent:           agentLoop,
		SessionManager:  sessionManager,
		QueryEngine:     queryEngine,
		ToolRegistry:    toolRegistry,
		MemoryStore:     memStore,
		HookPipeline:    hookPipeline,
		SubAgentRuntime: subAgentRuntime,
		PermissionGate:  permissionGate,
	}

}

// NewRequest creates an isolated Agent while reusing the long-lived runtime.
func (a *App) NewRequest(opts *AppOptions) *App {
	contextManager := appcontext.NewContextManager(nil)
	contextManager.SetLayer(appcontext.ContextWindowKeySystem, SystemPrompt, -1)

	model := os.Getenv("DEFAULT_MODEL")
	if opts != nil && opts.Model != "" {
		model = opts.Model
	}

	config := agent.AgentConfig{
		QueryEngine:     a.QueryEngine,
		ToolRegistry:    a.ToolRegistry,
		PermissionGate:  a.PermissionGate,
		ContextManager:  contextManager,
		SessionManager:  a.SessionManager,
		MemoryStore:     a.MemoryStore,
		HookPipeline:    a.HookPipeline,
		DefaultModel:    model,
		MaxIterations:   15,
		MaxBudgetTokens: 50000,
	}
	if opts != nil {
		config.OnTextDelta = opts.OnTextDelta
		config.OnThinkingDelta = opts.OnThinkingDelta
		config.OnToolCall = opts.OnToolCall
		config.OnToolResult = opts.OnToolResult
		config.OnDiagnosisRecord = opts.OnDiagnosisRecord
		config.OnInterviewQuestions = opts.OnInterviewQuestions
		config.OnProgress = opts.OnProgress
		config.OnRetry = opts.OnRetry
	}

	return &App{
		Agent:           agent.NewAgentLoop(config),
		SessionManager:  a.SessionManager,
		QueryEngine:     a.QueryEngine,
		ToolRegistry:    a.ToolRegistry,
		MemoryStore:     a.MemoryStore,
		HookPipeline:    a.HookPipeline,
		SubAgentRuntime: a.SubAgentRuntime,
		PermissionGate:  a.PermissionGate,
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
		} else if err := validateProvider(providerInst); err != nil {
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
		} else if err := validateProvider(providerInst); err != nil {
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
		} else if err := validateProvider(providerInst); err != nil {
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
		logger.DefaultLogger.Warn("No language model providers are available")
	}

	return providers

}

func validateProvider(provider queryengine.LLMProvider) error {
	return validateProviderWithTimeout(provider, 5*time.Second)
}

func validateProviderWithTimeout(provider queryengine.LLMProvider, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return provider.Validate(ctx)
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
