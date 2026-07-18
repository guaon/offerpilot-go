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

type messageText struct {
	role string
	text string
}

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

func NewContextManagerWithMaxTokens(maxTokens int) *ContextManager {
	return &ContextManager{
		maxTokens: maxTokens,
		layers:    make(map[ContextWindowKey]*ContextLayer),
	}
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

func (cm *ContextManager) normTarget(t int) int {
	if t <= 0 {
		return cm.maxTokens * 6 / 10
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
	aggressive := float64(orig)/float64(target) >= 2
	compressed := cm.summarizeQuery(messages, aggressive)
	return CompressionResult{
		Messages:         compressed,
		Level:            compressionLevel(aggressive),
		OriginalTokens:   orig,
		CompressedTokens: cm.estimateMessagesTokens(compressed),
	}

}

func (cm *ContextManager) CompressAsync(ctx context.Context, messages []queryengine.Message, targetTokens int) (CompressionResult, error) {
	target := cm.normTarget(targetTokens)
	orig := cm.estimateMessagesTokens(messages)
	if orig <= target {
		return CompressionResult{Messages: messages, Level: CompressionLevelNone, OriginalTokens: orig, CompressedTokens: orig}, nil
	}
	aggressive := float64(orig)/float64(target) >= 2
	compressed, err := cm.llmSummarizeQuery(ctx, messages, aggressive)
	if err != nil {
		compressed = cm.summarizeQuery(messages, aggressive)
	}

	return CompressionResult{
		Messages:         compressed,
		Level:            compressionLevel(aggressive),
		OriginalTokens:   orig,
		CompressedTokens: cm.estimateMessagesTokens(compressed),
	}, nil

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

func (cm *ContextManager) summarizeQuery(messages []queryengine.Message, aggressive bool) []queryengine.Message {
	keep := max(6, len(messages)*4/10)
	if aggressive {
		keep = 4
	}
	if len(messages) <= keep {
		return messages
	}
	older, recent := messages[:len(messages)-keep], messages[len(messages)-keep:] //分割消息，older为需要压缩的消息，recent为保留的消息

	entities := extractQueryEntities(older) //从older中提取关键实体，包括主题，决策和用户偏好等
	text := buildSummaryText(entities, formatQueryExchanges(older), aggressive)
	return append([]queryengine.Message{{Role: queryengine.MessageRoleUser, Content: &text}}, recent...)
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

// ==================== summary text builder ====================将提取的关键信息和最近的对话组装成一段结构化的总结文本。
func buildSummaryText(entries keyEntities, recentExchanges string, aggressive bool) string {
	var parts []string
	if len(entries.topics) > 0 {
		parts = append(parts, "讨论主题： "+strings.Join(entries.topics, ","))
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

	//非激进模式保留用户偏好和格式化的对话文本
	if len(entries.userPreferences) > 0 {
		parts = append(parts, "用户偏好： "+strings.Join(entries.userPreferences, ";"))
	}
	if recentExchanges != "" {
		parts = append(parts, "最近交流：\n"+recentExchanges)
	}

	return "[对话历史摘要]\n" + strings.Join(parts, "\n")

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

func extractKeyEntities(entries []msgEntry) keyEntities {
	topics := make(map[string]bool)
	var decisions, userPreferences []string

	for _, e := range entries {
		isUser := e.role == "user"
		content := e.content

		if isUser {
			firstLine := content
			if idx := strings.Index(content, "\n"); idx != -1 {
				firstLine = content[:idx]
			}
			if len(firstLine) > 60 {
				firstLine = firstLine[:60]
			}
			if len(firstLine) > 5 {
				topics[firstLine] = true
			}
		}

		if strings.Contains(content, "决定") || strings.Contains(content, "选择") || strings.Contains(content, "确认") {
			for _, s := range strings.Split(content, "。") {
				if strings.Contains(s, "决定") || strings.Contains(s, "选择") || strings.Contains(s, "确认") {
					s = strings.TrimSpace(s)
					if len(s) < 100 && len(s) > 0 {
						decisions = append(decisions, s)
						if len(decisions) >= 3 {
							break
						}
					}
				}
			}
		}

		if isUser && (strings.Contains(content, "我想") || strings.Contains(content, "我要") || strings.Contains(content, "我希望")) {
			for _, s := range strings.Split(content, "。") {
				if strings.Contains(s, "我想") || strings.Contains(s, "我要") || strings.Contains(s, "我希望") {
					s = strings.TrimSpace(s)
					if len(s) < 80 && len(s) > 0 {
						userPreferences = append(userPreferences, s)
						if len(userPreferences) >= 3 {
							break
						}
					}
				}
			}
		}
	}

	var topicList []string
	for t := range topics {
		topicList = append(topicList, t)

	}
	if len(topicList) > 5 {
		topicList = topicList[:5]
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

// ==================== recent exchanges formatting ====================
func (cm *ContextManager) llmSummarizeQuery(ctx context.Context, messages []queryengine.Message, aggressive bool) ([]queryengine.Message, error) {
	keep := max(6, len(messages)*4/10)
	if aggressive {
		keep = 4
	}
	if len(messages) <= 4 {
		return messages, nil
	}

	older, recent := messages[:len(messages)-keep], messages[len(messages)-keep:]

	if cm.chatModel == nil {
		return cm.summarizeQuery(messages, false), nil
	}

	entries := make([]msgEntry, 0, len(older))
	for _, m := range older {
		if m.Content != nil && *m.Content != "" {
			entries = append(entries, msgEntry{role: string(m.Role), content: *m.Content})
		}
	}

	text, err := cm.callSummaryLLM(ctx, entries, aggressive)
	if err != nil {
		return nil, err
	}
	summaryText := "[对话历史摘要]\n" + text
	summary := queryengine.Message{Role: queryengine.MessageRoleUser, Content: &summaryText}
	return append([]queryengine.Message{summary}, recent...), nil
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
