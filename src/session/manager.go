package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type SessionManager struct {
	mu          sync.Mutex
	sessions    map[string]*Session
	checkpoints map[string][]*CheckPoints
	db          *sql.DB
}

// NewSessionManager 创建 SessionManager。db 为 MySQL 连接；传 nil 则仅内存模式。
func NewSessionManager(db *sql.DB) (*SessionManager, error) {
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

func (sm *SessionManager) EnsureLoaded() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.sessions == nil {
		sm.sessions = make(map[string]*Session)
	}
	if sm.checkpoints == nil {
		sm.checkpoints = make(map[string][]*CheckPoints)
	}
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
		metadataJSON, _ := json.Marshal(s.Metadata)
		sm.db.Exec(`
			INSERT INTO chat_sessions(id, user_id, state, metadata, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?)`,
			s.ID, userID, string(s.State), string(metadataJSON), now, now)
	}

	return s
}

// ClaimSession 将匿名会话归属到指定用户（登录认领）。
func (sm *SessionManager) ClaimSession(sessionID, userID string) error {
	s := sm.sessions[sessionID]
	if s == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s.Metadata.UserID = userID
	s.UpdatedAt = time.Now().UnixMilli()

	if sm.db != nil {
		metadataJSON, _ := json.Marshal(s.Metadata)
		sm.db.Exec(`UPDATE chat_sessions SET user_id = ?, metadata = ?, updated_at = ? WHERE id = ?`,
			userID, string(metadataJSON), s.UpdatedAt, sessionID)
	}
	return nil
}

// Transition 将指定会话从一个状态转换到另一个状态。
func (sm *SessionManager) Transition(id string, newState SessionState) error {
	s := sm.sessions[id]
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	valid := sm.validTransitions(s.State)
	found := false
	for _, v := range valid {
		if v == newState {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("invalid transition: %s → %s", s.State, newState)
	}

	s.State = newState
	s.UpdatedAt = time.Now().UnixMilli()

	if sm.db != nil {
		sm.db.Exec(
			`UPDATE chat_sessions SET state = ?, updated_at = ? WHERE id = ?`,
			string(newState), s.UpdatedAt, id)
	}

	return nil
}

func (sm *SessionManager) Get(id string) (*Session, error) {
	s := sm.sessions[id]
	if s == nil {
		if sm.db != nil {
			if err := sm.loadSessionFromDB(id); err == nil && sm.sessions[id] != nil {
				return sm.sessions[id], nil
			}
		}
		return nil, fmt.Errorf("session %s not found", id)
	}
	return s, nil
}

func (sm *SessionManager) GetMessages(id string) ([]*schema.Message, error) {
	s, err := sm.Get(id)
	if err != nil {
		return nil, err
	}
	return s.Messages, nil
}

func (sm *SessionManager) AddMessage(id string, message *schema.Message) error {
	s := sm.sessions[id]
	if s == nil {
		return fmt.Errorf("session %s not found", id)
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
			INSERT INTO messages (session_id, role, content, tool_call_id, tool_calls, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, id, string(message.Role), strings.ToValidUTF8(message.Content, "\ufffd"), message.ToolCallID, toolCallsJSON, time.Now().UnixMilli())

		metadataJSON, _ := json.Marshal(s.Metadata)
		sm.db.Exec(`
			UPDATE chat_sessions SET metadata = ?, updated_at = ? WHERE id = ?
		`, string(metadataJSON), s.UpdatedAt, id)
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
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", id)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to delete messages: %w", err)
		}
		now := time.Now().UnixMilli()
		for _, msg := range messages {
			var toolCallsJSON string
			if len(msg.ToolCalls) > 0 {
				data, _ := json.Marshal(msg.ToolCalls)
				toolCallsJSON = string(data)
			}

			_, err = tx.Exec(`
				INSERT INTO messages (session_id, role, content, tool_call_id, tool_calls, created_at)
				VALUES(?, ?, ?, ?, ?, ?)
			`, id, string(msg.Role), strings.ToValidUTF8(msg.Content, "\ufffd"), msg.ToolCallID, toolCallsJSON, now)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to insert message: %w", err)
			}
		}

		_, err = tx.Exec("UPDATE chat_sessions SET updated_at = ? WHERE id = ?", s.UpdatedAt, id)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update session: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
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

// ReWind 回到检查点时的状态。
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

