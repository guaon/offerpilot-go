package tool

import (
	"encoding/json"
	"math/rand"
	"strings"

	"github.com/cloudwego/eino/schema"
)

var (
	TechKeywords = []string{
		"Python", "Java", "Go", "TypeScript", "Rust", "C++",
		"LLM", "Agent", "RAG", "NLP", "ML", "Deep Learning",
		"LangChain", "LangGraph", "PyTorch", "TensorFlow",
		"Kubernetes", "Docker", "AWS", "GCP", "Azure",
		"React", "Next.js", "Node.js", "FastAPI", "Spring",
		"PostgreSQL", "Redis", "MongoDB", "Elasticsearch",
		"CI/CD", "Microservices", "System Design",
		"Prompt Engineering", "Fine-tuning", "RLHF", "Embedding",
		"Vector Database", "Milvus", "Pinecone", "Weaviate",
	}

	SoftSkills = []string{
		"沟通", "协作", "领导力", "自驱", "抗压",
		"跨团队", "快速学习", "创新", "解决问题", "项目管理",
	}

	MockResults = []struct {
		Title        string
		Dimension    string
		Question     string
		ExpertAnswer string
	}{
		{"ReAct 循环的工程实现", "architecture", "什么是 ReAct 模式？在工程实现中需要注意什么？", "ReAct 核心是 Observe → Think → Act → Observe 闭环。工程关键点：循环终止条件（max_iterations 兜底）、Tool 错误恢复、Context 膨胀管理、结构化日志观测。"},
		{"Tool Calling 机制", "architecture", "Agent 的 Tool Calling 机制是怎么工作的？", "LLM 输出结构化调用意图而非自然语言。关键差异：OpenAI function_call 参数是字符串 JSON 需 parse，Anthropic tool_use 直接是 object。工程重点：schema 精度、流式拼接、并行策略。"},
		{"Agent Harness vs 框架", "engineering", "什么是 Agent Harness？和 LangChain 有什么区别？", "Harness 是自建基础设施层，10 层模型涵盖 Tools→Skills→QueryEngine→Context→Memory→Permission→Sessions→Command→Hook→Sub-agent。选择 Harness 而非框架是因为生产需要完全控制权、可调试性和性能定位。"},
		{"System Prompt 设计", "model", "System Prompt 的设计有什么讲究？", "5 个设计原则：角色具体、边界明确、格式规范、Tool 指引、分段组织。坑：太长稀释注意力、指令冲突、缺 negative examples。"},
		{"多 Provider 统一调用层", "architecture", "如何设计一个支持多 Provider 的 LLM 调用层？", "5 层设计：统一 Provider Interface（AsyncIterable<StreamEvent>）、Provider Router（model→provider 映射）、Retry + Error Classification（统一错误分类）、Token 计数归一化、流式适配层。"},
	}
)

func SearchKnowledge() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "search_knowledge",
			Desc: "搜索面试知识库,查找与指定主题相关的面试题目、参考答案和考察点分析",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"query":     {Type: schema.String, Desc: "搜索关键词或问题描述", Required: true},
				"dimension": {Type: schema.String, Desc: "限定搜索的维度分类(可选)", Enum: []string{"architecture", "engineering", "model", "rag", "multi-agent", "evaluation", "full-stack"}},
				"limit":     {Type: schema.Number, Desc: "返回结果数量上限,默认 5"},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			query := getStr(input, "query")
			dimension := getStr(input, "dimension")
			limit := getInt(input, "limit", 5)

			type result struct {
				Title        string  `json:"title"`
				Dimension    string  `json:"dimension"`
				Questions    string  `json:"question"`
				ExpertAnswer string  `json:"expertAnser"`
				Score        float64 `json:"score"`
			}

			var results []result
			var source string = "db"

			dbResults, err := SearchKnowledgeFromDB(query, dimension, limit)
			if err != nil || len(dbResults) == 0 {
				source = "mock"
				lower := strings.ToLower(query)
				filtered := MockResults

				if dimension != "" {
					var filtered2 []struct{ Title, Dimension, Question, ExpertAnswer string }
					for _, r := range filtered {
						if r.Dimension == dimension {
							filtered2 = append(filtered2, r)
						}
					}
					filtered = filtered2
				}

				var filtered3 []struct{ Title, Dimension, Question, ExpertAnswer string }
				for _, r := range filtered {
					if strings.Contains(strings.ToLower(r.Title), lower) ||
						strings.Contains(strings.ToLower(r.Question), lower) ||
						strings.Contains(strings.ToLower(r.ExpertAnswer), lower) {
						filtered3 = append(filtered3, r)
					}
				}
				filtered = filtered3

				if len(filtered) > limit {
					filtered = filtered[:limit]
				}

				results = make([]result, len(filtered))
				for i, r := range filtered {
					results[i] = result{r.Title, r.Dimension, r.Question, r.ExpertAnswer, 1 - float64(i)*0.1}
				}
			} else {
				results = make([]result, len(dbResults))
				for i, r := range dbResults {
					results[i] = result{r.Title, r.Dimension, r.Question, r.ExpertAnswer, 1 - float64(i)*0.1}
				}
			}

			data, _ := json.Marshal(map[string]interface{}{"query": query, "dimension": dimension, "results": results})
			return ToolResult{Success: true, Output: string(data), Metadata: map[string]interface{}{"source": source}}
		},
	}
}

