package realtime

import (
	"fmt"
	"time"
)

var defaultQuestions = []string{
	"请介绍一下你在 Agent 方向的工作经历",
	"什么是 ReAct 模式？工程实现中需要注意什么？",
	"如何设计一个支持多 Provider 的 LLM 调用层？",
	"RAG 系统中 Chunk 策略有哪些选择？",
	"说一个你优化系统性能的具体案例",
}

type RealtimeInterviewSession struct {
	session          RealtimeSession
	analyzer         *DefectAnalyzer
	questions        []string
	questionStartTime int64
}

func NewRealtimeInterviewSession(questions []string) *RealtimeInterviewSession {
	if questions == nil {
		questions = defaultQuestions
	}

	return &RealtimeInterviewSession{
		session: RealtimeSession{
			ID:              generateShortID(),
			State:           RealtimeSessionIdle,
			CurrentQuestion: "",
			QuestionsAsked:  0,
			TotalQuestions:  len(questions),
			Transcript:      []TranscriptEntry{},
			Defects:         []DefectEntry{},
		},
		analyzer: NewDefectAnalyzer(),
		questions: questions,
	}
}

func (s *RealtimeInterviewSession) ID() string {
	return s.session.ID
}

func (s *RealtimeInterviewSession) State() RealtimeSessionState {
	return s.session.State
}

func (s *RealtimeInterviewSession) CurrentQuestion() string {
	return s.session.CurrentQuestion
}

func (s *RealtimeInterviewSession) Progress() struct {
	Asked int
	Total int
} {
	return struct {
		Asked int
		Total int
	}{
		Asked: s.session.QuestionsAsked,
		Total: s.session.TotalQuestions,
	}
}

func (s *RealtimeInterviewSession) Start() string {
	s.session.State = RealtimeSessionQuestioning
	return s.NextQuestion()
}

func (s *RealtimeInterviewSession) NextQuestion() string {
	if s.session.QuestionsAsked >= len(s.questions) {
		s.session.State = RealtimeSessionIdle
		return ""
	}

	q := s.questions[s.session.QuestionsAsked]
	s.session.CurrentQuestion = q
	s.session.State = RealtimeSessionQuestioning
	s.questionStartTime = time.Now().UnixMilli()

	s.session.Transcript = append(s.session.Transcript, TranscriptEntry{
		Speaker:   SpeakerInterviewer,
		Text:      q,
		Timestamp: time.Now().UnixMilli(),
	})

	return q
}

type SubmitAnswerResult struct {
	Defects []DefectEntry `json:"defects"`
	Summary string        `json:"summary"`
}

func (s *RealtimeInterviewSession) SubmitAnswer(answerText string) SubmitAnswerResult {
	elapsed := time.Now().UnixMilli() - s.questionStartTime

	s.session.Transcript = append(s.session.Transcript, TranscriptEntry{
		Speaker:    SpeakerCandidate,
		Text:       answerText,
		Timestamp:  time.Now().UnixMilli(),
		DurationMs: elapsed,
	})

	s.session.State = RealtimeSessionAnalyzing

	defects := s.analyzer.Analyze(AnalysisInput{
		Question:  s.session.CurrentQuestion,
		Answer:    answerText,
		ElapsedMs: elapsed,
	})

	s.session.Defects = append(s.session.Defects, defects...)
	s.session.QuestionsAsked++
	s.session.State = RealtimeSessionAnswering

	var critical, moderate int
	for _, d := range defects {
		if d.Severity == DefectSeverityCritical {
			critical++
		} else if d.Severity == DefectSeverityModerate {
			moderate++
		}
	}

	var summary string
	if len(defects) == 0 {
		summary = "✅ 这道题回答不错，没有明显缺陷"
	} else {
		summary = "发现 " + fmt.Sprintf("%d", len(defects)) + " 个问题"
		if critical > 0 {
			summary += "（" + fmt.Sprintf("%d", critical) + " 个严重）"
		}
		summary += "："
		for i, d := range defects {
			if i > 0 {
				summary += "；"
			}
			summary += d.Description
		}
	}

	return SubmitAnswerResult{
		Defects: defects,
		Summary: summary,
	}
}

type ReportResult struct {
	TotalDefects  int                 `json:"totalDefects"`
	BySeverity    map[string]int      `json:"bySeverity"`
	TopIssues     []string            `json:"topIssues"`
	OverallScore  int                 `json:"overallScore"`
}

func (s *RealtimeInterviewSession) GetReport() ReportResult {
	defects := s.session.Defects
	bySeverity := map[string]int{
		"critical": 0,
		"moderate": 0,
		"minor":    0,
	}

	for _, d := range defects {
		bySeverity[string(d.Severity)]++
	}

	typeCounts := map[string]int{}
	for _, d := range defects {
		typeCounts[string(d.Type)]++
	}

	typeCountSlice := make([]struct {
		Type  string
		Count int
	}, 0, len(typeCounts))
	for t, c := range typeCounts {
		typeCountSlice = append(typeCountSlice, struct {
			Type  string
			Count int
		}{t, c})
	}

	for i := 0; i < len(typeCountSlice)-1; i++ {
		for j := i + 1; j < len(typeCountSlice); j++ {
			if typeCountSlice[j].Count > typeCountSlice[i].Count {
				typeCountSlice[i], typeCountSlice[j] = typeCountSlice[j], typeCountSlice[i]
			}
		}
	}

	topIssues := make([]string, 0, 3)
	for i, tc := range typeCountSlice {
		if i >= 3 {
			break
		}
		topIssues = append(topIssues, tc.Type+" ("+fmt.Sprintf("%d", tc.Count)+"次)")
	}

	maxDefects := s.session.QuestionsAsked * 4
	overallScore := 1
	if maxDefects > 0 {
		overallScore = int(float64(10) - float64(len(defects))/float64(maxDefects)*7)
		if overallScore < 1 {
			overallScore = 1
		}
	}

	return ReportResult{
		TotalDefects: len(defects),
		BySeverity:   bySeverity,
		TopIssues:    topIssues,
		OverallScore: overallScore,
	}
}

func (s *RealtimeInterviewSession) GetTranscript() []TranscriptEntry {
	return s.session.Transcript
}