package context

import (
	"github.com/cloudwego/eino/schema"

	queryengine "MyOfferPilot/src/query-engine"
)

type CompressionLevel string

const (
	CompressionLevelNone       CompressionLevel = "none"
	CompressionLevelSummary    CompressionLevel = "summary"
	CompressionLevelAggressive CompressionLevel = "aggressive"
)

type ContextLayer struct {
	Name       string
	Priority   int
	Content    string
	TokenCount int
}

type CompressionResult struct {
	Messages        []queryengine.Message
	Level           CompressionLevel
	OriginalTokens  int
	CompressedTokens int
}

type AgenticCompressionResult struct {
	Messages        []*schema.AgenticMessage
	Level           CompressionLevel
	OriginalTokens  int
	CompressedTokens int
}

// CompressedMemory holds the progressive summary of older conversation exchanges.
// It is rebuilt each time Compress() runs by detecting an existing summary message
// and accumulating newly evicted exchanges into it.
type CompressedMemory struct {
	Summary       string   `json:"summary"`
	Topics        []string `json:"topics"`
	Decisions     []string `json:"decisions"`
	TechPoints    []string `json:"techPoints"`
	ExchangeCount int      `json:"exchangeCount"`
}

type ContextWindowKey string

const (
	ContextWindowKeySystem    ContextWindowKey = "system"
	ContextWindowKeyKnowledge ContextWindowKey = "knowledge"
	ContextWindowKeyMemory    ContextWindowKey = "memory"
	ContextWindowKeySession   ContextWindowKey = "session"
	ContextWindowKeyImmediate ContextWindowKey = "immediate"
)