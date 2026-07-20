package provider

import (
	"context"
	queryengine "MyOfferPilot/src/query-engine"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) Name() string {
	return "mock"
}

func (p *MockProvider) Stream(params queryengine.StreamParams) <-chan queryengine.StreamEvent {
	events := make(chan queryengine.StreamEvent)

	go func() {
		defer close(events)

		var userContent string
		if len(params.Messages) > 0 {
			lastMsg := params.Messages[len(params.Messages)-1]
			if lastMsg.Content != nil {
				userContent = *lastMsg.Content
			}
		}

		hasTools := len(params.Tools) > 0

		response := p.generateResponse(userContent, params.Messages, hasTools, params.Tools)

		if response.Type == "tool_call" {
			tcID := fmt.Sprintf("mock_tc_%d", time.Now().UnixMilli())
			events <- &queryengine.ToolUseStartEvent{ID: tcID, Name: response.ToolName}

			inputJSON, _ := json.Marshal(response.ToolInput)
			events <- &queryengine.ToolUseDeltaEvent{Input: string(inputJSON)}

			events <- &queryengine.ToolUseEndEvent{}

			events <- &queryengine.MessageEndEvent{
				Usage:      queryengine.TokenUsage{InputTokens: 100, OutputTokens: 50},
				StopReason: queryengine.StopReasonToolUse,
			}
		} else {
			chunks := p.chunkText(response.Text)
			for _, chunk := range chunks {
				events <- &queryengine.TextDeltaEvent{Content: chunk}
				time.Sleep(15 * time.Millisecond)
			}

			events <- &queryengine.MessageEndEvent{
				Usage:      queryengine.TokenUsage{InputTokens: 100, OutputTokens: int(float64(len(response.Text)) / 4)},
				StopReason: queryengine.StopReasonEndTurn,
			}
		}
	}()

	return events
}

func (p *MockProvider) CountTokens(messages []queryengine.Message, _tools []queryengine.ToolSchema, _model string) (int, error) {
	total := 0
	for _, m := range messages {
		if m.Content != nil {
			total += len(*m.Content)
		}
	}
	return total / 4, nil
}

func (p *MockProvider) Validate(ctx context.Context) error {
	return nil
}

type mockResponse struct {
	Type      string
	Text      string
	ToolName  string
	ToolInput map[string]interface{}
}

func (p *MockProvider) generateResponse(userContent string, messages []queryengine.Message, hasTools bool, tools []queryengine.ToolSchema) mockResponse {
	lower := strings.ToLower(userContent)

	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Role == queryengine.MessageRoleTool {
			if lastMsg.Content != nil {
				return mockResponse{Type: "text", Text: p.generateFromToolResult(*lastMsg.Content, messages)}
			}
		}
	}

	if hasTools && len(tools) > 0 {
		if (strings.Contains(lower, "jd") || strings.Contains(lower, "职位") || strings.Contains(lower, "岗位")) &&
			len(userContent) > 100 && (strings.Contains(lower, "分析") || strings.Contains(lower, "要求") || strings.Contains(lower, "职责")) {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "analyze_jd",
				ToolInput: map[string]interface{}{"jdText": userContent},
			}
		}

		if (strings.Contains(lower, "简历") || strings.Contains(lower, "resume")) && (strings.Contains(lower, "jd") || strings.Contains(lower, "匹配") || strings.Contains(lower, "岗位")) {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "match_resume_jd",
				ToolInput: map[string]interface{}{"resumeText": userContent, "jdText": userContent},
			}
		}

		if (strings.Contains(lower, "简历") || strings.Contains(lower, "resume")) && (strings.Contains(lower, "优化") || strings.Contains(lower, "修改") || strings.Contains(lower, "改")) {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "optimize_resume",
				ToolInput: map[string]interface{}{"section": "full", "content": userContent},
			}
		}

		if strings.Contains(lower, "实时面试") || strings.Contains(lower, "面试模拟") || strings.Contains(lower, "模拟开始") || strings.Contains(lower, "开始模拟") {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "realtime_interview",
				ToolInput: map[string]interface{}{"action": "start"},
			}
		}

		if strings.Contains(lower, "模拟面试") || strings.Contains(lower, "mock interview") || strings.Contains(lower, "出题") {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "mock_interview",
				ToolInput: map[string]interface{}{"dimension": "mixed", "difficulty": "medium", "count": 5},
			}
		}

		if strings.Contains(lower, "诊断") || strings.Contains(lower, "题目") || strings.Contains(lower, "我的回答") {
			question := p.extractQuestion(userContent)
			answer := p.extractAnswer(userContent)
			if question != "" && answer != "" {
				return mockResponse{
					Type:      "tool_call",
					ToolName:  "diagnose_answer",
					ToolInput: map[string]interface{}{"question": question, "answer": answer},
				}
			}
		}

		if strings.Contains(lower, "搜索") || strings.Contains(lower, "查找") || strings.Contains(lower, "知识库") {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "search_knowledge",
				ToolInput: map[string]interface{}{"query": userContent[:min(len(userContent), 50)], "limit": 5},
			}
		}

		if strings.Contains(lower, "维度") || strings.Contains(lower, "分类") {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "list_dimensions",
				ToolInput: map[string]interface{}{},
			}
		}

		if strings.Contains(lower, "追问") || strings.Contains(lower, "深入") {
			return mockResponse{
				Type:      "tool_call",
				ToolName:  "generate_followup",
				ToolInput: map[string]interface{}{"question": userContent, "answer": "", "depth": "medium"},
			}
		}
	}

	return mockResponse{Type: "text", Text: p.generateTextResponse(userContent)}
}

