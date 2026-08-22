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

	ap := s.GetActiveProfile()
	if ap == nil {
		t.Fatal("GetActiveProfile 不应返回 nil")
	}

	s.UpdateActiveProfile(ActiveProfile{
		CurrentTopic:    "rag",
		CurrentQuestion: "什么是 RAG",
		QuestionIndex:   2,
	})

	got := s.GetActiveProfile()
	if got.CurrentTopic != "rag" {
		t.Errorf("期望 topic=rag，得到 %s", got.CurrentTopic)
	}
	if got.QuestionIndex != 2 {
		t.Errorf("期望 index=2，得到 %d", got.QuestionIndex)
	}
}