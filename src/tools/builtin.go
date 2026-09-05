package tool

import (
	"encoding/json"
	"fmt"
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

// isAgentRelated 判断查询是否属于 Agent/LLM 方向（知识库覆盖范围）。
func isAgentRelated(query, dimension string) bool {
	agentDims := map[string]bool{
		"architecture": true, "engineering": true, "model": true,
		"rag": true, "multi-agent": true, "evaluation": true, "full-stack": true,
	}
	if dimension != "" && agentDims[dimension] {
		return true
	}
	agentKeywords := []string{
		"agent", "llm", "rag", "prompt", "react", "tool call", "function calling",
		"embedding", "向量", "chunk", "rerank", "hyde", "langchain", "langgraph",
		"context window", "token", "system prompt", "harness", "大模型", "gpt", "claude",
		"多agent", "多 agent", "tool use", "sub-agent", "子 agent",
	}
	lower := strings.ToLower(query)
	for _, kw := range agentKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func SearchKnowledge() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "search_knowledge",
			Desc: "搜索面试知识库,查找与指定主题相关的面试题目、参考答案和考察点分析。知识库目前覆盖 AI Agent / LLM 工程方向。其他方向（Go/Java/C++/Python/前端/系统设计等）请勿调用此工具，直接使用你的训练数据生成题目和答案",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"query":     {Type: schema.String, Desc: "搜索关键词或问题描述", Required: true},
				"dimension": {Type: schema.String, Desc: "限定搜索的维度分类(可选)。Agent方向可选：architecture/engineering/model/rag/multi-agent/evaluation/full-stack"},
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
				Content      string  `json:"content"`
				Questions    string  `json:"question"`
				ExpertAnswer string  `json:"expertAnswer"`
				Score        float64 `json:"score"`
			}

			var results []result
			var source string = "db"

			dbResults, err := SearchKnowledgeFromDB(query, dimension, limit)
			if err != nil || len(dbResults) == 0 {
				// 知识库未命中：判断是否 Agent 方向
				if isAgentRelated(query, dimension) {
					// Agent 方向：fallback 到模拟数据
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
						results[i] = result{r.Title, r.Dimension, "", r.Question, r.ExpertAnswer, 1 - float64(i)*0.1}
					}
				} else {
					// 非 Agent 方向：知识库不覆盖，返回空结果 + 提示
					source = "llm"
					results = []result{}
				}
			} else {
				results = make([]result, len(dbResults))
				for i, r := range dbResults {
					results[i] = result{r.Title, r.Dimension, r.Content, r.Question, r.ExpertAnswer, 1 - float64(i)*0.1}
				}
			}

			output := map[string]interface{}{
				"query":     query,
				"dimension": dimension,
				"results":   results,
				"source":    source,
			}
			// 非 Agent 方向：告诉 LLM 自行生成
			if source == "llm" {
				output["message"] = "知识库只覆盖 AI Agent/LLM 工程方向，当前查询不在覆盖范围内。请根据你的训练数据为该方向生成面试题目和参考答案，并在输出中标注'知识来源：LLM 生成，非知识库题目'。"
			}
			data, _ := json.Marshal(output)
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

			// 五维评估框架（来自岗位评估方法论）
			evaluationFramework := map[string]interface{}{
				"title": "请按以下五维框架输出结构化评估，每维打分并给出理由",
				"dimensions": []map[string]interface{}{
					{"name": "技术栈匹配", "weight": "30%", "guide": "核心要求是否匹配用户的主要技能？Agent方向关注：Agent架构、Tool开发、LLM调用层、Prompt Engineering、RAG"},
					{"name": "经验匹配", "weight": "25%", "guide": "工作内容的实质是否匹配？不要只看岗位名称——一个'数据顾问'和一个'数据科学家'可能做同样的事"},
					{"name": "行为/文化匹配", "weight": "15%", "guide": "JD中是否有红旗信号：部门混乱、维护为主、领导层口碑差、加班文化严重？"},
					{"name": "地点与后勤", "weight": "Pass/Fail", "guide": "通勤可达/远程→PASS；需要搬迁→FAIL（硬否决）；频繁国际出差→FLAG"},
					{"name": "职业规划匹配", "weight": "30%", "guide": "这个岗位是否推进用户的职业目标？工作内容是否激发而非消耗用户？"},
				},
				"thresholds": []map[string]string{
					{"label": "强匹配", "range": "75+", "action": "绝对投递，全力定制"},
					{"label": "良好匹配", "range": "60-74", "action": "投递，在求职信中解决缺口"},
					{"label": "中等匹配", "range": "45-59", "action": "慎重考虑"},
					{"label": "弱匹配", "range": "30-44", "action": "除非有战略原因，否则跳过"},
					{"label": "不匹配", "range": "<30", "action": "跳过"},
				},
				"outputFormat": "| 维度 | 分数 | 说明 |\n|------|------|------|\n| 技术栈 | XX/100 | ... |\n| ... | 综合评分：XX/100 | 结论：[强匹配/良好/中等/弱/不匹配] |",
			}

			// 资格门检查提示
			eligibilityCheck := map[string]string{
				"visa":     "检查JD中是否明确要求公民/永居/安全审查。如果要求了用户没有的→硬停。如果未提及→标记未验证。",
				"language": "检查JD对岗位本身的语言要求（不是广告用什么语言写的）。要求用户未声明的语言→硬停。要求级别可能不够→FLAG。",
			}

			data, _ := json.Marshal(map[string]interface{}{
				"techStack":           map[string]interface{}{"required": techRequired, "niceToHave": techNiceToHave},
				"softSkills":          softSkills,
				"interviewPrep":       prepFocus,
				"evaluationFramework": evaluationFramework,
				"eligibilityCheck":    eligibilityCheck,
			})
			return ToolResult{Success: true, Output: string(data)}
		},
	}
}

