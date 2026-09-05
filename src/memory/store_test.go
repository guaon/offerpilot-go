package memory

import (
	"testing"
)

func TestMemoryStoreAddAndQueryByUser(t *testing.T) {
	s, err := NewMemoryStore(nil)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}

	s.Add(MemoryEntry{UserID: "u1", Type: MemoryTypeFace, Content: "姓名: 李明"})
	s.Add(MemoryEntry{UserID: "u2", Type: MemoryTypeFace, Content: "姓名: 张三"})

	got := s.Query(MemoryQuery{UserID: "u1"})
	if len(got) != 1 {
		t.Fatalf("期望 1 条 u1 记忆，得到 %d", len(got))
	}
	if got[0].Content != "姓名: 李明" {
		t.Errorf("期望 '姓名: 李明'，得到 '%s'", got[0].Content)
	}
	if got[0].UserID != "u1" {
		t.Errorf("期望 UserID=u1，得到 %s", got[0].UserID)
	}
}

func TestMemoryStoreQueryByType(t *testing.T) {
	s, _ := NewMemoryStore(nil)

	s.Add(MemoryEntry{UserID: "u1", Type: MemoryTypeWeakness, Content: "rag 维度得分 4"})
	s.Add(MemoryEntry{UserID: "u1", Type: MemoryTypeStrength, Content: "架构 维度得分 8"})

	weak := s.Query(MemoryQuery{UserID: "u1", Type: MemoryTypeWeakness})
	if len(weak) != 1 || weak[0].Type != MemoryTypeWeakness {
		t.Fatalf("期望 1 条 weakness，得到 %d", len(weak))
	}

	strong := s.Query(MemoryQuery{UserID: "u1", Type: MemoryTypeStrength})
	if len(strong) != 1 || strong[0].Type != MemoryTypeStrength {
		t.Fatalf("期望 1 条 strength，得到 %d", len(strong))
	}
}

func TestActiveProfileUpdate(t *testing.T) {
	s, _ := NewMemoryStore(nil)

	ap := s.GetActiveProfile("session-1")
	if ap == nil {
		t.Fatal("GetActiveProfile 不应返回 nil")
	}

	s.UpdateActiveProfile("session-1", ActiveProfile{
		CurrentTopic:    "rag",
		CurrentQuestion: "什么是 RAG",
		QuestionIndex:   2,
	})

	got := s.GetActiveProfile("session-1")
	if got.CurrentTopic != "rag" {
		t.Errorf("期望 topic=rag，得到 %s", got.CurrentTopic)
	}
	if got.QuestionIndex != 2 {
		t.Errorf("期望 index=2，得到 %d", got.QuestionIndex)
	}
}

func TestProfilesAreScopedByUserAndSession(t *testing.T) {
	s, _ := NewMemoryStore(nil)

	s.SetProfile("user-1", &StructuredProfile{UserID: "user-1", JobDirection: "agent"})
	s.SetProfile("user-2", &StructuredProfile{UserID: "user-2", JobDirection: "backend"})
	if got := s.GetProfile("user-1"); got == nil || got.JobDirection != "agent" {
		t.Fatalf("unexpected user-1 profile: %#v", got)
	}
	if got := s.GetProfile("user-2"); got == nil || got.JobDirection != "backend" {
		t.Fatalf("unexpected user-2 profile: %#v", got)
	}

	s.UpdateActiveProfile("session-1", ActiveProfile{CurrentTopic: "rag"})
	s.UpdateActiveProfile("session-2", ActiveProfile{CurrentTopic: "go"})
	if got := s.GetActiveProfile("session-1"); got.CurrentTopic != "rag" {
		t.Fatalf("unexpected session-1 profile: %#v", got)
	}
	if got := s.GetActiveProfile("session-2"); got.CurrentTopic != "go" {
		t.Fatalf("unexpected session-2 profile: %#v", got)
	}
}

func TestKnowledgePointsAreScopedByUser(t *testing.T) {
	s, _ := NewMemoryStore(nil)
	s.SetKnowledgePoints("user-1", []KnowledgePoint{{UserID: "user-1", PointName: "rag"}})
	s.SetKnowledgePoints("user-2", []KnowledgePoint{{UserID: "user-2", PointName: "go"}})

	points := s.GetKnowledgePoints("user-1")
	if len(points) != 1 || points[0].PointName != "rag" {
		t.Fatalf("unexpected user-1 knowledge points: %#v", points)
	}
}
