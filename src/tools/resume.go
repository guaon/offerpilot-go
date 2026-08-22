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