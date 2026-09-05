package tool

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type ResumeInterviewQuestion struct {
	Question  string `json:"question"`
	Dimension string `json:"dimension"`
	Evidence  string `json:"evidence"`
}

var resumeEvidenceSignal = regexp.MustCompile(`(?i)(项目|负责|主导|设计|实现|优化|架构|系统|平台|服务|性能|延迟|吞吐|并发|降|升|%|ms|秒|万|亿|PR|github)`)

func BuildResumeInterviewQuestions(resume string, count int) []ResumeInterviewQuestion {
	if count <= 0 {
		count = 5
	}
	if count > 8 {
		count = 8
	}
	evidence := extractResumeEvidence(resume, count)
	if len(evidence) == 0 {
		return nil
	}
	templates := []struct {
		dimension string
		question  string
	}{
		{"project", "简历中写到「%s」。请说明你的具体职责边界、关键技术决策和可量化结果。"},
		{"technical", "围绕简历中的「%s」，当时最大的技术难点是什么？你比较过哪些替代方案，为什么选择最终方案？"},
		{"project", "针对「%s」，如果现在重新实现一次，你会保留什么、改变什么？请结合当时的约束回答。"},
		{"behavioral", "简历中提到「%s」。请用 STAR 结构说明一次分歧、失败或风险，以及你如何推动结果。"},
		{"technical", "请画出或口述「%s」背后的核心链路，并说明性能、可靠性和可观测性是如何验证的。"},
	}
	result := make([]ResumeInterviewQuestion, 0, count)
	for index := 0; index < count; index++ {
		fact := evidence[index%len(evidence)]
		template := templates[index%len(templates)]
		result = append(result, ResumeInterviewQuestion{
			Question:  fmt.Sprintf(template.question, fact),
			Dimension: template.dimension,
			Evidence:  fact,
		})
	}
	return result
}

func extractResumeEvidence(resume string, limit int) []string {
	type candidate struct {
		text  string
		score int
	}
	seen := make(map[string]struct{})
	candidates := make([]candidate, 0)
	for _, raw := range strings.Split(strings.ReplaceAll(resume, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.TrimLeft(raw, "-*•0123456789.、 "))
		length := utf8.RuneCountInString(line)
		if length < 12 || length > 180 || strings.HasPrefix(line, "#") {
			continue
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(line), " "))
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		score := 0
		if resumeEvidenceSignal.MatchString(line) {
			score += 3
		}
		if regexp.MustCompile(`\d`).MatchString(line) {
			score += 2
		}
		if strings.ContainsAny(line, "：:") {
			score++
		}
		candidates = append(candidates, candidate{text: line, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if limit > len(candidates) {
		limit = len(candidates)
	}
	result := make([]string, 0, limit)
	for _, item := range candidates[:limit] {
		result = append(result, item.text)
	}
	return result
}
