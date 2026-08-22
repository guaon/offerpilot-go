package context

import (
	"testing"

	queryengine "MyOfferPilot/src/query-engine"
)

func TestGroupExchanges(t *testing.T) {
	msg := func(role queryengine.MessageRole, content string) queryengine.Message {
		return queryengine.Message{Role: role, Content: &content}
	}

	messages := []queryengine.Message{
		msg(queryengine.MessageRoleUser, "问题1"),
		msg(queryengine.MessageRoleAssistant, "回答1"),
		msg(queryengine.MessageRoleUser, "问题2"),
		msg(queryengine.MessageRoleAssistant, "回答2"),
		msg(queryengine.MessageRoleUser, "问题3"),
	}

	exchanges := groupExchanges(messages)

	if len(exchanges) != 3 {
		t.Fatalf("期望 3 个 exchange，得到 %d", len(exchanges))
	}
	if len(exchanges[0]) != 2 {
		t.Errorf("期望 exchange[0] 有 2 条消息，得到 %d", len(exchanges[0]))
	}
	if len(exchanges[2]) != 1 {
		t.Errorf("期望 exchange[2] 有 1 条消息，得到 %d", len(exchanges[2]))
	}
}

func TestCompressNoCompression(t *testing.T) {
	cm := NewContextManager(nil)

	msg := func(role queryengine.MessageRole, content string) queryengine.Message {
		return queryengine.Message{Role: role, Content: &content}
	}
	messages := []queryengine.Message{
		msg(queryengine.MessageRoleUser, "你好"),
		msg(queryengine.MessageRoleAssistant, "你好，有什么可以帮你"),
	}

	// targetTokens 很大，不触发压缩
	result := cm.Compress(messages, 100000)

	if result.Level != CompressionLevelNone {
		t.Errorf("期望不压缩，得到 Level=%s", result.Level)
	}
	if len(result.Messages) != 2 {
		t.Errorf("期望原样 2 条消息，得到 %d", len(result.Messages))
	}
}

func TestCompressTriggersAndKeepsRecent(t *testing.T) {
	cm := NewContextManager(nil)

	msg := func(role queryengine.MessageRole, content string) queryengine.Message {
		return queryengine.Message{Role: role, Content: &content}
	}

	// 构造多轮长对话，超过很小的 targetTokens
	var messages []queryengine.Message
	for i := 0; i < 10; i++ {
		messages = append(messages, msg(queryengine.MessageRoleUser, "这是一个很长的问题内容用于触发压缩机制的第"+string(rune('0'+i))+"轮"))
		messages = append(messages, msg(queryengine.MessageRoleAssistant, "这是对应的很长回答内容用于测试压缩逻辑是否正确保留最近交换"))
	}

	// targetTokens 很小，必然触发压缩
	result := cm.Compress(messages, 50)

	if result.Level == CompressionLevelNone {
		t.Fatal("期望触发压缩，得到 Level=none")
	}

	// 压缩后第一条应该是摘要消息（user 角色，带摘要标记）
	if len(result.Messages) == 0 {
		t.Fatal("压缩后消息为空")
	}
	first := result.Messages[0]
	if first.Role != queryengine.MessageRoleUser {
		t.Errorf("期望第一条是摘要消息（user），得到 %s", first.Role)
	}
	if first.Content == nil || len(*first.Content) == 0 {
		t.Error("摘要内容不应为空")
	}

	// 压缩后总 token 应小于原始
	if result.CompressedTokens >= result.OriginalTokens {
		t.Errorf("期望压缩后 token 减少，原始=%d 压缩=%d", result.OriginalTokens, result.CompressedTokens)
	}
}
