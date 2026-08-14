package context

import (
	queryengine "MyOfferPilot/src/query-engine"
	"context"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const defaultMaxTokens = 100000

type ContextManager struct {
	maxTokens    int
	layers       map[ContextWindowKey]*ContextLayer
	chatModel    model.AgenticModel
	summaryModel string
}

type ContextManagerOptions struct {
	MaxTokens    int
	ChatModel    model.AgenticModel
	SummaryModel string
}

type keyEntities struct {
	topics          []string
	decisions       []string
	userPreferences []string
}

type msgEntry struct {
	role    string
	content string
}

func NewContextManager(opts *ContextManagerOptions) *ContextManager {
	m := &ContextManager{
		maxTokens: defaultMaxTokens,
		layers:    make(map[ContextWindowKey]*ContextLayer),
	}

	if opts != nil {
		if opts.MaxTokens > 0 {
			m.maxTokens = opts.MaxTokens

		}
		m.chatModel = opts.ChatModel
		m.summaryModel = opts.SummaryModel
	}
	return m
}

func (cm *ContextManager) SetLayer(name ContextWindowKey, content string, priority int) {
	if priority < 0 {
		priority = cm.defaultPriority(name)
	}

	cm.layers[name] = &ContextLayer{
		Name:       string(name),
		Priority:   priority,
		Content:    content,
		TokenCount: cm.estimateTokens(content),
	}

}

func (cm *ContextManager) BuildSystemPrompt() string {
	sorted := make([]*ContextLayer, 0, len(cm.layers))

	for _, layer := range cm.layers {
		if layer != nil && layer.Content != "" {
			sorted = append(sorted, layer)
		}
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	parts := make([]string, len(sorted))
	for i, layer := range sorted {
		parts[i] = layer.Content
	}

	return strings.Join(parts, "\n\n")
}

func (cm *ContextManager) defaultPriority(name ContextWindowKey) int {
	switch name {
	case ContextWindowKeySystem:
		return 100
	case ContextWindowKeyImmediate:
		return 90
	case ContextWindowKeyKnowledge:
		return 80
	case ContextWindowKeyMemory:
		return 60
	case ContextWindowKeySession:
		return 40
	default:
		return 50
	}
}

// summaryMarker prefixes the compressed-history message so it can be
// detected and incrementally updated on subsequent Compress calls.
const summaryMarker = "[对话历史摘要]"

func (cm *ContextManager) normTarget(t int) int {
	if t <= 0 {
		// Default working context budget: 8000 tokens (~32000 chars).
		// This leaves room for model response (4096 tokens) and system prompt.
		return 8000
	}
	return t
}

func compressionLevel(aggressive bool) CompressionLevel {
	if aggressive {
		return CompressionLevelAggressive
	}
	return CompressionLevelSummary
}

func (cm *ContextManager) Compress(messages []queryengine.Message, targetTokens int) CompressionResult {
	target := cm.normTarget(targetTokens)
	orig := cm.estimateMessagesTokens(messages)
	if orig <= target {
		return CompressionResult{Messages: messages, Level: CompressionLevelNone, OriginalTokens: orig, CompressedTokens: orig}
	}

	// 1. Detect existing progressive summary (first message starting with the marker)
	var priorSummary string
	startIdx := 0
	if len(messages) > 0 && messages[0].Role == queryengine.MessageRoleUser && messages[0].Content != nil {
		if strings.HasPrefix(*messages[0].Content, summaryMarker) {
			priorSummary = *messages[0].Content
			startIdx = 1
		}
	}

	// 2. Group remaining messages into exchanges (user → assistant [+ tool results])
	restMessages := messages[startIdx:]
	exchanges := groupExchanges(restMessages)

	// 3. Walk backwards, keep exchanges that fit within token budget
	minKeep := 2
	var keptExchanges [][]queryengine.Message
	var keptTokens int
	for i := len(exchanges) - 1; i >= 0; i-- {
		excTokens := cm.estimateMessagesTokens(exchanges[i])
		if keptTokens+excTokens > target && len(keptExchanges) >= minKeep {
			break
		}
		keptExchanges = append([][]queryengine.Message{exchanges[i]}, keptExchanges...)
		keptTokens += excTokens
	}

	// 4. Build progressive summary: merge prior summary with newly evicted exchanges
	olderCount := len(exchanges) - len(keptExchanges)
	var olderMessages []queryengine.Message
	for i := 0; i < olderCount; i++ {
		olderMessages = append(olderMessages, exchanges[i]...)
	}

	aggressive := float64(orig)/float64(target) >= 2
	newSummary := cm.buildProgressiveSummary(priorSummary, olderMessages, aggressive)

	// 5. Assemble: summary message + kept exchanges
	var result []queryengine.Message
	result = append(result, queryengine.Message{Role: queryengine.MessageRoleUser, Content: &newSummary})
	for _, exc := range keptExchanges {
		result = append(result, exc...)
	}

	return CompressionResult{
		Messages:         result,
		Level:            compressionLevel(aggressive),
		OriginalTokens:   orig,
		CompressedTokens: cm.estimateMessagesTokens(result),
	}
}

func (cm *ContextManager) CompressAgentic(messages []*schema.AgenticMessage, targetTokens int) AgenticCompressionResult {
	target := cm.normTarget(targetTokens)
	orig := cm.estimateAgenticMessagesTokens(messages)
	if orig <= target {
		return AgenticCompressionResult{Messages: messages, Level: CompressionLevelNone, OriginalTokens: orig, CompressedTokens: orig}
	}
	aggressive := float64(orig)/float64(target) >= 2
	compressed := cm.summarizeAgentic(messages, aggressive)
	return AgenticCompressionResult{
		Messages:         compressed,
		Level:            compressionLevel(aggressive),
		OriginalTokens:   orig,
		CompressedTokens: cm.estimateAgenticMessagesTokens(compressed),
	}
}

func (cm *ContextManager) CompressAgenticAsync(ctx context.Context, messages []*schema.AgenticMessage, targetTokens int) (AgenticCompressionResult, error) {
	target := cm.normTarget(targetTokens)
	orig := cm.estimateAgenticMessagesTokens(messages)
	if orig <= target {
		return AgenticCompressionResult{Messages: messages, Level: CompressionLevelNone, OriginalTokens: orig, CompressedTokens: orig}, nil
	}
	aggressive := float64(orig)/float64(target) >= 2
	compressed, err := cm.llmSummarizeAgentic(ctx, messages, aggressive)
	if err != nil {
		compressed = cm.summarizeAgentic(messages, aggressive)
	}
	return AgenticCompressionResult{
		Messages:         compressed,
		Level:            compressionLevel(aggressive),
		OriginalTokens:   orig,
		CompressedTokens: cm.estimateAgenticMessagesTokens(compressed),
	}, nil
}

// groupExchanges splits a flat message list into exchanges.
// An exchange starts with a user message and includes everything until the next
// user message (assistant replies + tool call/result pairs).
func groupExchanges(messages []queryengine.Message) [][]queryengine.Message {
	if len(messages) == 0 {
		return nil
	}
	var exchanges [][]queryengine.Message
	var current []queryengine.Message
	for _, m := range messages {
		if m.Role == queryengine.MessageRoleUser && len(current) > 0 {
			exchanges = append(exchanges, current)
			current = nil
		}
		current = append(current, m)
	}
	if len(current) > 0 {
		exchanges = append(exchanges, current)
	}
	return exchanges
}

// buildProgressiveSummary merges a prior summary with newly evicted messages.
func (cm *ContextManager) buildProgressiveSummary(priorSummary string, olderMessages []queryengine.Message, aggressive bool) string {
	if len(olderMessages) == 0 {
		if priorSummary != "" {
			return priorSummary
		}
		return summaryMarker
	}

	entities := extractQueryEntities(olderMessages)
	newPart := buildSummaryText(entities, formatQueryExchanges(olderMessages), aggressive)

	if priorSummary == "" {
		return newPart
	}

	// Merge: strip marker from prior, combine key sections
	priorBody := strings.TrimPrefix(priorSummary, summaryMarker+"\n")
	priorBody = strings.TrimPrefix(priorBody, summaryMarker)

	// For prior summary we keep: topics, decisions, tech points
	// For new summary we extract: topics, decisions, user preferences, recent exchanges
	// The merged result prioritizes recent info while preserving accumulated knowledge

	return summaryMarker + "\n" + mergeSummaries(priorBody, newPart, aggressive)
}

// mergeSummaries combines two summary blocks, keeping the total under ~1200 chars.
func mergeSummaries(prior, newPart string, aggressive bool) string {
	newBody := strings.TrimPrefix(newPart, summaryMarker+"\n")
	newBody = strings.TrimPrefix(newBody, summaryMarker)

	if aggressive {
		// Aggressive mode: keep only the new summary + topic list from prior
		topics := extractSection(prior, "讨论主题")
		if topics != "" && !strings.Contains(newBody, topics) {
			return newBody + " | 历史主题: " + topics
		}
		return newBody
	}

	// Non-aggressive: concatenate, truncate if too long
	combined := prior + "\n---\n" + newBody
	if len(combined) > 1500 {
		// Keep last ~1000 chars + topic header from prior
		if idx := strings.Index(prior, "\n"); idx > 0 && idx < 100 {
			prior = prior[:idx] // just keep the first line (topic header)
		} else if len(prior) > 200 {
			prior = prior[:200]
		}
		if len(newBody) > 1000 {
			newBody = newBody[:1000]
		}
		combined = prior + "\n" + newBody
	}
	return combined
}

// extractSection pulls a named section value from a summary string.
func extractSection(summary, sectionName string) string {
	prefix := sectionName + "： "
	if idx := strings.Index(summary, prefix); idx >= 0 {
		start := idx + len(prefix)
		end := strings.IndexAny(summary[start:], "\n|")
		if end < 0 {
			return strings.TrimSpace(summary[start:])
		}
		return strings.TrimSpace(summary[start : start+end])
	}
	return ""
}

func (cm *ContextManager) summarizeQuery(messages []queryengine.Message, aggressive bool) []queryengine.Message {
	exchanges := groupExchanges(messages)
	if len(exchanges) <= 2 {
		return messages
	}

	// Keep last 2 exchanges, summarize the rest
	olderMessages := make([]queryengine.Message, 0)
	for i := 0; i < len(exchanges)-2; i++ {
		olderMessages = append(olderMessages, exchanges[i]...)
	}
	recentMessages := make([]queryengine.Message, 0)
	for i := len(exchanges) - 2; i < len(exchanges); i++ {
		recentMessages = append(recentMessages, exchanges[i]...)
	}

	entities := extractQueryEntities(olderMessages)
	text := buildSummaryText(entities, formatQueryExchanges(olderMessages), aggressive)
	return append([]queryengine.Message{{Role: queryengine.MessageRoleUser, Content: &text}}, recentMessages...)
}

func (cm *ContextManager) summarizeAgentic(messages []*schema.AgenticMessage, aggressive bool) []*schema.AgenticMessage {
	keep := max(6, len(messages)*4/10)
	if aggressive {
		keep = 4
	}
	if len(messages) <= keep {
		return messages
	}
	older, recent := messages[:len(messages)-keep], messages[len(messages)-keep:]

	entities := extractAgenticEntities(older)
	text := buildSummaryText(entities, formatAgenticExchanges(older), aggressive)
	return append([]*schema.AgenticMessage{schema.UserAgenticMessage(text)}, recent...)
}

// ==================== summary text builder ====================
func buildSummaryText(entries keyEntities, recentExchanges string, aggressive bool) string {
	var parts []string

	if len(entries.topics) > 0 {
		parts = append(parts, "讨论主题： "+strings.Join(entries.topics, "、"))
	}
	if len(entries.decisions) > 0 {
		parts = append(parts, "关键决策："+strings.Join(entries.decisions, ";"))
	}

	if aggressive {
		content := "[对话上下文： "
		if len(parts) > 0 {
			content += strings.Join(parts, " | ")
		} else {
			content += "无关键实体"
		}
		return content + "]"
	}

	if len(entries.userPreferences) > 0 {
		parts = append(parts, "用户偏好： "+strings.Join(entries.userPreferences, ";"))
	}
	if recentExchanges != "" {
		parts = append(parts, "最近交流：\n"+recentExchanges)
	}

	return summaryMarker + "\n" + strings.Join(parts, "\n")
}

// ==================== entity extraction ====================提取消息的主题，决策，用户偏好
func extractQueryEntities(messages []queryengine.Message) keyEntities {
	entries := make([]msgEntry, 0, len(messages))
	for _, m := range messages {
		if m.Content != nil && *m.Content != "" {
			entries = append(entries, msgEntry{role: string(m.Role), content: *m.Content})
		}
	}
	return extractKeyEntities(entries)
}

func extractAgenticEntities(messages []*schema.AgenticMessage) keyEntities {
	var entries []msgEntry
	for _, m := range messages {
		role := agenticRoleName(m.Role)
		for _, block := range m.ContentBlocks {
			if content := extractBlockText(block); content != "" {
				entries = append(entries, msgEntry{role: role, content: content})
			}
		}
	}
	return extractKeyEntities(entries)
}

// techKeywords used by entity extraction to detect technical discussion topics.
var techKeywords = []string{
	"Agent", "RAG", "LLM", "embedding", "向量", "ReAct", "Tool Call", "Function Call",
	"Prompt", "微调", "训练", "推理", "大模型", "GPT", "Claude", "LangChain",
	"Docker", "Kubernetes", "K8s", "微服务", "架构", "分布式", "高并发",
	"Python", "Go", "Java", "TypeScript", "Rust", "C++",
	"PostgreSQL", "MySQL", "Redis", "MongoDB", "Elasticsearch", "Milvus",
	"评测", "benchmark", "延迟", "QPS", "吞吐", "性能",
	"Context Window", "Token", "Chunk", "Rerank", "HyDE",
	"System Prompt", "Memory", "Session", "Hook", "Permission",
	"STAR", "简历", "面试", "JD", "offer",
}

func extractKeyEntities(entries []msgEntry) keyEntities {
	topics := make(map[string]bool)
	var decisions, userPreferences, techPoints []string

	for _, e := range entries {
		isUser := e.role == "user"
		content := e.content
		contentLower := strings.ToLower(content)

		// Extract topics from user first-lines (keep existing logic)
		if isUser {
			firstLine := content
			if idx := strings.Index(content, "\n"); idx != -1 {
				firstLine = content[:idx]
			}
			if len(firstLine) > 80 {
				firstLine = firstLine[:80]
			}
			if len(firstLine) > 5 {
				topics[firstLine] = true
			}
		}

		// Detect tech keywords in any message
		for _, kw := range techKeywords {
			kwLower := strings.ToLower(kw)
			if strings.Contains(contentLower, kwLower) && len(kw) >= 3 {
				topics[kw] = true
			}
		}

		// Detect decisions by keyword
		if strings.Contains(content, "决定") || strings.Contains(content, "选择") ||
			strings.Contains(content, "确认") || strings.Contains(content, "采用") ||
			strings.Contains(content, "方案是") {
			for _, s := range strings.Split(content, "。") {
				hasKeyword := strings.Contains(s, "决定") || strings.Contains(s, "选择") ||
					strings.Contains(s, "确认") || strings.Contains(s, "采用") ||
					strings.Contains(s, "方案是")
				if hasKeyword {
					s = strings.TrimSpace(s)
					if len(s) < 120 && len(s) > 0 {
						decisions = append(decisions, s)
						if len(decisions) >= 3 {
							break
						}
					}
				}
			}
		}

		// Detect user preferences
		if isUser && (strings.Contains(content, "我想") || strings.Contains(content, "我要") ||
			strings.Contains(content, "我希望") || strings.Contains(content, "我需要") ||
			strings.Contains(content, "目标是")) {
			for _, s := range strings.Split(content, "。") {
				hasPref := strings.Contains(s, "我想") || strings.Contains(s, "我要") ||
					strings.Contains(s, "我希望") || strings.Contains(s, "我需要") ||
					strings.Contains(s, "目标是")
				if hasPref {
					s = strings.TrimSpace(s)
					if len(s) < 100 && len(s) > 0 {
						userPreferences = append(userPreferences, s)
						if len(userPreferences) >= 3 {
							break
						}
					}
				}
			}
		}

		// Detect technical details worth preserving
		if len(content) > 60 && (strings.Contains(contentLower, "原理") ||
			strings.Contains(contentLower, "本质") || strings.Contains(contentLower, "核心") ||
			strings.Contains(contentLower, "关键") || strings.Contains(contentLower, "底层")) {
			s := strings.TrimSpace(content)
			if len(s) > 150 {
				s = s[:150]
			}
			techPoints = append(techPoints, s)
			if len(techPoints) >= 5 {
				techPoints = techPoints[:5]
			}
		}
	}

	var topicList []string
	for t := range topics {
		topicList = append(topicList, t)
	}
	if len(topicList) > 8 {
		topicList = topicList[:8]
	}

	return keyEntities{
		topics:          topicList,
		decisions:       decisions,
		userPreferences: userPreferences,
	}
}

// ==================== recent exchanges formatting ====================
func formatQueryExchanges(older []queryengine.Message) string {
	if len(older) < 4 {
		return ""
	}

	var sb strings.Builder
	for _, m := range older[len(older)-4:] {
		if m.Content != nil && *m.Content != "" {
			content := *m.Content
			if len(content) > 150 {
				content = content[:150]
			}
			sb.WriteString("[")
			sb.WriteString(string(m.Role))
			sb.WriteString("]:")
			sb.WriteString(content)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func formatAgenticExchanges(older []*schema.AgenticMessage) string {
	if len(older) < 4 {
		return ""
	}

	var sb strings.Builder
	for _, m := range older[len(older)-4:] {
		role := agenticRoleName(m.Role)

		for _, block := range m.ContentBlocks {
			content := extractBlockText(block)
			if content != "" {
				if len(content) > 150 {
					content = content[:150]
				}

				sb.WriteString("[")
				sb.WriteString(role)
				sb.WriteString("]:")
				sb.WriteString(content)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func (cm *ContextManager) llmSummarizeAgentic(ctx context.Context, messages []*schema.AgenticMessage, aggressive bool) ([]*schema.AgenticMessage, error) {
	keep := max(6, len(messages)*4/10)
	if aggressive {
		keep = 4
	}
	if len(messages) <= keep {
		return messages, nil
	}
	older, recent := messages[:len(messages)-keep], messages[len(messages)-keep:]

	if cm.chatModel == nil {
		return cm.summarizeAgentic(messages, false), nil
	}

	var entries []msgEntry
	for _, m := range older {
		role := agenticRoleName(m.Role)
		for _, block := range m.ContentBlocks {
			if content := extractBlockText(block); content != "" {
				entries = append(entries, msgEntry{role: role, content: content})
			}
		}
	}

	text, err := cm.callSummaryLLM(ctx, entries, aggressive)
	if err != nil {
		return nil, err
	}
	summary := schema.UserAgenticMessage("[对话历史摘要]\n" + text)
	return append([]*schema.AgenticMessage{summary}, recent...), nil
}

func (cm *ContextManager) callSummaryLLM(ctx context.Context, entries []msgEntry, aggressive bool) (string, error) {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString("[")
		sb.WriteString(e.role)
		sb.WriteString("]: ")
		sb.WriteString(e.content)
		sb.WriteString("\n")
	}

	var prompt string
	if aggressive {
		prompt = "以下是一段对话历史，请提取关键实体、决策和结论，用3-5个要点总结(中文)：\n\n" + sb.String()
	} else {
		prompt = "以下是一段对话历史，请保留关键信息和上下文，用简洁的摘要概括（中文），保留重要的技术细节和用户偏好：\n\n" + sb.String()
	}

	opts := []model.Option{model.WithMaxTokens(512)}
	if cm.summaryModel != "" {
		opts = append(opts, model.WithModel(cm.summaryModel))
	}

	response, err := cm.chatModel.Generate(ctx, []*schema.AgenticMessage{
		schema.SystemAgenticMessage("你是一个对话历史摘要助手，只输出摘要内容，不加额外说明。"),
		schema.UserAgenticMessage(prompt),
	}, opts...)

	if err != nil {
		return "", err
	}
	text := extractResponseText(response)
	if text == "" {
		text = "无法生成摘要"
	}
	return text, nil

}

// ==================== Agentic helpers ====================
func agenticRoleName(role schema.AgenticRoleType) string {
	switch role {
	case schema.AgenticRoleTypeSystem:
		return "system"
	case schema.AgenticRoleTypeUser:
		return "user"
	case schema.AgenticRoleTypeAssistant:
		return "assistant"
	default:
		return "user"
	}

}

func extractBlockText(block *schema.ContentBlock) string {
	switch {
	case block.Type == schema.ContentBlockTypeAssistantGenText && block.AssistantGenText != nil:
		return block.AssistantGenText.Text
	case block.Type == schema.ContentBlockTypeUserInputText && block.UserInputText != nil:
		return block.UserInputText.Text
	case block.Type == schema.ContentBlockTypeReasoning && block.Reasoning != nil:
		return block.Reasoning.Text

	}
	return ""
}

func extractResponseText(response *schema.AgenticMessage) string {
	var text string
	for _, block := range response.ContentBlocks {
		if block.Type == schema.ContentBlockTypeAssistantGenText && block.AssistantGenText != nil {
			text += block.AssistantGenText.Text
		}
	}
	return text
}

// ==================== token estimation ====================
func (cm *ContextManager) estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

func (cm *ContextManager) estimateMessagesTokens(messages []queryengine.Message) int {
	sum := 0
	for _, m := range messages {
		if m.Content != nil {
			sum += cm.estimateTokens(*m.Content)
		}
	}
	return sum
}

func (cm *ContextManager) estimateAgenticMessagesTokens(messages []*schema.AgenticMessage) int {
	sum := 0
	for _, m := range messages {
		for _, block := range m.ContentBlocks {
			if block.Type == schema.ContentBlockTypeAssistantGenText && block.AssistantGenText != nil {
				sum += cm.estimateTokens(block.AssistantGenText.Text)
			} else if block.Type == schema.ContentBlockTypeUserInputText && block.UserInputText != nil {
				sum += cm.estimateTokens(block.UserInputText.Text)
			} else if block.Type == schema.ContentBlockTypeReasoning && block.Reasoning != nil {
				sum += cm.estimateTokens(block.Reasoning.Text)
			}
		}
	}
	return sum
}