func AnalyzeJD() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "analyze_jd",
			Desc: "解析职位描述(JD,提取硬性要求、技术栈、加分项、团队信息和面试准备重点",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"jdText":      {Type: schema.String, Desc: "职位描述全文", Required: true},
				"targetLevel": {Type: schema.String, Desc: "目标职级（可选，辅助分析难度）", Enum: []string{"junior", "mid", "senior", "staff", "principal"}},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			jdText := getStr(input, "jdText")
			lower := strings.ToLower(jdText)

			var techRequired, techNiceToHave []string
			for _, kw := range TechKeywords {
				if strings.Contains(lower, strings.ToLower(kw)) {
					if strings.Contains(jdText, "必须") || strings.Contains(jdText, "required") || strings.Contains(jdText, "熟练") {
						techRequired = append(techRequired, kw)
					} else {
						techNiceToHave = append(techNiceToHave, kw)
					}
				}
			}

			if len(techRequired) == 0 && len(techNiceToHave) > 0 {
				split := (len(techNiceToHave)*6 + 9) / 10
				techRequired, techNiceToHave = techNiceToHave[:split], techNiceToHave[split:]
			}

			var softSkills []string
			for _, s := range SoftSkills {
				if strings.Contains(jdText, s) {
					softSkills = append(softSkills, s)
				}
			}

			var prepFocus []string
			hasTech := func(techs ...string) bool {
				for _, t := range techs {
					for _, r := range techRequired {
						if r == t {
							return true
						}
					}
				}
				return false
			}
			if hasTech("LLM", "Agent", "RAG", "Prompt Engineering") {
				prepFocus = append(prepFocus, "Agent 架构设计 & ReAct 循环实现", "RAG 全链路（Chunk → Embedding → Retrieval → Rerank）")
			}
			if hasTech("System Design", "Microservices", "Kubernetes") {
				prepFocus = append(prepFocus, "系统设计（高并发、分布式）")
			}
			if hasTech("Python", "Go", "Java") {
				prepFocus = append(prepFocus, "语言基础 & 算法题")
			}

			if len(softSkills) > 0 {
				prepFocus = append(prepFocus, "行为面试（STAR法则准备 3-5 个案例")
			}
			if len(prepFocus) == 0 {
				prepFocus = append(prepFocus, "技术深度+项目经验复盘")
			}

			data, _ := json.Marshal(map[string]interface{}{
				"techStack":     map[string]interface{}{"required": techRequired, "niceToHave": techNiceToHave},
				"softSkills":    softSkills,
				"interviewPrep": prepFocus,
			})
			return ToolResult{Success: true, Output: string(data)}
		},
	}
}