// ListByUserID 返回指定用户的所有会话（按更新时间倒序）。
func (sm *SessionManager) ListByUserID(userID string) []*Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var result []*Session
	for _, s := range sm.sessions {
		if s.Metadata.UserID == userID {
			result = append(result, s)
		}
	}

	// 按更新时间倒序
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].UpdatedAt > result[i].UpdatedAt {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// Delete 删除指定会话，需校验 userID 归属。
func (sm *SessionManager) Delete(sessionID, userID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s := sm.sessions[sessionID]
	if s == nil {
		return fmt.Errorf("session not found")
	}
	if s.Metadata.UserID != "" && s.Metadata.UserID != userID {
		return fmt.Errorf("permission denied")
	}

	delete(sm.sessions, sessionID)
	delete(sm.checkpoints, sessionID)

	if sm.db != nil {
		sm.db.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
		sm.db.Exec("DELETE FROM chat_sessions WHERE id = ?", sessionID)
	}
	return nil
}

func (sm *SessionManager) loadFromDB() error {
	if sm.db == nil {
		return nil
	}

	rows, err := sm.db.Query("SELECT id, state, user_id, metadata, created_at, updated_at FROM chat_sessions ORDER BY created_at DESC")
	if err != nil {
		return fmt.Errorf("query sessions failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, state, userID, metadataJSON string
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &state, &userID, &metadataJSON, &createdAt, &updatedAt); err != nil {
			return fmt.Errorf("scan session failed: %w", err)
		}

		var metadata SessionMetadata
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
		s := &Session{
			ID:        id,
			State:     SessionState(state),
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			Messages:  make([]*schema.Message, 0),
			Metadata:  metadata,
		}

		if userID != "" {
			s.Metadata.UserID = userID
		}
		msgRows, err := sm.db.Query("SELECT role, content, tool_call_id, tool_calls FROM messages WHERE session_id = ? ORDER BY id", id)
		if err != nil {
			return fmt.Errorf("query messages failed: %w", err)
		}
		for msgRows.Next() {
			var role, content, toolCallID, toolCallsJSON string
			if err := msgRows.Scan(&role, &content, &toolCallID, &toolCallsJSON); err != nil {
				msgRows.Close()
				return fmt.Errorf("scan message failed: %w", err)
			}

			msg := &schema.Message{
				Role:       schema.RoleType(role),
				Content:    content,
				ToolCallID: toolCallID,
			}

			if toolCallsJSON != "" {
				var toolCalls []schema.ToolCall
				if err := json.Unmarshal([]byte(toolCallsJSON), &toolCalls); err == nil {
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

func (sm *SessionManager) loadSessionFromDB(id string) error {
	if sm.db == nil {
		return fmt.Errorf("no database")
	}

	row := sm.db.QueryRow(
		"SELECT id, state, user_id, metadata, created_at, updated_at FROM chat_sessions WHERE id = ?",
		id,
	)

	var sid, state, userID, metadataJSON string
	var createdAt, updatedAt int64
	if err := row.Scan(&sid, &state, &userID, &metadataJSON, &createdAt, &updatedAt); err != nil {
		return fmt.Errorf("session not found in db: %w", err)
	}

	var metadata SessionMetadata
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	s := &Session{
		ID:        sid,
		State:     SessionState(state),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Messages:  make([]*schema.Message, 0),
		Metadata:  metadata,
	}

	if userID != "" {
		s.Metadata.UserID = userID
	}

	msgRows, err := sm.db.Query(
		"SELECT role, content, tool_call_id, tool_calls FROM messages WHERE session_id = ? ORDER BY id",
		id,
	)
	if err != nil {
		return fmt.Errorf("query messages failed: %w", err)
	}
	defer msgRows.Close()

	for msgRows.Next() {
		var role, content, toolCallID, toolCallsJSON string
		if err := msgRows.Scan(&role, &content, &toolCallID, &toolCallsJSON); err != nil {
			return fmt.Errorf("scan message failed: %w", err)
		}

		msg := &schema.Message{
			Role:       schema.RoleType(role),
			Content:    content,
			ToolCallID: toolCallID,
		}

		if toolCallsJSON != "" {
			var toolCalls []schema.ToolCall
			if err := json.Unmarshal([]byte(toolCallsJSON), &toolCalls); err == nil {
				msg.ToolCalls = toolCalls
			}
		}

		s.Messages = append(s.Messages, msg)
	}
	msgRows.Close()

	sm.sessions[id] = s
	return nil
}