package tool

import (
	"regexp"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ExtractKeywords 从文本中提取技术关键词。
func ExtractKeywords(text string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`TypeScript|JavaScript|Python|Go|Rust|Java|C\+\+`),
		regexp.MustCompile(`React|Vue|Angular|Next\.?js|Node\.?js|Express`),
		regexp.MustCompile(`Docker|Kubernetes|K8s|CI\/CD|GitHub Actions`),
		regexp.MustCompile(`PostgreSQL|MySQL|Redis|MongoDB|SQLite|Elasticsearch`),
		regexp.MustCompile(`LLM|GPT|Claude|Agent|RAG|embedding|向量`),
		regexp.MustCompile(`分布式|微服务|gRPC|REST|GraphQL|WebSocket|SSE`),
		regexp.MustCompile(`TDD|单元测试|E2E|集成测试|自动化测试`),
		regexp.MustCompile(`Webpack|Vite|ESBuild|Tailwind|shadcn`),
		regexp.MustCompile(`AWS|GCP|Azure|阿里云|腾讯云`),
		regexp.MustCompile(`机器学习|深度学习|NLP|CV|MLOps|训练|微调`),
		regexp.MustCompile(`Milvus|Qdrant|Pinecone|Faiss|向量数据库`),
		regexp.MustCompile(`Prompt|CoT|ReAct|Tool Use|Function Calling`),
		regexp.MustCompile(`架构设计|系统设计|高可用|高并发|性能优化`),
		regexp.MustCompile(`数据处理|ETL|数据管道|Spark|Flink`),
	}

	keywords := make(map[string]bool)
	for _, pattern := range patterns {
		matches := pattern.FindAllString(text, -1)
		for _, m := range matches {
			keywords[strings.ToLower(strings.TrimSpace(m))] = true
		}
	}

	cnMatches := regexp.MustCompile(`[一-鿿]{2,6}(?:系统|架构|服务|引擎|平台|框架|协议|模型|算法|能力)`).FindAllString(text, -1)
	for _, m := range cnMatches {
		keywords[m] = true
	}

	result := make([]string, 0, len(keywords))
	for k := range keywords {
		result = append(result, k)
	}
	return result
}

// DetectLevel 从 JD 文本检测目标职级。
func DetectLevel(jd string) string {
	if regexp.MustCompile(`[5五]年以上|资深|高级|P[67]`).MatchString(jd) {
		return "高级工程师 (P6-P7)"
	}
	if regexp.MustCompile(`[3三]年以上|中级|P5`).MatchString(jd) {
		return "中级工程师 (P5)"
	}
	if regexp.MustCompile(`[8八]年以上|专家|架构师|P[89]`).MatchString(jd) {
		return "专家/架构师 (P8+)"
	}
	return "工程师"
}

// DetectFocus 从 JD 文本检测技术方向。
func DetectFocus(jd string) []string {
	areas := make([]string, 0)
	if regexp.MustCompile(`Agent|LLM|大模型|GPT|Claude`).MatchString(jd) {
		areas = append(areas, "AI/LLM 工程")
	}
	if regexp.MustCompile(`架构|系统设计|分布式`).MatchString(jd) {
		areas = append(areas, "系统架构")
	}
	if regexp.MustCompile(`全栈|前端|后端|Web`).MatchString(jd) {
		areas = append(areas, "全栈开发")
	}
	if regexp.MustCompile(`RAG|检索|知识库|向量`).MatchString(jd) {
		areas = append(areas, "RAG/检索")
	}
	if regexp.MustCompile(`数据|ETL|管道|分析`).MatchString(jd) {
		areas = append(areas, "数据工程")
	}
	if len(areas) == 0 {
		areas = append(areas, "软件工程")
	}
	return areas
}