func MockInterview() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "mock_interview",
			Desc: "根据 JD 和简历生成模拟面试题目序列，覆盖技术深度、项目经验、行为面试三个维度",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"jdText":     {Type: schema.String, Desc: "JD内容（可选）"},
				"resumeText": {Type: schema.String, Desc: "简历内容（可选）"},
				"dimension":  {Type: schema.String, Desc: "面试维度（默认 mixed）", Enum: []string{"technical", "project", "behavioral", "mixed"}},
				"difficulty": {Type: schema.String, Desc: "难度（默认 medium）", Enum: []string{"easy", "medium", "hard"}},
				"count":      {Type: schema.Number, Desc: "题目数量（默认 5）"},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			dimension := getStr(input, "dimension", "mixed")
			difficulty := getStr(input, "difficulty", "medium")
			count := getInt(input, "count", 5)

			type question struct {
				Q    string
				Dim  string
				Diff string
			}

			technical := []question{
				{"请介绍一下 Agent 的 ReAct 循环，以及在工程实现中需要注意什么？", "technical", "medium"},
				{"如何设计一个支持多 Provider 的 LLM 调用层？说说你的接口抽象思路", "technical", "hard"},
				{"RAG 系统中，Chunk 策略和 Retrieval 策略分别有哪些选择？trade-off 是什么？", "technical", "hard"},
				{"Tool Calling 的流式处理要注意什么？如果 tool input 是增量送达的怎么处理？", "technical", "medium"},
				{"什么是 Agent Harness？和 LangChain 的本质区别在哪里？", "technical", "medium"},
				{"如何解决 Agent 循环中的 Context Window 膨胀问题？", "technical", "hard"},
				{"Embedding 模型选型时你会考虑哪些因素？", "technical", "easy"},
				{"System Prompt 的设计有什么讲究？怎么减少 prompt injection 风险？", "technical", "medium"},
			}

			project := []question{
				{"你做过的最复杂的 Agent 项目是什么？遇到了什么核心难点？", "project", "medium"},
				{"你在项目中是如何做技术选型的？举一个关键决策的例子", "project", "medium"},
				{"说一个你优化系统性能的案例，量化结果是什么？", "project", "medium"},
				{"你如何衡量 Agent 的输出质量？用过什么评测方案？", "project", "hard"},
				{"项目中遇到过线上事故吗？你是怎么处理和复盘的？", "project", "medium"},
			}

			behavioral := []question{
				{"说一个你和团队意见不一致的例子，最终怎么解决的？", "behavioral", "medium"},
				{"你是怎么在紧急 deadline 下保证交付质量的？", "behavioral", "easy"},
				{"你是怎么快速学习一个新技术领域的？举个最近的例子", "behavioral", "easy"},
				{"你如何评估自己的技术成长？最近半年最大的提升是什么？", "behavioral", "easy"},
			}

			pool := append(append(technical, project...), behavioral...)

			if dimension != "mixed" {
				var filtered []question
				for _, q := range pool {
					if q.Dim == dimension {
						filtered = append(filtered, q)
					}
				}

				pool = filtered
			}

			if difficulty == "easy" {
				var filtered []question
				for _, q := range pool {
					if q.Diff != "hard" {
						filtered = append(filtered, q)
					}
				}
				pool = filtered
			} else if difficulty == "hard" {
				var filtered []question
				for _, q := range pool {
					if q.Diff != "easy" {
						filtered = append(filtered, q)
					}

				}
				pool = filtered
			}

			for i := len(pool) - 1; i > 0; i-- {
				j := rand.Intn(i + 1)
				pool[i], pool[j] = pool[j], pool[i]
			}
			if len(pool) > count {
				pool = pool[:count]
			}

			type qResult struct {
				Index      int    `json:"index"`
				Question   string `json:"question"`
				Dimension  string `json:"dimension"`
				Difficulty string `json:"difficulty"`
			}

			results := make([]qResult, len(pool))
			for i, q := range pool {
				results[i] = qResult{i + 1, q.Q, q.Dim, q.Diff}
			}

			data, _ := json.Marshal(map[string]interface{}{
				"dimension":      dimension,
				"difficulty":     difficulty,
				"totalQuestions": len(results),
				"question":       results,
				"tips":           []string{"每道题用 STAR 法则组织回答", "技术题先给结论在展开", "主动提到踩坑经验和量化结果"},
			})

			return ToolResult{Success: true, Output: string(data)}

		},
	}
}

func getStr(input map[string]interface{}, key string, defaultValue ...string) string {
	if v, ok := input[key].(string); ok {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func getInt(input map[string]interface{}, key string, defaultValue int) int {
	if v, ok := input[key].(float64); ok {
		return int(v)
	}
	return defaultValue
}
