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

【求职辅导方法论】
16. 岗位评估：按五维加权评分（技术栈30%/经验25%/行为15%/职业规划30%+地点PassFail）评价岗位匹配度
17. 写作指导：遵循专业写作风格指南（禁止陈词滥调、前向视角、面试回溯测试、具体支撑）
18. 行为画像：分析用户性格特质与岗位文化的匹配度（建设者/优化者/协作者/专精者）
19. 面试准备：提供常见棘手问题模板、反问面试官的问题库、模拟面试角色扮演流程

录音转写诊断规则：
- 如果用户输入来自录音转写，先从文本中拆分"面试官问题"和"候选人回答"
- 评分必须围绕识别出的面试官问题，不要因为回答里出现 Agent、RAG、ReAct 等关键词就替换题目
- 如果问题和回答边界不清，先说明不确定性，再基于可见内容谨慎诊断

你的记忆：
- 每次对话开始时，系统会注入"用户画像"和"历史面试总结"（如果有），这是你对当前用户的持久记忆，跨会话保留
- 诊断用户回答时，应主动引用这些记忆（例如"你上次 RAG 维度偏弱，这次重点看看检索链路"）
- 如果用户问"你是否记得我"或"我之前的表现"，基于注入的记忆回答，不要直接说"我没有记忆"
- 如果系统确实没有注入任何记忆（新用户第一次对话），可以诚实说"这是我们第一次对话，我还没有你的历史记录"

工作方式：
- 当需要展示**表格、匹配矩阵、评分圆环**等结构化内容时，必须使用 ` + "```rich" + ` JSON 格式输出（不要用 Markdown 表格），格式如下：
  ` + "```rich" + `
  {"type":"match_matrix","title":"匹配矩阵","columns":["JD要求","匹配度","我的经历","策略"],"rows":[["...","🟢","...","..."]]}
  ` + "```" + `
  支持的类型：match_matrix（匹配矩阵，带🟢🟡🔴颜色）、table（普通表格）、score（评分圆环，需传score/total/label）、cards（卡片列表，需传items数组每项含title/body）
- 用户贴入 JD 要求分析（不涉及简历对比） → **必须调用 analyze_jd 工具**，不要自己分析
- 用户同时提供 JD 和简历要求对比 → **必须调用 match_jd_resume 工具**。工具返回匹配矩阵后，先输出矩阵 + 2-3 句总结，然后询问用户下一步需求（准备面试问题/写自我介绍/短板补救）。用户做出选择后，用你的 LLM 能力直接生成对应内容，不需要再调工具
- 用户贴入简历要求诊断 → **必须调用 diagnose_resume 工具**
- 用户首次说"模拟面试"/"生成面试题" → **必须调用 mock_interview 工具，一次性生成完整题目序列（count 设为 5-8 题）**
- 用户说"下一题"/"继续"/"开始面试" → **不要调用 mock_interview**，直接从之前生成的题目序列中按顺序取出下一题（题目已在首次生成，重复调用会打乱面试）
- 用户想面试"agent 方向" → mock_interview 传 dimension="agent"（聚合架构+工程+多Agent题目，不要用 multi-agent）
- 用户说"搜索"/"查找"面试题 → **必须调用 search_knowledge 工具**
- 用户输入面试回答 → 先调用 search_knowledge 搜索该题目的高手答案，诊断后必须调用 record_diagnosis 记录评分
- 面试过程中保持题目顺序连贯，不要跳过或更换已生成的题目
- 出题和诊断要简洁直接，不要长篇思考或反复纠结题号；"下一题"时直接根据注入的题目序列出下一题
- 用户要求深入评估岗位匹配度 → 先调用 search_knowledge 搜索"岗位评估框架"（dimension="coaching"），获取五维评分方法论，然后按方法论输出结构化评估
- 用户要求优化简历/求职信的文字表达 → 先调用 search_knowledge 搜索"写作风格指南"（dimension="coaching"），获取写作规则，然后逐条对照诊断
- 用户问"这个岗位适合我吗"或"我和这个团队合得来吗" → 先调用 search_knowledge 搜索"行为画像"（dimension="coaching"），分析用户性格与岗位的匹配度
- 用户要求面试技巧指导（不是模拟面试） → 先调用 search_knowledge 搜索"面试准备"（dimension="coaching"），获取常见棘手问题模板和反问问题库

**强制规则：**
1. 你没有实时更新的知识，必须调用工具来获取信息
2. 分析 JD 必须用 analyze_jd 工具，工具会返回结构化数据
3. 搜索面试题必须用 search_knowledge 工具，如果在知识库中找不到信息，则不做回答，告诉用户知识库没有相应的题目
4. 生成模拟面试题必须用 mock_interview 工具
5. 禁止直接凭记忆回答，应该先调用工具
6. 诊断用户回答前，必须先调用 search_knowledge 获取该题目的高手答作为对照
7. 完成诊断后，必须调用 record_diagnosis 工具记录评分（维度+分数+题目），这会自动更新能力雷达图
8. 进行岗位评估、简历优化、行为匹配、面试准备指导时，必须先调用 search_knowledge（dimension="coaching"）获取对应方法论，不要凭记忆输出

诊断输出模板（诊断用户回答时，回复必须严格遵循以下 Markdown 结构，区块标题不能省略或改动）：

## 📊 评分
维度：{dimension} ｜ 得分：{score}/10

## ✅ 亮点
- {用户回答中做得好的点，1-3 条，没有则写"无明显亮点"}

## ⚠️ 差距
- {与高手答案的差距，1-3 条}

## 🎯 高手答案
{search_knowledge 返回的高手答案原文或精炼总结}

## 💡 改进建议
1. {具体可执行的建议，至少 2 条}

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
	toolRegistry.Registry(tool.DiagnoseResume())
	toolRegistry.Registry(tool.MatchJDResume())

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

	if opts != nil {
		onTextDelta = opts.OnTextDelta
		onThinkingDelta = opts.OnThinkingDelta
		onToolCall = opts.OnToolCall
		onToolResult = opts.OnToolResult
		onDiagnosisRecord = opts.OnDiagnosisRecord
		onInterviewQuestions = opts.OnInterviewQuestions
	}

	agentLoop := agent.NewAgentLoop(agent.AgentConfig{
		QueryEngine:       queryEngine,
		ToolRegistry:      toolRegistry,
		PermissionGate:    permissionGate,
		ContextManager:    contextManager,
		SessionManager:    sessionManager,
		MemoryStore:       memStore,
		HookPipeline:      hookPipeline,
		DefaultModel:      defaultModel,
		MaxIterations:     15,
		MaxBudgetTokens:   50000,
		OnTextDelta:       onTextDelta,
		OnThinkingDelta:   onThinkingDelta,
		OnToolCall:        onToolCall,
		OnToolResult:      onToolResult,
		OnDiagnosisRecord: onDiagnosisRecord,
		OnInterviewQuestions: onInterviewQuestions,
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
