package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MemoryStore struct {
	entries         []*MemoryEntry
	db              *sql.DB
	active          *ActiveProfile    // 第一层活性画像（内存态）
	knowledgePoints []KnowledgePoint  // 第二层知识点缓存
	profile         *StructuredProfile // 第二层结构化画像缓存
}

// NewMemoryStore 创建 MemoryStore。db 为 MySQL 连接；传 nil 则仅内存模式（不持久化）。
func NewMemoryStore(db *sql.DB) (*MemoryStore, error) {
	store := &MemoryStore{
		db:              db,
		entries:         make([]*MemoryEntry, 0),
		active:          &ActiveProfile{},
		knowledgePoints: make([]KnowledgePoint, 0),
	}
	if db != nil {
		if err := store.loadFromDB(); err != nil {
			return nil, fmt.Errorf("failed to load from DB: %w", err)
		}
	}

	return store, nil
}

func (s *MemoryStore) Add(entry MemoryEntry) *MemoryEntry {
	now := time.Now().UnixMilli()
	full := &MemoryEntry{
		ID:             uuid.New().String(),
		UserID:         entry.UserID,
		SessionID:      entry.SessionID,
		Type:           entry.Type,
		Content:        entry.Content,
		Importance:     entry.Importance,
		CreateAt:       now,
		LastAccessedAt: now,
		AccessCount:    0,
	}
	s.entries = append(s.entries, full)

	if s.db != nil {
		s.db.Exec(`
			INSERT INTO memories(id, user_id, session_id, type, content, importance, access_count, created_at, last_accessed_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			full.ID, full.UserID, full.SessionID, string(full.Type), full.Content, full.Importance, 0, full.CreateAt, full.LastAccessedAt)
	}

	return full
}

func (s *MemoryStore) Query(q MemoryQuery) []*MemoryEntry {
	results := make([]*MemoryEntry, 0, len(s.entries))
	for _, e := range s.entries {
		if q.UserID != "" && e.UserID != q.UserID {
			continue
		}
		if q.SessionID != "" && e.SessionID != q.SessionID {
			continue
		}
		if q.Type != "" && e.Type != q.Type {
			continue
		}
		if q.MinImportance != 0 && e.Importance < q.MinImportance {
			continue
		}
		if q.Query != "" {
			if !strings.Contains(strings.ToLower(e.Content), strings.ToLower(q.Query)) {
				continue
			}
		}
		results = append(results, e)
	}

	// 按 importance 降序排序
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Importance > results[i].Importance {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if q.Limit > 0 && q.Limit < len(results) {
		results = results[:q.Limit]
	}

	now := time.Now().UnixMilli()
	for _, entry := range results {
		entry.LastAccessedAt = now
		entry.AccessCount++

		if s.db != nil {
			s.db.Exec(`
				UPDATE memories SET access_count = ?, last_accessed_at = ? WHERE id = ?
			`, entry.AccessCount, entry.LastAccessedAt, entry.ID)
		}
	}
	return results
}

func (s *MemoryStore) GetBySession(sessionID string) []*MemoryEntry {
	var results []*MemoryEntry
	for _, e := range s.entries {
		if e.SessionID == sessionID {
			results = append(results, e)
		}
	}
	return results
}

func (s *MemoryStore) Remove(id string) bool {
	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)

			if s.db != nil {
				s.db.Exec("DELETE FROM memories WHERE id = ?", id)
			}

			return true
		}
	}
	return false
}

func (s *MemoryStore) Size() int {
	return len(s.entries)
}

// LoadFromMySQL 从 MySQL 按 user_id 加载记忆条目到内存缓存（供 Server 启动时按需调用）。
func (s *MemoryStore) LoadFromMySQL(userID string) error {
	if s.db == nil {
		return nil
	}

	rows, err := s.db.Query(`
		SELECT id, user_id, session_id, type, content, importance, access_count, created_at, last_accessed_at
		FROM memories
		WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return fmt.Errorf("query memories failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entry MemoryEntry
		var typeStr string
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.SessionID,
			&typeStr,
			&entry.Content,
			&entry.Importance,
			&entry.AccessCount,
			&entry.CreateAt,
			&entry.LastAccessedAt,
		); err != nil {
			return fmt.Errorf("scan memory failed: %w", err)
		}
		entry.Type = MemoryType(typeStr)

		// 去重：已缓存则跳过
		dup := false
		for _, e := range s.entries {
			if e.ID == entry.ID {
				dup = true
				break
			}
		}
		if !dup {
			s.entries = append(s.entries, &entry)
		}
	}

	return rows.Err()
}

func (s *MemoryStore) loadFromDB() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`
		SELECT id, user_id, session_id, type, content, importance, access_count, created_at, last_accessed_at
		FROM memories
		ORDER BY created_at DESC
	`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entry MemoryEntry
		var typeStr string
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.SessionID,
			&typeStr,
			&entry.Content,
			&entry.Importance,
			&entry.AccessCount,
			&entry.CreateAt,
			&entry.LastAccessedAt,
		); err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		entry.Type = MemoryType(typeStr)
		s.entries = append(s.entries, &entry)
	}

	return rows.Err()
}

// GetActiveProfile 返回第一层活性画像（内存态）。
func (s *MemoryStore) GetActiveProfile() *ActiveProfile {
	if s.active == nil {
		s.active = &ActiveProfile{}
	}
	return s.active
}

// UpdateActiveProfile 更新第一层活性画像。
func (s *MemoryStore) UpdateActiveProfile(ap ActiveProfile) {
	ap.UpdatedAt = time.Now().UnixMilli()
	s.active = &ap
}

// GetKnowledgePoints 返回第二层知识点缓存。
func (s *MemoryStore) GetKnowledgePoints() []KnowledgePoint {
	return s.knowledgePoints
}

// SetKnowledgePoints 设置第二层知识点缓存。
func (s *MemoryStore) SetKnowledgePoints(points []KnowledgePoint) {
	s.knowledgePoints = points
}

// GetProfile 返回第二层结构化画像缓存。
func (s *MemoryStore) GetProfile() *StructuredProfile {
	return s.profile
}

// SetProfile 设置第二层结构化画像缓存。
func (s *MemoryStore) SetProfile(p *StructuredProfile) {
	s.profile = p
}