package agent

import (
	"testing"

	queryengine "MyOfferPilot/src/query-engine"

	"github.com/cloudwego/eino/schema"
)

func TestConvertFromQueryEngineMessagesPreserveToolCalls(t *testing.T) {
	al := &AgentLoop{}

	toolCalls := []queryengine.ToolCall{
		{ID: "call_1", Name: "search_knowledge"},
		{ID: "call_2", Name: "record_diagnosis"},
	}
	emptyContent := ""

	messages := []queryengine.Message{
		{
			Role:      queryengine.MessageRoleAssistant,
			Content:   &emptyContent,
			ToolCalls: &toolCalls,
		},
	}

	got := al.convertFromQueryEngineMessages(messages)

	if len(got) != 1 {
		t.Fatalf("期望 1 条消息，得到 %d", len(got))
	}
	if got[0].Role != schema.Assistant {
		t.Errorf("期望 assistant 角色，得到 %s", got[0].Role)
	}
	// 关键：tool_calls 必须保留，否则 DeepSeek 报 "content or tool_calls must be set"
	if len(got[0].ToolCalls) != 2 {
		t.Fatalf("期望 2 个 tool_calls，得到 %d（tool_calls 丢失！）", len(got[0].ToolCalls))
	}
	if got[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("期望 tool_call ID=call_1，得到 %s", got[0].ToolCalls[0].ID)
	}
	if got[0].ToolCalls[1].Function.Name != "record_diagnosis" {
		t.Errorf("期望 tool name=record_diagnosis，得到 %s", got[0].ToolCalls[1].Function.Name)
	}
}

func TestConvertFromQueryEngineMessagesPreserveToolCallID(t *testing.T) {
	al := &AgentLoop{}

	toolCallID := "call_tool_1"
	content := "工具结果"

	messages := []queryengine.Message{
		{
			Role:       queryengine.MessageRoleTool,
			Content:    &content,
			ToolCallID: &toolCallID,
		},
	}

	got := al.convertFromQueryEngineMessages(messages)

	if len(got) != 1 {
		t.Fatalf("期望 1 条消息，得到 %d", len(got))
	}
	if got[0].Role != schema.Tool {
		t.Errorf("期望 tool 角色，得到 %s", got[0].Role)
	}
	if got[0].ToolCallID != "call_tool_1" {
		t.Errorf("期望 ToolCallID=call_tool_1，得到 %s（ToolCallID 丢失！）", got[0].ToolCallID)
	}
}