func (p *MockProvider) generateTextResponse(input string) string {
	lower := strings.ToLower(input)

	if strings.Contains(lower, "你好") || strings.Contains(lower, "hello") || strings.Contains(lower, "hi") {
		return `你好！我是 OfferPilot，你的全链路求职辅导 Agent。

我可以帮你：
1. **JD 分析** — 贴入职位描述，我帮你拆解要求和准备重点
2. **简历优化** — 贴入简历，我给出量化、结构、关键词优化建议
3. **简历-JD 匹配** — 找出差距项和包装方向
4. **面试诊断** — 输入面试题 + 你的回答，输出评分和改进建议
5. **模拟面试** — 生成个性化面试题序列

试试看，输入一道你想练习的题目吧。`
	}

	return `收到你的输入。请用以下格式让我帮你诊断：

**题目**：（面试问题）
**我的回答**：（你的作答内容）

或者直接说"帮我诊断一下 XXX 的回答"，我会调用诊断工具分析。`
}

func (p *MockProvider) generateFromToolResult(toolResult string, messages []queryengine.Message) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(toolResult), &data); err != nil {
		return `已处理完成。你还想继续吗？可以贴入 JD、简历，或者直接开始面试练习。`
	}

	if _, ok := data["dimensions"]; ok {
		dims := data["dimensions"].([]interface{})
		var res strings.Builder
		res.WriteString(`当前知识库包含 **7 个考察维度**：

`)
		for i, d := range dims {
			dim := d.(map[string]interface{})
			res.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, dim["name"], dim["id"]))
		}
		res.WriteString(`

你想从哪个维度开始练习？`)
		return res.String()
	}

	if results, ok := data["results"]; ok {
		rs := results.([]interface{})
		if len(rs) == 0 {
			return fmt.Sprintf(`在知识库中搜索了"%s"，暂未找到完全匹配的题目。你可以直接输入完整的面试题，我来帮你分析。`, data["query"])
		}
		var items strings.Builder
		for i, r := range rs[:min(len(rs), 3)] {
			if i > 0 {
				items.WriteString("\n")
			}
			result := r.(map[string]interface{})
			items.WriteString(fmt.Sprintf("- **%s**：%s", result["title"], result["question"]))
		}
		return fmt.Sprintf(`找到以下相关题目：

%s

需要我对其中某道题进行详细讲解吗？`, items.String())
	}

	if _, ok := data["score"]; ok {
		return p.buildDiagnosisResponse(data, messages)
	}

	if _, ok := data["followups"]; ok {
		fs := data["followups"].([]interface{})
		var qs strings.Builder
		for i, f := range fs {
			if i > 0 {
				qs.WriteString("\n")
			}
			qs.WriteString(fmt.Sprintf("%d. %s", i+1, f))
		}
		hint := ""
		if h, ok := data["hint"]; ok {
			hint = fmt.Sprintf("\n> 💡 %s", h)
		}
		return fmt.Sprintf(`## 面试官可能的追问

%s%s

你要试着回答其中一个吗？`, qs.String(), hint)
	}

	if _, ok := data["techStack"]; ok {
		var res strings.Builder
		res.WriteString(`## JD 分析结果

`)
		if summary, ok := data["summary"].(map[string]interface{}); ok {
			res.WriteString(fmt.Sprintf("**职级判断**：%s | **经验要求**：%s年 | **学历**：%s\n\n",
				getString(summary, "level", "未知"),
				getString(summary, "yearsRequired", "未明确"),
				getString(summary, "education", "未明确")))
		}
		techStack := data["techStack"].(map[string]interface{})
		if required, ok := techStack["required"].([]interface{}); ok && len(required) > 0 {
			res.WriteString("### 必备技术栈\n")
			for i, r := range required {
				if i > 0 {
					res.WriteString("、")
				}
				res.WriteString(fmt.Sprintf("%s", r))
			}
			res.WriteString("\n\n")
		}
		if niceToHave, ok := techStack["niceToHave"].([]interface{}); ok && len(niceToHave) > 0 {
			res.WriteString("### 加分项\n")
			for i, r := range niceToHave {
				if i > 0 {
					res.WriteString("、")
				}
				res.WriteString(fmt.Sprintf("%s", r))
			}
			res.WriteString("\n\n")
		}
		if interviewPrep, ok := data["interviewPrep"].([]interface{}); ok && len(interviewPrep) > 0 {
			res.WriteString("### 面试准备重点\n")
			for i, p := range interviewPrep {
				res.WriteString(fmt.Sprintf("%d. %s\n", i+1, p))
			}
			res.WriteString("\n")
		}
		if keyInsights, ok := data["keyInsights"].([]interface{}); ok && len(keyInsights) > 0 {
			res.WriteString("### 关键洞察\n")
			for _, k := range keyInsights {
				res.WriteString(fmt.Sprintf("- %s\n", k))
			}
		}
		res.WriteString("\n---\n要我帮你把简历和这个 JD 做匹配分析吗？")
		return res.String()
	}

	if _, ok := data["matchRate"]; ok {
		var res strings.Builder
		res.WriteString(fmt.Sprintf(`## 简历-JD 匹配分析

### 匹配度：%s

**%s**

`, data["matchRate"], data["verdict"]))
		if matched, ok := data["matched"].([]interface{}); ok && len(matched) > 0 {
			res.WriteString("✅ 匹配项：")
			for i, m := range matched[:min(len(matched), 5)] {
				if i > 0 {
					res.WriteString("、")
				}
				res.WriteString(fmt.Sprintf("%s", m))
			}
			res.WriteString("\n\n")
		}
		if missing, ok := data["missing"].([]interface{}); ok && len(missing) > 0 {
			res.WriteString("❌ 缺失项：")
			for i, m := range missing[:min(len(missing), 5)] {
				if i > 0 {
					res.WriteString("、")
				}
				res.WriteString(fmt.Sprintf("%s", m))
			}
			res.WriteString("\n\n")
		}
		if suggestions, ok := data["suggestions"].([]interface{}); ok && len(suggestions) > 0 {
			res.WriteString("### 优化建议\n")
			for i, s := range suggestions {
				res.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
			}
		}
		return res.String()
	}

	if _, ok := data["diagnosis"]; ok {
		_, hasPrinciples := data["principles"]
		if hasPrinciples {
			var res strings.Builder
			diagnosis := data["diagnosis"].(map[string]interface{})
			res.WriteString(fmt.Sprintf(`## 简历优化建议

**段落评分**：%s/10

`, diagnosis["score"]))
			if issues, ok := diagnosis["issues"].([]interface{}); ok && len(issues) > 0 {
				res.WriteString("### 发现的问题\n")
				for _, i := range issues {
					res.WriteString(fmt.Sprintf("- %s\n", i))
				}
				res.WriteString("\n")
			}
			if suggestions, ok := data["suggestions"].([]interface{}); ok && len(suggestions) > 0 {
				res.WriteString("### 改进建议\n")
				for i, s := range suggestions {
					res.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
				}
				res.WriteString("\n")
			}
			if rewriteExample, ok := data["rewriteExample"]; ok && rewriteExample != nil {
				res.WriteString("### 改写示例\n")
				res.WriteString(fmt.Sprintf("```\n%s\n```\n\n", rewriteExample))
			}
			if principles, ok := data["principles"].([]interface{}); ok && len(principles) > 0 {
				res.WriteString("### 写作原则\n")
				for _, p := range principles {
					res.WriteString(fmt.Sprintf("- %s\n", p))
				}
			}
			return res.String()
		}
	}

	if _, ok := data["sessionId"]; ok {
		_, hasTTS := data["ttsText"]
		if hasTTS {
			var res strings.Builder
			state := getString(data, "state", "")
			if state == "questioning" {
				progress := getString(data, "progress", "")
				res.WriteString(fmt.Sprintf(`## 🎙️ 面试模拟

**面试官提问**：%s

进度：%s

---
请用文字输入你的回答，我会实时分析缺陷。`, data["currentQuestion"], progress))
			} else if state == "analyzed" {
				res.WriteString(fmt.Sprintf(`## 📊 实时缺陷分析

%s

`, data["summary"]))
				if defects, ok := data["defects"].([]interface{}); ok && len(defects) > 0 {
					res.WriteString(`| 类型 | 严重度 | 建议 |
|------|--------|------|
`)
					for _, d := range defects {
						defect := d.(map[string]interface{})
						res.WriteString(fmt.Sprintf("| %s | %s | %s |\n", defect["type"], defect["severity"], defect["suggestion"]))
					}
				}
				res.WriteString("\n输入\"下一题\"继续，或\"总结\"查看报告。")
			} else if state == "finished" {
				res.WriteString("面试模拟已完成全部题目。输入\"总结\"获取综合评价报告。")
			} else if _, ok := data["overallScore"]; ok {
				res.WriteString(fmt.Sprintf(`## 📋 面试模拟总结

**综合评分**：%s/10
**总缺陷数**：%d

`, data["overallScore"], data["totalDefects"]))
				if topIssues, ok := data["topIssues"].([]interface{}); ok && len(topIssues) > 0 {
					res.WriteString("**主要问题**：")
					for i, t := range topIssues {
						if i > 0 {
							res.WriteString("、")
						}
						res.WriteString(fmt.Sprintf("%s", t))
					}
					res.WriteString("\n")
				}
			}
			return res.String()
		}
	}

	if _, ok := data["questions"]; ok {
		_, hasTotal := data["totalQuestions"]
		if hasTotal {
			var res strings.Builder
			res.WriteString(fmt.Sprintf(`## 模拟面试题（%d 道）

`, int64(data["totalQuestions"].(float64))))
			if questions, ok := data["questions"].([]interface{}); ok {
				for _, q := range questions {
					question := q.(map[string]interface{})
					res.WriteString(fmt.Sprintf("**Q%d**（%s / %s）：%s\n\n",
						int64(question["index"].(float64)),
						question["dimension"],
						question["difficulty"],
						question["question"]))
				}
			}
			if tips, ok := data["tips"].([]interface{}); ok && len(tips) > 0 {
				res.WriteString("---\n### 作答技巧\n")
				for _, t := range tips {
					res.WriteString(fmt.Sprintf("- %s\n", t))
				}
			}
			res.WriteString("\n选一道开始作答，我会实时诊断你的回答。")
			return res.String()
		}
	}

	return `已处理完成。你还想继续吗？可以贴入 JD、简历，或者直接开始面试练习。`
}

