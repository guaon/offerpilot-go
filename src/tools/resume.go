package tool

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// SplitSections 按 # 标题或空行将简历文本拆分为段落。
func SplitSections(content string) []map[string]string {
	lines := strings.Split(content, "\n")
	sections := make([]map[string]string, 0)
	var current map[string]string

	for _, line := range lines {
		match := regexp.MustCompile(`^#{1,3}\s+(.+)`).FindStringSubmatch(line)
		if match != nil {
			if current != nil {
				sections = append(sections, current)
			}
			current = map[string]string{"title": match[1], "text": ""}
		} else if current != nil {
			if current["text"] == "" {
				current["text"] = line
			} else {
				current["text"] += "\n" + line
			}
		} else if strings.TrimSpace(line) != "" {
			current = map[string]string{"title": "项目经历", "text": line}
		}
	}

	if current != nil {
		sections = append(sections, current)
	}

	if len(sections) == 0 {
		paragraphs := regexp.MustCompile(`\n{2,}`).Split(content, -1)
		for i, p := range paragraphs {
			if strings.TrimSpace(p) != "" {
				sections = append(sections, map[string]string{"title": fmt.Sprintf("段落 %d", i+1), "text": strings.TrimSpace(p)})
			}
		}
	}

	return sections
}

// AnalyzeSection 对单个简历段落进行规则诊断，返回评分、问题、建议。
func AnalyzeSection(section map[string]string) map[string]interface{} {
	title := section["title"]
	text := section["text"]
	issues := make([]string, 0)
	suggestions := make([]string, 0)
	score := 8

	if len(text) < 30 {
		issues = append(issues, "内容过少")
		suggestions = append(suggestions, "补充技术栈、成果和具体数据")
		score -= 2
	}

	if !regexp.MustCompile(`\d+`).MatchString(text) {
		issues = append(issues, "缺少量化数据")
		suggestions = append(suggestions, "加入性能指标：延迟、QPS、成功率、覆盖人数等")
		score -= 1
	}

	if !regexp.MustCompile(`[结果成果效果提升降低优化]`).MatchString(text) && len(text) > 50 {
		issues = append(issues, "未体现成果")
		suggestions = append(suggestions, "用 STAR 结构结尾加上 Result（成果）")
		score -= 1
	}

	if !regexp.MustCompile(`[选择|设计|架构|方案]`).MatchString(text) && len(text) > 80 {
		issues = append(issues, "未体现技术决策")
		suggestions = append(suggestions, "描述为什么选择该技术方案，体现判断力")
		score -= 1
	}

	if len(text) > 60 && !strings.Contains(text, "负责") && !strings.Contains(text, "主导") && !strings.Contains(text, "我") {
		issues = append(issues, "未突出个人贡献")
		suggestions = append(suggestions, "明确个人角色：\"我负责...\"、\"我主导了...\"")
		score -= 1
	}

	buzzwords := len(regexp.MustCompile(`精通|熟悉|了解|掌握`).FindAllString(text, -1))
	if buzzwords >= 3 {
		issues = append(issues, "技能描述太泛")
		suggestions = append(suggestions, "用项目经验佐证技能水平，而非堆砌\"精通/熟悉\"")
		score -= 1
	}

	if len(issues) == 0 {
		suggestions = append(suggestions, "继续保持，可适当补充更多量化数据")
	}

	if score < 3 {
		score = 3
	}
	if score > 10 {
		score = 10
	}

	return map[string]interface{}{
		"section":     title,
		"score":       score,
		"issues":      issues,
		"suggestions": suggestions,
	}
}

// GenerateResume 返回 generate_resume Tool 定义。
// 根据用户画像和目标 JD，生成或优化简历全文。
func GenerateResume() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "generate_resume",
			Desc: "根据用户画像和目标JD，生成或优化简历全文。支持两种模式：generate（从画像+JD生成新简历）、optimize（优化现有简历）。调用后请按返回的模板指令逐项生成简历，不要省略任何部分",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"action":    {Type: schema.String, Desc: "generate（从画像生成新简历）或 optimize（优化现有简历）", Required: true, Enum: []string{"generate", "optimize"}},
				"resume":    {Type: schema.String, Desc: "现有简历全文（optimize 模式必填）"},
				"jd":        {Type: schema.String, Desc: "目标JD全文（可选，有则定向优化）"},
				"direction": {Type: schema.String, Desc: "目标方向", Enum: []string{"agent", "backend", "data", "algorithm"}},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			action := getStr(input, "action", "generate")
			resume := getStr(input, "resume")
			jd := getStr(input, "jd")
			direction := getStr(input, "direction", "agent")

			if action == "optimize" && strings.TrimSpace(resume) == "" {
				return ToolResult{Success: false, Output: "optimize 模式需要提供现有简历内容"}
			}

			template := buildResumePrompt(action, resume, jd, direction)
			return ToolResult{Success: true, Output: template}
		},
	}
}

