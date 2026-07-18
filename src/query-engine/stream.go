package queryengine

import "encoding/json"

type StreamCollector struct {
	Text             string
	ToolCalls        []ToolCall
	CurrentToolInput string
	CurrentToolId    string
	CurrentToolName  string
	Usage            TokenUsage
	StopReason       StopReason
}

func NewStreamCollector() *StreamCollector {
	return &StreamCollector{
		ToolCalls: make([]ToolCall, 0),
		Usage: TokenUsage{
			InputTokens:  0,
			OutputTokens: 0,
		},
		StopReason: StopReasonEndTurn,
	}
}

// Feed 方法负责接收和处理流式事件，将这些事件逐步组装成完整的数据结构（文本内容、工具调用、使用量统计等）
func (sc *StreamCollector) Feed(event StreamEvent) {
	switch event.GetType() {
	case "text_delta":
		if textDelta, ok := event.(*TextDeltaEvent); ok { //将event转换为*TextDeltaEvent指针类型
			sc.Text += textDelta.Content
		}
	case "thinking_delta":
		if thinkingDelta, ok := event.(*ThinkingDeltaEvent); ok {
			_ = thinkingDelta
		}
	case "tool_use_start":
		if toolStart, ok := event.(*ToolUseStartEvent); ok {
			sc.CurrentToolId = toolStart.ID
			sc.CurrentToolName = toolStart.Name
			sc.CurrentToolInput = ""
		}
	case "tool_use_delta":
		if toolDelta, ok := event.(*ToolUseDeltaEvent); ok {
			sc.CurrentToolInput += toolDelta.Input
		}
	case "tool_use_end":
		if _, ok := event.(*ToolUseEndEvent); ok {
			sc.ToolCalls = append(sc.ToolCalls, ToolCall{
				ID:    sc.CurrentToolId,
				Name:  sc.CurrentToolName,
				Input: sc.parseInput(sc.CurrentToolInput),
			})
			sc.CurrentToolId = ""
			sc.CurrentToolInput = ""
			sc.CurrentToolName = ""
		}
	case "message_end":
		if msgEnd, ok := event.(*MessageEndEvent); ok {
			sc.Usage = msgEnd.Usage
			sc.StopReason = msgEnd.StopReason
		}
	}
}

func (sc *StreamCollector) Result() ParsedResponse {
	response := ParsedResponse{
		Usage:      sc.Usage,
		StopReason: sc.StopReason,
	}

	if len(sc.ToolCalls) > 0 {
		response.Type = ToolUseResponse
		response.ToolCalls = &sc.ToolCalls
	} else {
		response.Type = TextResponse
		if sc.Text != "" {
			response.Content = &sc.Text
		}
	}

	return response
}

func (sc *StreamCollector) parseInput(raw string) map[string]interface{} {
	if raw == "" {
		return make(map[string]interface{})
	}

	var result map[string]interface{}
	err := json.Unmarshal([]byte(raw), &result)
	if err != nil {
		return map[string]interface{}{
			"_raw": raw,
		}
	}
	return result
}
