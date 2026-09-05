package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MemoryStore struct {
	mu              sync.Mutex
	entries         []*MemoryEntry
	db              *sql.DB
	active          map[string]*ActiveProfile
	knowledgePoints []KnowledgePoint // 第二层知识点缓存
	profiles        map[string]*StructuredProfile
}

// NewMemoryStore 创建 MemoryStore。db 为 MySQL 连接；传 nil 则仅内存模式（不持久化）。
func NewMemoryStore(db *sql.DB) (*MemoryStore, error) {
	store := &MemoryStore{
		db:              db,
		entries:         make([]*MemoryEntry, 0),
		active:          make(map[string]*ActiveProfile),
		knowledgePoints: make([]KnowledgePoint, 0),
		profiles:        make(map[string]*StructuredProfile),
	}
	if db != nil {
		if err := store.loadFromDB(); err != nil {
			return nil, fmt.Errorf("failed to load from DB: %w", err)
		}
	}

	return store, nil
}

func (s *MemoryStore) Add(entry MemoryEntry) *MemoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	copy := *full
	return &copy
}

func (s *MemoryStore) Query(q MemoryQuery) []*MemoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	copies := make([]*MemoryEntry, len(results))
	for i, entry := range results {
		copy := *entry
		copies[i] = &copy
	}
	return copies
}

func (s *MemoryStore) GetBySession(sessionID string) []*MemoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	var results []*MemoryEntry
	for _, e := range s.entries {
		if e.SessionID == sessionID {
			copy := *e
			results = append(results, &copy)
		}
	}
	return results
}

func (s *MemoryStore) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()

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
func (s *MemoryStore) GetActiveProfile(sessionID string) *ActiveProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = make(map[string]*ActiveProfile)
	}
	profile := s.active[sessionID]
	if profile == nil {
		return &ActiveProfile{}
	}
	copy := *profile
	copy.Questions = append([]string(nil), profile.Questions...)
	copy.StuckPoints = append([]string(nil), profile.StuckPoints...)
	copy.ExpressionNotes = append([]string(nil), profile.ExpressionNotes...)
	return &copy
}

// UpdateActiveProfile 更新第一层活性画像。
func (s *MemoryStore) UpdateActiveProfile(sessionID string, ap ActiveProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = make(map[string]*ActiveProfile)
	}
	ap.UpdatedAt = time.Now().UnixMilli()
	ap.Questions = append([]string(nil), ap.Questions...)
	ap.StuckPoints = append([]string(nil), ap.StuckPoints...)
	ap.ExpressionNotes = append([]string(nil), ap.ExpressionNotes...)
	s.active[sessionID] = &ap
}

// GetKnowledgePoints 返回第二层知识点缓存。
func (s *MemoryStore) GetKnowledgePoints(userID string) []KnowledgePoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	points := make([]KnowledgePoint, 0)
	for _, point := range s.knowledgePoints {
		if point.UserID == userID {
			points = append(points, point)
		}
	}
	return points
}

// SetKnowledgePoints 设置第二层知识点缓存。
func (s *MemoryStore) SetKnowledgePoints(userID string, points []KnowledgePoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.knowledgePoints[:0]
	for _, point := range s.knowledgePoints {
		if point.UserID != userID {
			filtered = append(filtered, point)
		}
	}
	s.knowledgePoints = append(filtered, points...)
}

// GetProfile 返回第二层结构化画像缓存。
func (s *MemoryStore) GetProfile(userID string) *StructuredProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profiles == nil || s.profiles[userID] == nil {
		return nil
	}
	copy := *s.profiles[userID]
	return &copy
}

// SetProfile 设置第二层结构化画像缓存。
func (s *MemoryStore) SetProfile(userID string, p *StructuredProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profiles == nil {
		s.profiles = make(map[string]*StructuredProfile)
	}
	copy := *p
	s.profiles[userID] = &copy
}