// GenerateSuggestions 根据缺失关键词生成简历改进建议。
func GenerateSuggestions(missing []string, resume string) []string {
	suggestions := make([]string, 0)

	if len(missing) > 5 {
		suggestions = append(suggestions, "JD 要求的技术栈覆盖不足，建议在项目经历中补充相关技术的使用经验")
	}

	hasDocker := false
	hasDistributed := false
	hasAgent := false
	hasTest := false
	for _, kw := range missing {
		if strings.Contains(kw, "docker") || strings.Contains(kw, "k8s") || strings.Contains(kw, "kubernetes") || strings.Contains(kw, "容器") {
			hasDocker = true
		}
		if strings.Contains(kw, "分布式") || strings.Contains(kw, "高并发") || strings.Contains(kw, "高可用") {
			hasDistributed = true
		}
		if strings.Contains(kw, "agent") || strings.Contains(kw, "llm") || strings.Contains(kw, "大模型") || strings.Contains(kw, "rag") {
			hasAgent = true
		}
		if strings.Contains(kw, "测试") || strings.Contains(kw, "tdd") || strings.Contains(kw, "e2e") {
			hasTest = true
		}
	}

	if hasDocker {
		suggestions = append(suggestions, "补充容器化/部署相关经验，即使只是 Docker 单机部署也值得提及")
	}
	if hasDistributed {
		suggestions = append(suggestions, "在项目中突出系统规模（QPS、数据量、节点数），体现分布式思维")
	}
	if hasAgent {
		suggestions = append(suggestions, "突出 AI/LLM 相关实践，包括 Prompt 工程、RAG 搭建、Agent 开发")
	}
	if hasTest {
		suggestions = append(suggestions, "补充测试实践：测试覆盖率、TDD 经验、CI 自动化")
	}

	if !regexp.MustCompile(`\d+%|\d+ms|\d+QPS|\d+万`).MatchString(resume) {
		suggestions = append(suggestions, "简历中缺少量化数据，建议每个项目至少有 1-2 个数字指标")
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "匹配度良好，建议进一步强化最核心的 2-3 个技术点的深度描述")
	}

	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}
	return suggestions
}

// MatchJDResume 返回 match_jd_resume Tool 定义（LLM 语义匹配）。
// Tool 返回结构化模板，Agent 用 LLM 能力逐节填充。
func MatchJDResume() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "match_jd_resume",
			Desc: "深度对比 JD 和简历，生成完整面试准备文档。包含：JD拆解表、匹配矩阵、候选人定位、强匹配点、短板补救话术、项目推荐、自我介绍、反问问题。调用后请按返回的模板逐节输出，不要省略",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"jd":      {Type: schema.String, Desc: "职位描述全文", Required: true},
				"resume":  {Type: schema.String, Desc: "简历全文", Required: true},
				"company": {Type: schema.String, Desc: "公司名称（可选）"},
				"role":    {Type: schema.String, Desc: "目标岗位名称（可选）"},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			jd := getStr(input, "jd")
			resume := getStr(input, "resume")
			company := getStr(input, "company")
			role := getStr(input, "role")
			if strings.TrimSpace(jd) == "" || strings.TrimSpace(resume) == "" {
				return ToolResult{Success: false, Output: "JD 和简历内容均不能为空"}
			}
			if company == "" {
				company = "（从 JD 中推断）"
			}
			if role == "" {
				role = "（从 JD 中推断）"
			}

			template := buildMatchTemplate(jd, resume, company, role)
			return ToolResult{Success: true, Output: template}
		},
	}
}

// buildMatchTemplate 构建匹配矩阵模板，只返回核心内容，避免信息过载。
// Agent 首次只输出匹配矩阵 + 总结，用户选择后再展开其他维度。
func buildMatchTemplate(jd, resume, company, role string) string {
	return `【匹配指令】请分析以下 JD 和简历，只输出匹配矩阵和一段简短总结。禁止输出其他内容。

## 匹配矩阵

逐条分析，用 🟢/🟡/🔴 标注，每格必须填具体内容，禁止写"无"或"需补充"：

| JD 要求 | 匹配度 | 我的对应经历 | 可讲的故事 | 量化证据 | 面试表达策略 |
|---------|--------|-------------|-----------|---------|-------------|
| Agent 系统开发 | 🟢 强匹配 | 独立搭建过 Agent 系统 | 从0到1的 Agent 项目 | 服务数/用户量 | 强调架构设计能力 |
| 多 Agent 协作 | 🟡 可迁移 | 微服务间 gRPC/消息通信 | IM 系统多服务协调 | 7 个微服务协作 | 类比"服务编排≈Agent 协作" |
| RAG 检索 | 🔴 需补齐 | 有 ES 全文搜索经验 | 购物平台 ES 搜索引擎 | 搜索响应时间 | 坦诚"检索原理相通，向量库可快速上手" |

规则：
- 🟢 强匹配：简历有直接对应的项目经验
- 🟡 可迁移：没有直接经验，但相关技能可以迁移，说明迁移逻辑
- 🔴 需补齐：确实没有，给出具体补救方案（如"2周可完成一个 RAG Demo"）
- "面试表达策略"列必须是一句可以直接说出口的话

## 总结

匹配完成后，用 2-3 句话总结：核心优势是什么，最大短板是什么，优先准备什么。

## 引导

输出完矩阵和总结后，询问用户想深入了解哪个方面：
- "需要我帮你准备具体的面试问题吗？"
- "需要我帮你写一段针对这个岗位的自我介绍吗？"
- "需要我展开分析短板怎么补救吗？"

---
<JD>
` + jd + `
</JD>

<简历>
` + resume + `
</简历>

公司：` + company + `
岗位：` + role + `
---`
}