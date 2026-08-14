package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MemoryStore struct {
	entries []*MemoryEntry
	db      *sql.DB
}

func NewMemoryStore(dpPath string) (*MemoryStore, error) {
	var db *sql.DB
	var err error

	if dpPath != "" {
		db, err = sql.Open("sqlite", dpPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open database:%w", err)

		}
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("failed to ping database:%w", err)
		}
		if err := createMemoryTable(db); err != nil {
			return nil, fmt.Errorf("failed to create table:%w", err)
		}

	}

	store := &MemoryStore{
		db:      db,
		entries: make([]*MemoryEntry, 0),
	}
	if db != nil {
		if err := store.loadFromDB(); err != nil {
			return nil, fmt.Errorf("failed to load from DB:%w", err)
		}
	}

	return store, nil
}

func createMemoryTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS memories(
	    id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		type TEXT NOT NULL,
        content TEXT NOT NULL,
		importance REAL NOT NULL,
		access_count INTEGER NOT NULL DEFAULT 0,
		create_at INTEGER NOT NULL,
		last_accessed_at INTEGER NOT NULL

	);

	CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id);
	CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
	CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance);
	`

	_, err := db.Exec(query)
	return err

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
		   INSERT INTO memories(id,user_id,session_id,type,content,importance,access_count,create_at,last_accessed_at)
		   VALUES(?,?,?,?,?,?,?,?,?)`,
			full.ID, full.UserID, full.SessionID, full.Type, full.Content, full.Importance, 0, full.CreateAt, full.LastAccessedAt)
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
			  UPDATE memories SET access_count=?,last_accessed_at=? WHERE id=?
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
				s.db.Exec("DELETE FROM memories WHERE id=?", id)
			}

			return true
		}

	}
	return false
}

func (s *MemoryStore) Size() int {
	return len(s.entries)
}

func (s *MemoryStore) loadFromDB() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`
	   SELECT id,session_id,type,content,importance,access_count,created_at,last_accessed_at
	   FROM memories
	`)

	if err != nil {
		return fmt.Errorf("query failed :%w", err)

	}
	defer rows.Close()

	for rows.Next() {
		var entry MemoryEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.SessionID,
			&entry.Type,
			&entry.Content,
			&entry.Importance,
			&entry.AccessCount,
			&entry.CreateAt,
			&entry.LastAccessedAt,
		); err != nil {
			return fmt.Errorf("scan failed:%w", err)
		}
		s.entries = append(s.entries, &entry)
	}

	return rows.Err()
}