func MockInterview() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "mock_interview",
			Desc: "根据 JD 和简历生成模拟面试题目序列。Agent/LLM 方向从知识库拉取真实面试题；其他方向（Go/Java/C++/Python/前端/系统设计等）告知 LLM 自行生成题目",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"jdText":     {Type: schema.String, Desc: "JD内容（可选）"},
				"resumeText": {Type: schema.String, Desc: "简历内容（可选）"},
				"dimension":  {Type: schema.String, Desc: "面试方向。Agent方向：agent / architecture / engineering / model / rag / multi-agent / evaluation / full-stack。其他方向自由填写如 go / java / c++ / python / 前端 / 系统设计 等"},
				"difficulty": {Type: schema.String, Desc: "难度（默认 medium）", Enum: []string{"easy", "medium", "hard"}},
				"count":      {Type: schema.Number, Desc: "题目数量（默认 5）"},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			dimension := getStr(input, "dimension", "mixed")
			difficulty := getStr(input, "difficulty", "medium")
			count := getInt(input, "count", 5)
			if count < 1 {
				count = 1
			} else if count > 8 {
				count = 8
			}

			type qResult struct {
				Index      int    `json:"index"`
				Question   string `json:"question"`
				Dimension  string `json:"dimension"`
				Difficulty string `json:"difficulty"`
				Evidence   string `json:"evidence,omitempty"`
			}

			if resumeText := getStr(input, "resumeText"); resumeText != "" {
				personalized := BuildResumeInterviewQuestions(resumeText, count)
				if len(personalized) > 0 {
					results := make([]qResult, 0, len(personalized))
					questions := make([]string, 0, len(personalized))
					for index, question := range personalized {
						results = append(results, qResult{
							Index: index + 1, Question: question.Question, Dimension: question.Dimension,
							Difficulty: difficulty, Evidence: question.Evidence,
						})
						questions = append(questions, question.Question)
					}
					if ctx.OnInterviewQuestions != nil {
						ctx.OnInterviewQuestions(questions)
					}
					data, _ := json.Marshal(map[string]interface{}{
						"source": "resume_evidence", "dimension": dimension, "difficulty": difficulty,
						"totalQuestions": len(results), "question": results,
						"instruction": "每次只问一道题。追问必须围绕 evidence 核验职责边界、技术决策和量化结果，不得补写简历中不存在的事实。",
					})
					return ToolResult{Success: true, Output: string(data)}
				}
			}

			// 知识库维度：从知识库拉取真实面试题
			kbDims := map[string]bool{
				"architecture": true, "engineering": true, "model": true,
				"rag": true, "multi-agent": true, "evaluation": true, "full-stack": true,
			}
			if dimension == "agent" || kbDims[dimension] {
				var entries []KnowledgeEntry
				if dimension == "agent" {
					for _, dim := range []string{"architecture", "engineering", "multi-agent"} {
						sub, _ := SearchKnowledgeQuestions(dim, count)
						entries = append(entries, sub...)
						if len(entries) >= count {
							break
						}
					}
					if len(entries) > count {
						entries = entries[:count]
					}
				} else {
					entries, _ = SearchKnowledgeQuestions(dimension, count)
				}

				if len(entries) > 0 {
					results := make([]qResult, 0, len(entries))
					for i, e := range entries {
						q := e.Question
						if q == "" {
							q = e.Title
						}
						dim := e.Dimension
						if dim == "" {
							dim = dimension
						}
						results = append(results, qResult{i + 1, q, dim, difficulty, ""})
					}
					if ctx.OnInterviewQuestions != nil {
						qs := make([]string, len(results))
						for i, r := range results {
							qs[i] = r.Question
						}
						ctx.OnInterviewQuestions(qs)
					}
					data, _ := json.Marshal(map[string]interface{}{
						"source":         "knowledge_base",
						"dimension":      dimension,
						"difficulty":     difficulty,
						"totalQuestions": len(results),
						"question":       results,
						"tips":           []string{"每道题用 STAR 法则组织回答", "技术题先给结论再展开", "主动提到踩坑经验和量化结果"},
					})
					return ToolResult{Success: true, Output: string(data)}
				}
				// 知识库该维度无题目，回退到 hardcoded 题
				dimension = "technical"
			}

			// 行为面：通用题（所有方向适用）
			if dimension == "behavioral" {
				type question struct {
					Q    string
					Dim  string
					Diff string
				}
				behavioral := []question{
					{"说一个你和团队意见不一致的例子，最终怎么解决的？", "behavioral", "medium"},
					{"你是怎么在紧急 deadline 下保证交付质量的？", "behavioral", "easy"},
					{"你是怎么快速学习一个新技术领域的？举个最近的例子", "behavioral", "easy"},
					{"你如何评估自己的技术成长？最近半年最大的提升是什么？", "behavioral", "easy"},
				}
				results := make([]qResult, len(behavioral))
				for i, q := range behavioral {
					results[i] = qResult{i + 1, q.Q, q.Dim, q.Diff, ""}
				}
				if ctx.OnInterviewQuestions != nil {
					qs := make([]string, len(results))
					for i, r := range results {
						qs[i] = r.Question
					}
					ctx.OnInterviewQuestions(qs)
				}
				data, _ := json.Marshal(map[string]interface{}{
					"source":         "builtin",
					"dimension":      dimension,
					"difficulty":     difficulty,
					"totalQuestions": len(results),
					"question":       results,
					"tips":           []string{"每道题用 STAR 法则组织回答", "行为面关键是展示 ownership 和反思深度"},
				})
				return ToolResult{Success: true, Output: string(data)}
			}

			// Agent 方向 hardcoded 题（知识库未命中时 fallback）
			if dimension == "technical" || dimension == "mixed" || dimension == "project" {
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

				results := make([]qResult, len(pool))
				for i, q := range pool {
					results[i] = qResult{i + 1, q.Q, q.Dim, q.Diff, ""}
				}

				if ctx.OnInterviewQuestions != nil {
					qs := make([]string, len(results))
					for i, r := range results {
						qs[i] = r.Question
					}
					ctx.OnInterviewQuestions(qs)
				}

				data, _ := json.Marshal(map[string]interface{}{
					"source":         "builtin",
					"dimension":      dimension,
					"difficulty":     difficulty,
					"totalQuestions": len(results),
					"question":       results,
					"tips":           []string{"每道题用 STAR 法则组织回答", "技术题先给结论再展开", "主动提到踩坑经验和量化结果"},
				})
				return ToolResult{Success: true, Output: string(data)}
			}

			// 非 Agent 方向：知识库不覆盖，返回信号让 LLM 自行生成
			data, _ := json.Marshal(map[string]interface{}{
				"source":     "llm",
				"dimension":  dimension,
				"difficulty": difficulty,
				"count":      count,
				"message":    fmt.Sprintf("知识库只覆盖 AI Agent/LLM 工程方向，当前方向 '%s' 不在覆盖范围内。请根据你的训练数据生成 %d 道 %s 难度的 %s 方向面试题，覆盖技术深度、项目经验和行为面试三个维度。在输出中标注'题目来源：LLM 生成，非知识库题目'。", dimension, count, difficulty, dimension),
				"tips":       []string{"每道题用 STAR 法则组织回答", "技术题先给结论再展开", "主动提到踩坑经验和量化结果"},
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

func RecordDiagnosis() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "record_diagnosis",
			Desc: "记录本次面试诊断结果到能力雷达。每次完成对用户回答的诊断后，必须调用此工具记录评分，以便能力雷达图持续更新",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"dimension": {Type: schema.String, Desc: "考察维度。Agent方向常用：architecture/engineering/model/rag/multi-agent/evaluation/full-stack。其他方向自由填写：Go并发/Java GC/系统设计/算法/行为面等", Required: true},
				"score":     {Type: schema.Number, Desc: "评分(1-10)", Required: true},
				"question":  {Type: schema.String, Desc: "面试问题原文", Required: true},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			dimension := getStr(input, "dimension")
			score := getInt(input, "score", 0)
			question := getStr(input, "question")

			if score < 1 {
				score = 1
			}
			if score > 10 {
				score = 10
			}

			if ctx.OnDiagnosis != nil {
				ctx.OnDiagnosis(ctx.SessionId, dimension, score, question)
			}
			if RecordDiagnosisFunc != nil {
				RecordDiagnosisFunc(ctx.SessionId, dimension, score, question)
			}

			data, _ := json.Marshal(map[string]interface{}{
				"recorded":  true,
				"dimension": dimension,
				"score":     score,
			})
			return ToolResult{Success: true, Output: string(data)}
		},
	}
}