func (p *MockProvider) buildDiagnosisResponse(data map[string]interface{}, messages []queryengine.Message) string {
	score := data["score"].(map[string]interface{})
	gaps := getStringSlice(data, "gaps")
	suggestions := getStringSlice(data, "suggestions")
	breakdown := data["breakdown"].(map[string]interface{})

	overallScore := int64(score["overall"].(float64))
	scoreEmoji := "🔴"
	if overallScore >= 7 {
		scoreEmoji = "🟢"
	} else if overallScore >= 5 {
		scoreEmoji = "🟡"
	}

	var res strings.Builder
	res.WriteString(fmt.Sprintf(`## 诊断结果

### %s 总分：%d / %d

`, scoreEmoji, overallScore, int64(score["max"].(float64))))

	res.WriteString(`| 维度 | 得分 | 评价 |
|------|------|------|
`)

	if td, ok := breakdown["technicalDepth"]; ok {
		d := int64(td.(float64))
		eval := "❌ 偏浅"
		if d >= 7 {
			eval = "✅ 有深度"
		} else if d >= 5 {
			eval = "⚠️ 一般"
		}
		res.WriteString(fmt.Sprintf("| 技术深度 | %d/10 | %s |\n", d, eval))
	}
	if st, ok := breakdown["structure"]; ok {
		d := int64(st.(float64))
		eval := "❌ 缺乏层次"
		if d >= 7 {
			eval = "✅ 清晰"
		} else if d >= 5 {
			eval = "⚠️ 松散"
		}
		res.WriteString(fmt.Sprintf("| 表达结构 | %d/10 | %s |\n", d, eval))
	}
	if pe, ok := breakdown["practicalExperience"]; ok {
		d := int64(pe.(float64))
		eval := "❌ 缺少案例"
		if d >= 7 {
			eval = "✅ 有实战"
		} else if d >= 5 {
			eval = "⚠️ 偏理论"
		}
		res.WriteString(fmt.Sprintf("| 实践经验 | %d/10 | %s |\n", d, eval))
	}
	if co, ok := breakdown["completeness"]; ok {
		d := int64(co.(float64))
		eval := "❌ 不够完整"
		if d >= 7 {
			eval = "✅ 全面"
		} else if d >= 5 {
			eval = "⚠️ 有遗漏"
		}
		res.WriteString(fmt.Sprintf("| 完整性 | %d/10 | %s |\n", d, eval))
	}

	if len(gaps) > 0 {
		res.WriteString("\n### 主要差距\n\n")
		for _, g := range gaps {
			res.WriteString(fmt.Sprintf("- %s\n", g))
		}
	}

	if len(suggestions) > 0 {
		res.WriteString("\n\n### 改进建议\n\n")
		for i, s := range suggestions {
			res.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
	}

	res.WriteString("\n\n---\n要我针对这道题生成追问，还是换一道题练习？")

	return res.String()
}

func (p *MockProvider) extractQuestion(text string) string {
	re := regexp.MustCompile(`题目[：:]\s*(.+?)(?:\n\n|我的回答|$)`)
	if matches := re.FindStringSubmatch(text); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	re2 := regexp.MustCompile(`诊断.*?[：:]\s*(.+?)(?:\n|$)`)
	if matches := re2.FindStringSubmatch(text); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func (p *MockProvider) extractAnswer(text string) string {
	re := regexp.MustCompile(`(?:我的)?回答[：:]\s*(.+)$`)
	if matches := re.FindStringSubmatch(text); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func (p *MockProvider) chunkText(text string) []string {
	chunks := []string{}
	i := 0
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i < len(text) {
		size := 4 + rng.Intn(12)
		end := min(i+size, len(text))
		chunks = append(chunks, text[i:end])
		i = end
	}
	return chunks
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getString(m map[string]interface{}, key, def string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return def
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key]; ok && v != nil {
		if slice, ok := v.([]interface{}); ok {
			result := []string{}
			for _, item := range slice {
				result = append(result, fmt.Sprintf("%v", item))
			}
			return result
		}
	}
	return []string{}
}
