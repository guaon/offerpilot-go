package queryengine

import "context"

type StopReason string
type ResponseType string
type MessageRole string

const (
	TextResponse    ResponseType = "text"
	ToolUseResponse ResponseType = "tool_use"
)

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonToolUse   StopReason = "tool_use"
	StopReasonMaxTokens StopReason = "max_tokens"
)

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

type ToolCall struct {
	ID    string
	Name  string
	Input map[string]interface{}
}

type TokenUsage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

type ParsedResponse struct {
	Type       ResponseType
	Content    *string
	ToolCalls  *[]ToolCall
	Usage      TokenUsage
	StopReason StopReason
}

type StreamEvent interface {
	GetType() string
}

// TextDeltaEvent represents a text delta event
type TextDeltaEvent struct {
	Content string `json:"content"`
}

func (e *TextDeltaEvent) GetType() string { return "text_delta" }

// ThinkingDeltaEvent represents a thinking delta event
type ThinkingDeltaEvent struct {
	Content string `json:"content"`
}

func (e *ThinkingDeltaEvent) GetType() string { return "thinking_delta" }

// ToolUseStartEvent represents the start of a tool use
type ToolUseStartEvent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (e *ToolUseStartEvent) GetType() string { return "tool_use_start" }

// ToolUseDeltaEvent represents a tool use delta event
type ToolUseDeltaEvent struct {
	Input string `json:"input"`
}

func (e *ToolUseDeltaEvent) GetType() string { return "tool_use_delta" }

// ToolUseEndEvent represents the end of a tool use
type ToolUseEndEvent struct{}

func (e *ToolUseEndEvent) GetType() string { return "tool_use_end" }

// MessageEndEvent represents the end of a message
type MessageEndEvent struct {
	Usage      TokenUsage `json:"usage"`
	StopReason StopReason `json:"stopReason"`
}

func (e *MessageEndEvent) GetType() string { return "message_end" }

type ErrorEvent struct {
	Err error `json:"-"`
}

func (e *ErrorEvent) GetType() string { return "error" }

type Message struct {
	Role       MessageRole
	Content    *string
	ToolCallID *string
	ToolCalls  *[]ToolCall
	IsError    *bool
}

type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

type StreamParams struct {
	Model        string
	Messages     []Message
	Tools        []ToolSchema
	MaxTokens    int
	Temperature  float64
	SystemPrompt string
	AbortSignal  context.Context
}

type QueryParams struct {
	Task            *string
	Model           *string
	Messages        []Message
	Tools           *[]ToolSchema
	MaxTokens       *int
	Temperature     *float64
	SystemPrompt    *string
	UseCache        *bool
	CacheTtl        *int
	OnTextDelta     func(text string)
	OnThinkingDelta func(text string)
}

type LLMProvider interface {
	Name() string
	Stream(params StreamParams) <-chan StreamEvent
	CountTokens(message []Message, tools []ToolSchema, model string) (int, error)
}

func NewMessageWithContent(role MessageRole, content string) *Message {
	return &Message{Role: role, Content: &content}
}
