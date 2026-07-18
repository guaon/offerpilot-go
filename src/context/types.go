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

type ContextWindow struct {
	System     ContextLayer
	Knowledge  ContextLayer
	Memory     ContextLayer
	Session    ContextLayer
	Immediate  ContextLayer
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

type ContextWindowKey string

const (
	ContextWindowKeySystem    ContextWindowKey = "system"
	ContextWindowKeyKnowledge ContextWindowKey = "knowledge"
	ContextWindowKeyMemory    ContextWindowKey = "memory"
	ContextWindowKeySession   ContextWindowKey = "session"
	ContextWindowKeyImmediate ContextWindowKey = "immediate"
)