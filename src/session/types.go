package session

import "github.com/cloudwego/eino/schema"

type SessionState string

const (
	SessionStateIdle      SessionState = "idle"
	SessionStateActive    SessionState = "active"
	SessionStatePaused    SessionState = "paused"
	SessionStateCompleted SessionState = "completed"
	SessionStateError     SessionState = "error"
)

type SessionMetadata struct {
	UserID         string   `json:"userId"`
	Topic          string   `json:"topic"`
	QuestionsAsked int      `json:"questionAsked"`
	TotalScore     float64  `json:"totalScore"`
	Dimensions     []string `json:"dimensions"`
}

type CheckPoints struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"sessionId"`
	CreatedAt    int64           `json:"createdAt"`
	MessageIndex int             `json:"messageIndex"`
	State        SessionState    `json:"state"`
	Metadata     SessionMetadata `json:"metaData"`
}

type Session struct {
	ID        string            `json:"id"`
	State     SessionState      `json:"state"`
	CreatedAt int64             `json:"createdAt"`
	UpdatedAt int64             `json:"updatedAt"`
	Messages  []*schema.Message `json:"messages"`
	Metadata  SessionMetadata   `json:"metadata"`
}
