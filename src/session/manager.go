package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type SessionManager struct {
	sessions    map[string]*Session
	checkpoints map[string][]*CheckPoints
	db          *sql.DB
}

func NewSessionManager(dbPath string) (*SessionManager, error) {
	var db *sql.DB
	var err error

	if dbPath != "" {
		db, err = sql.Open("sqlite", dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}

		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("failed to ping database: %w", err)
		}

		if err := createSessionTables(db); err != nil {
			return nil, fmt.Errorf("failed to create tables: %w", err)
		}
	}

	sm := &SessionManager{
		sessions:    make(map[string]*Session),
		checkpoints: make(map[string][]*CheckPoints),
		db:          db,
	}

	if db != nil {
		if err := sm.loadFromDB(); err != nil {
			return nil, fmt.Errorf("failed to load from DB: %w", err)
		}
	}

	return sm, nil
}

func createSessionTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sessions(
		    id TEXT NOT NULL,
			state TEXT NOT NULL,
			user_id TEXT,
			metadata TEXT NOT NULL,
			create_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS messages(
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT,
			tool_call_id TEXT,
			tool_calls TEXT,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);`,

		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query: %w", err)

		}
	}

	return nil
}

func (sm *SessionManager) Create(userID string) *Session {
	now := time.Now().UnixMilli()

	s := &Session{
		ID:        uuid.New().String(),
		State:     SessionStateIdle,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  make([]*schema.Message, 0),
		Metadata: SessionMetadata{
			UserID:         userID,
			QuestionsAsked: 0,
			Dimensions:     make([]string, 0),
		},
	}

	sm.sessions[s.ID] = s

	if sm.db != nil {
		metadataJSON, _ := json.Marshal(s.Messages)
		sm.db.Exec(`
		    INSERT INTO sessions(id,state,user_id,metadata,created_at,updated_at)
			VALUES(?,?,?,?,?,?)
		`, s.ID, s.State, userID, string(metadataJSON), now/1000, now/1000)
	}

	return s
}

// 将指定会话从一个状态转换到另一个状态
func (sm *SessionManager) Transition(id string, newState SessionState) error {
	s := sm.sessions[id]
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	valid := sm.validTransitions(s.State) //根据当前状态获取可转移的状态
	found := false
	for _, v := range valid {
		if v == newState {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("invalid transitions:%s→%s", s.State, newState)
	}

	s.State = newState
	s.UpdatedAt = time.Now().UnixMilli()

	if sm.db != nil {
		sm.db.Exec(
			`UPDATE sessions SET state = ?,updated_at=?WHERE id=?`,
			newState, s.UpdatedAt/1000, id)
	}

	return nil
}

func (sm *SessionManager) Get(id string) (*Session, error) {
	s := sm.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return s, nil
}

func (sm *SessionManager) GetMessages(id string) ([]*schema.Message, error) {
	s := sm.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return s.Messages, nil
}

func (sm *SessionManager) AddMessage(id string, message *schema.Message) error {
	s := sm.sessions[id]
	if s == nil {
		return fmt.Errorf("session %s  not found", id)
	}

	s.Messages = append(s.Messages, message)
	s.UpdatedAt = time.Now().UnixMilli()

	if message.Role == schema.User {
		s.Metadata.QuestionsAsked++
	}

	if sm.db != nil {
		var toolCallsJSON string
		if len(message.ToolCalls) > 0 {
			data, _ := json.Marshal(message.ToolCalls)
			toolCallsJSON = string(data)
		}

		sm.db.Exec(`
		    INSERT INTO messages (session_id,role,content,tool_call_id,tool_calls,created_at)
			VALUES (?,?,?,?,?,?)
		`, id, message.Role, message.Content, message.ToolCallID, toolCallsJSON, time.Now().UnixMilli()/1000)

		metadataJSON, _ := json.Marshal(s.Metadata)

		sm.db.Exec(`
			UPDATE sessions SET metadata=?,updated_at=? WHERE id=?
		`, metadataJSON, s.UpdatedAt/1000, id)

	}

	return nil

}

func (sm *SessionManager) validTransitions(current SessionState) []SessionState {
	switch current {
	case SessionStateIdle:
		return []SessionState{SessionStateActive}
	case SessionStateActive:
		return []SessionState{SessionStatePaused, SessionStateCompleted, SessionStateError}
	case SessionStatePaused:
		return []SessionState{SessionStateActive, SessionStateCompleted}
	case SessionStateCompleted:
		return []SessionState{}
	case SessionStateError:
		return []SessionState{SessionStateActive}
	default:
		return []SessionState{}
	}
}

func (sm *SessionManager) ReplaceMessages(id string, messages []*schema.Message) error {
	s := sm.sessions[id]
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}

	s.Messages = messages
	s.UpdatedAt = time.Now().UnixMilli()

	if sm.db != nil {
		tx, err := sm.db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction:%w", err)

		}
		_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", id)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to delete messages:%w", err)
		}
		now := time.Now().UnixMilli() / 1000
		for _, msg := range messages {
			var toolCallsJSON string
			if len(msg.ToolCalls) > 0 {
				data, _ := json.Marshal(msg.ToolCalls)
				toolCallsJSON = string(data)
			}

			_, err = tx.Exec(`
			  INSERT INTO messages (session_id,role,content,tool_call_id,tool_calls,created_at)
			  VALUES(?,?,?,?,?,?)
			`, id, msg.Role, msg.Content, msg.ToolCallID, toolCallsJSON, now)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to insert message:%w", err)
			}
		}

		_, err = tx.Exec("UPDATE sessions SET updated_at=? WHERE id=?", s.UpdatedAt/1000, id)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update session:%w", err)

		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction:%w", err)
		}

	}

	return nil
}

func (sm *SessionManager) Checkpoint(id string) (*CheckPoints, error) {
	s := sm.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("session %s not found", id)
	}

	cp := &CheckPoints{
		ID:           uuid.New().String(),
		SessionID:    id,
		CreatedAt:    time.Now().UnixMilli(),
		MessageIndex: len(s.Messages),
		State:        s.State,
		Metadata:     s.Metadata,
	}

	list := sm.checkpoints[id]
	list = append(list, cp)
	sm.checkpoints[id] = list

	return cp, nil
}

// 回到检查点时的状态
func (sm *SessionManager) ReWind(sessionID string, checkpointID string) error {
	s := sm.sessions[sessionID]
	if s == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	list := sm.checkpoints[sessionID]
	var cp *CheckPoints
	for _, c := range list {
		if c.ID == checkpointID {
			cp = c
			break
		}
	}
	if cp == nil {
		return fmt.Errorf("checkpoint %s not found", checkpointID)
	}

	s.Messages = s.Messages[:cp.MessageIndex]
	s.State = cp.State
	s.Metadata = cp.Metadata
	s.UpdatedAt = time.Now().UnixMilli()

	return nil
}

func (sm *SessionManager) ListActive() []*Session {
	var result []*Session
	for _, s := range sm.sessions {
		if s.State == SessionStateActive || s.State == SessionStatePaused {
			result = append(result, s)
		}

	}
	return result
}

func (sm *SessionManager) loadFromDB() error {
	if sm.db == nil {
		return nil
	}

	rows, err := sm.db.Query("SELECT id,state,user_id,metadata,created_at,updated_at FROM sessions")
	if err != nil {
		return fmt.Errorf("query sessions failed:%w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, state, userID, metadataJSON string
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &state, &userID, &metadataJSON, &createdAt, &updatedAt); err != nil {
			return fmt.Errorf("scan session failed:%w", err)
		}

		var metadata SessionMetadata
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {

			return fmt.Errorf("failed to unmarshal metadata:%w", err)
		}
		s := &Session{
			ID:        id,
			State:     SessionState(state),
			CreatedAt: createdAt * 1000,
			UpdatedAt: updatedAt * 1000,
			Messages:  make([]*schema.Message, 0),
			Metadata:  metadata,
		}

		if userID != "" {
			s.Metadata.UserID = userID
		}
		msgRows, err := sm.db.Query("SELECT role,tool_call_id,tool_calls FROM messages WHERE session_id=? ORDER BY id", id)
		if err != nil {
			return fmt.Errorf("query messages failed:%w", err)
		}
		defer msgRows.Close()
		for msgRows.Next() {
			var role, content, toolCallID, toolCallsJSON string
			if err := msgRows.Scan(&role, &content, &toolCallID, &toolCallsJSON); err != nil {
				msgRows.Close()
				return fmt.Errorf("scan message failed:%w", err)
			}

			msg := &schema.Message{
				Role:       schema.RoleType(role),
				Content:    content,
				ToolCallID: toolCallID,
			}

			if toolCallsJSON != "" {
				var toolCalls []schema.ToolCall
				if err := json.Unmarshal([]byte(toolCallsJSON), &toolCalls); err != nil {
					msg.ToolCalls = toolCalls
				}

			}

			s.Messages = append(s.Messages, msg)

		}
		msgRows.Close()

		sm.sessions[id] = s

	}
	return rows.Err()
}