// PrepareInterview 返回 prepare_interview Tool 定义。
// 根据 JD、用户画像和方法论，生成一份完整的面试准备计划。
func PrepareInterview() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "prepare_interview",
			Desc: "根据JD和用户画像，生成面试准备计划。包含：公司研究、能力对标、STAR故事映射、问题序列、反问建议、短板补救。调用后请先调 search_knowledge 获取方法论，再按模板逐项生成计划",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"jd":        {Type: schema.String, Desc: "目标JD全文（可选）"},
				"resume":    {Type: schema.String, Desc: "简历全文（可选）"},
				"direction": {Type: schema.String, Desc: "面试方向", Enum: []string{"agent", "backend", "data", "algorithm"}},
				"company":   {Type: schema.String, Desc: "目标公司名称（可选）"},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			jd := getStr(input, "jd")
			direction := getStr(input, "direction", "agent")
			company := getStr(input, "company")

			template := buildInterviewPrepTemplate(jd, direction, company)
			return ToolResult{Success: true, Output: template}
		},
	}
}

// buildInterviewPrepTemplate 构建面试准备计划指令模板。
func buildInterviewPrepTemplate(jd, direction, company string) string {
	var sb strings.Builder

	sb.WriteString("【面试准备计划生成指令】\n")
	sb.WriteString("请根据以下信息生成一份完整的面试准备计划。\n\n")

	// 获取方法论
	sb.WriteString("## 第一步：获取方法论（必须）\n")
	sb.WriteString("请先调用 search_knowledge 获取以下方法论：\n")
	sb.WriteString("- search_knowledge(query=\"面试准备\", dimension=\"coaching\") → 获取常见棘手问题模板和反问问题库\n")
	sb.WriteString("- search_knowledge(query=\"面试辅导\", dimension=\"coaching\") → 获取回答层次诊断和STAR方法论\n")
	sb.WriteString("- search_knowledge(query=\"行为面试\", dimension=\"coaching\") → 获取STAR评分标准和核心故事映射\n\n")

	// JD
	if strings.TrimSpace(jd) != "" {
		keywords := ExtractKeywords(jd)
		sb.WriteString("## 目标 JD\n")
		sb.WriteString(jd)
		sb.WriteString("\n\n")
		if len(keywords) > 0 {
			sb.WriteString("## JD 关键词\n")
			sb.WriteString(strings.Join(keywords, "、"))
			sb.WriteString("\n\n")
		}
	}

	// 方向
	sb.WriteString("## 面试方向\n")
	sb.WriteString(direction)
	sb.WriteString("\n\n")

	// 公司
	if company != "" {
		sb.WriteString("## 目标公司\n")
		sb.WriteString(company)
		sb.WriteString("\n\n")
	}

	// 数据来源
	sb.WriteString("## 数据来源\n")
	sb.WriteString("所有内容必须来自系统注入的【用户结构化画像】和【历史记忆】。禁止编造。\n\n")

	// 输出结构
	sb.WriteString("## 输出格式\n")
	sb.WriteString("直接按以下结构输出完整面试准备计划，使用 rich JSON 输出结构化内容（表格/矩阵），Markdown 输出文字内容：\n\n")
	sb.WriteString("### 一、公司研究\n")
	sb.WriteString("- 公司核心业务与技术栈\n")
	sb.WriteString("- 面试流程（几轮、每轮侧重）\n")
	sb.WriteString("- 需要提前了解的关键信息\n\n")
	sb.WriteString("### 二、能力对标\n")
	sb.WriteString("用 rich match_matrix 输出：JD 要求 | 我的匹配度 | 准备策略\n\n")
	sb.WriteString("### 三、STAR 故事映射\n")
	sb.WriteString("用 rich table 输出：面试常见问题 | 用哪个故事回答 | 故事要点\n")
	sb.WriteString("根据方法论准备 5 个核心故事。\n\n")
	sb.WriteString("### 四、面试问题序列\n")
	sb.WriteString("生成 5-8 道个性化问题，按难度递进：\n")
	sb.WriteString("- 热身题（1-2 道）\n")
	sb.WriteString("- 技术题（2-3 道，与 JD 相关）\n")
	sb.WriteString("- 行为题（1-2 道）\n")
	sb.WriteString("- 系统设计/架构题（1 道）\n\n")
	sb.WriteString("### 五、反问面试官\n")
	sb.WriteString("根据方法论准备 3-5 个高质量反问：\n")
	sb.WriteString("- 关于岗位的问题\n")
	sb.WriteString("- 关于团队的问题\n")
	sb.WriteString("- 关于技术的问题\n\n")
	sb.WriteString("### 六、短板补救\n")
	sb.WriteString("用 rich table 输出：短板 | 重要性 | 补救方案 | 预计时间\n")
	sb.WriteString("按优先级排序。\n\n")
	sb.WriteString("注意：直接输出计划内容，不要额外解释。")

	return sb.String()
}
