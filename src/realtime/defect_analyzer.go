package realtime

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type AnalysisInput struct {
	Question  string
	Answer    string
	ElapsedMs int64
}

type DefectAnalyzer struct{}

func NewDefectAnalyzer() *DefectAnalyzer {
	return &DefectAnalyzer{}
}

func (da *DefectAnalyzer) Analyze(input AnalysisInput) []DefectEntry {
	question := input.Question
	answer := input.Answer
	elapsedMs := input.ElapsedMs
	defects := []DefectEntry{}
	now := time.Now().UnixMilli()

	if len(answer) < 50 {
		defects = append(defects, da.create(DefectTypeTooShort, DefectSeverityCritical, "回答过于简短，缺乏有效信息", "至少展开 2-3 个要点，每个要点一句话", now))
	}

	if len(answer) > 100 {
		hasNumberedList := regexp.MustCompile(`[1-9一二三四五六七八九十][.、)）]`).MatchString(answer)
		if !hasNumberedList && !contains(answer, "首先") && !contains(answer, "其次") {
			defects = append(defects, da.create(DefectTypeNoStructure, DefectSeverityModerate, "回答缺乏结构，一段到底", "用\"第一…第二…第三…\"或\"首先…其次…最后…\"组织", now))
		}
	}

	if len(answer) > 80 && !contains(answer, "例如") && !contains(answer, "比如") && !contains(answer, "实际") && !contains(answer, "项目") && !contains(answer, "之前") {
		defects = append(defects, da.create(DefectTypeMissingExample, DefectSeverityModerate, "缺少具体案例支撑", "加一句\"比如在我之前的项目中…\"增强说服力", now))
	}

	vagueCount := countMatches(answer, `可能|大概|好像|一些|某些|差不多`)
	if vagueCount >= 3 {
		defects = append(defects, da.create(DefectTypeTooVague, DefectSeverityModerate, "模糊表述过多（"+strconv.Itoa(vagueCount)+" 处）", "用具体数字和明确说法替换\"大概\"\"可能\"", now))
	}

	if len(answer) > 150 && !contains(answer, "因为") && !contains(answer, "原因") && !contains(answer, "本质") && !contains(answer, "底层") {
		defects = append(defects, da.create(DefectTypeNoDepth, DefectSeverityMinor, "停留在表面描述，缺少原理分析", "补充一句\"之所以这样做是因为…\"展示深度理解", now))
	}

	fillerCount := countMatches(answer, `那个|就是说|嗯|额|然后就|对吧`)
	if fillerCount >= 3 {
		defects = append(defects, da.create(DefectTypeFillerWords, DefectSeverityMinor, "口头禅/填充词过多（"+strconv.Itoa(fillerCount)+" 处）", "放慢语速，用短暂停顿替代\"嗯\"\"那个\"", now))
	}

	if elapsedMs > 15000 && len(answer) < 100 {
		defects = append(defects, da.create(DefectTypeHesitation, DefectSeverityMinor, "思考时间过长，可能让面试官觉得不熟悉", "先说一句\"这个问题我从X角度来回答\"争取思考时间", now))
	}

	questionKeywords := regexp.MustCompile(`[\u4e00-\u9fff]{2,4}`).FindAllString(question, -1)
	if len(questionKeywords) > 3 {
		relevantHits := 0
		answerLower := strings.ToLower(answer)
		for _, kw := range questionKeywords {
			if strings.Contains(answerLower, strings.ToLower(kw)) {
				relevantHits++
			}
		}
		if float64(relevantHits) < float64(len(questionKeywords))*0.2 {
			defects = append(defects, da.create(DefectTypeOffTopic, DefectSeverityCritical, "回答可能偏题，与问题关键词重合度低", "先复述问题核心，再展开回答，确保不跑偏", now))
		}
	}

	return defects
}

func (da *DefectAnalyzer) create(type_ DefectType, severity DefectSeverity, description, suggestion string, timestamp int64) DefectEntry {
	return DefectEntry{
		ID:          generateShortID(),
		Type:        type_,
		Severity:    severity,
		Description: description,
		Timestamp:   timestamp,
		Suggestion:  suggestion,
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func countMatches(s, pattern string) int {
	re := regexp.MustCompile(pattern)
	return len(re.FindAllString(s, -1))
}

func generateShortID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}