// buildResumePrompt 构建简历生成指令模板。
// 方法论规则存放于知识库 resume-template.md，LLM 需通过 search_knowledge 获取。
func buildResumePrompt(action, resume, jd, direction string) string {
	var sb strings.Builder

	sb.WriteString("【简历生成指令】\n")
	sb.WriteString("请根据以下信息生成/优化简历。\n\n")

	// 1. 核心规则：必须先获取模板
	sb.WriteString("## 第一步：获取模板规则（必须）\n")
	sb.WriteString("请先调用 search_knowledge(query=\"简历模板\", dimension=\"coaching\") 获取完整的简历模板规则，包括：\n")
	sb.WriteString("- 分节结构与顺序\n")
	sb.WriteString("- Profile Statement 句式（按方向选择）\n")
	sb.WriteString("- Bullet 规则（句式变化、Impact-First、量化要求）\n")
	sb.WriteString("- 页数预算与相关性加权裁剪算法\n")
	sb.WriteString("- JD 关键词嵌入规则\n")
	sb.WriteString("- 诚实规则与事实溯源\n\n")

	// 2. 模式说明
	if action == "generate" {
		sb.WriteString("## 第二步：生成模式 —— 从零生成\n")
		sb.WriteString("从系统注入的用户画像中提取所有信息，按模板规则生成完整简历。\n")
		sb.WriteString("数据不足时，缺少的字段留空标注\"待补充\"，禁止编造。\n\n")
	} else {
		sb.WriteString("## 第二步：优化模式 —— 定向优化\n")
		sb.WriteString("优化现有简历，使其更匹配目标 JD。保留核心事实，调整描述角度和侧重。\n\n")
		sb.WriteString("## 现有简历\n")
		sb.WriteString(resume)
		sb.WriteString("\n\n")
	}

	// 3. JD 关键词
	if strings.TrimSpace(jd) != "" {
		keywords := ExtractKeywords(jd)
		if len(keywords) > 0 {
			sb.WriteString("## 目标 JD 关键词（确保简历中覆盖）\n")
			sb.WriteString(strings.Join(keywords, "、"))
			sb.WriteString("\n\n")
		}
	}

	// 4. 方向
	sb.WriteString("## 目标方向\n")
	sb.WriteString(direction)
	sb.WriteString("\n\n")

	// 5. 数据来源
	sb.WriteString("## 数据来源\n")
	sb.WriteString("所有内容必须来自系统注入的【用户结构化画像】和【历史记忆】。\n")
	sb.WriteString("禁止编造数字、日期、项目名、公司名。\n\n")

	// 6. 输出格式
	sb.WriteString("## 输出格式\n")
	sb.WriteString("直接输出 Markdown 格式的简历全文，按模板规则中的分节结构组织。\n")
	sb.WriteString("输出简历正文即可，不要额外的解释或说明文字。")

	return sb.String()
}

// DiagnoseResume 返回 diagnose_resume Tool 定义。
func DiagnoseResume() *ToolDefinition {
	return &ToolDefinition{
		Schema: &schema.ToolInfo{
			Name: "diagnose_resume",
			Desc: "诊断简历文本，按段落分析问题并给出评分和改进建议。诊断维度包括：内容完整性、量化数据、成果体现、技术决策、个人贡献、技能描述质量",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"content": {Type: schema.String, Desc: "简历全文", Required: true},
			}),
		},
		RiskLevel: RiskLevelLow,
		Execute: func(input map[string]interface{}, ctx ToolContext) ToolResult {
			content := getStr(input, "content")
			if strings.TrimSpace(content) == "" {
				return ToolResult{Success: false, Output: "简历内容为空"}
			}

			sections := SplitSections(content)
			diagnosis := make([]map[string]interface{}, 0, len(sections))
			for _, section := range sections {
				diagnosis = append(diagnosis, AnalyzeSection(section))
			}

			data, _ := json.Marshal(map[string]interface{}{
				"totalSections": len(diagnosis),
				"diagnosis":     diagnosis,
			})
			return ToolResult{Success: true, Output: string(data)}
		},
	}
}