package tool

import (
	"strings"
	"testing"
)

func TestBuildResumeInterviewQuestionsUsesResumeEvidence(t *testing.T) {
	resume := `# 项目经历
OfferPilot：主导多 Provider Agent 路由设计，将模型失败恢复时间从 30 秒降到 5 秒。
检索平台：负责混合检索和重排，上线后召回率提升 18%。
# 技能
Go、MySQL、Redis`
	questions := BuildResumeInterviewQuestions(resume, 5)
	if len(questions) != 5 {
		t.Fatalf("question count = %d, want 5", len(questions))
	}
	for _, question := range questions {
		if strings.TrimSpace(question.Evidence) == "" || !strings.Contains(question.Question, question.Evidence) {
			t.Fatalf("question is not grounded in evidence: %#v", question)
		}
	}
}

func TestBuildResumeInterviewQuestionsRejectsEmptyMaterial(t *testing.T) {
	if questions := BuildResumeInterviewQuestions("姓名\n电话", 5); len(questions) != 0 {
		t.Fatalf("questions = %#v, want none", questions)
	}
}
