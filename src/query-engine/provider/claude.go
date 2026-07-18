package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	queryengine "MyOfferPilot/src/query-engine"
)

type ClaudeProvider struct {
	name      string
	chatModel model.ChatModel
}

type ClaudeConfig struct {
	APIKey      string
	Model       string
	BaseURL     string
	MaxTokens   int
	Temperature float64
}

func NewClaudeProvider(ctx context.Context, config *ClaudeConfig) (*ClaudeProvider, error) {
	var baseURL *string
	if config.BaseURL != "" {
		baseURL = &config.BaseURL
	}

	var temperature *float32
	if config.Temperature > 0 {
		t := float32(config.Temperature)
		temperature = &t
	}

	claudeConfig := &claude.Config{
		APIKey:      config.APIKey,
		Model:       config.Model,
		BaseURL:     baseURL,
		MaxTokens:   config.MaxTokens,
		Temperature: temperature,
	}

	chatModel, err := claude.NewChatModel(ctx, claudeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create claude chat model: %w", err)
	}

	return &ClaudeProvider{
		name:      "claude",
		chatModel: chatModel,
	}, nil
}

func (p *ClaudeProvider) Name() string {
	return p.name
}

func (p *ClaudeProvider) Stream(params queryengine.StreamParams) <-chan queryengine.StreamEvent {
	events := make(chan queryengine.StreamEvent)

	go func() {
		defer close(events)

		ctx := context.Background()
		if params.AbortSignal != nil {
			ctx = params.AbortSignal
		}

		messages := convertToMessages(params.Messages)

		opts := []model.Option{
			model.WithModel(params.Model),
		}

		if params.MaxTokens != 0 {
			opts = append(opts, model.WithMaxTokens(params.MaxTokens))
		}
		if params.Temperature != 0 {
			opts = append(opts, model.WithTemperature(float32(params.Temperature)))
		}
		if params.SystemPrompt != "" {
			messages = append([]*schema.Message{schema.SystemMessage(params.SystemPrompt)}, messages...)
		}

		reader, err := p.chatModel.Stream(ctx, messages, opts...)
		if err != nil {
			events <- &queryengine.ErrorEvent{Err: err}
			return
		}

		defer reader.Close()

		for {
			msg, err := reader.Recv()
			if err != nil {
				if err != io.EOF {
					events <- &queryengine.ErrorEvent{Err: err}
				}
				return
			}

			if msg.Content != "" {
				events <- &queryengine.TextDeltaEvent{
					Content: msg.Content,
				}
			}

			if msg.ToolCalls != nil {
				for _, tc := range msg.ToolCalls {
					events <- &queryengine.ToolUseStartEvent{
						ID:   tc.ID,
						Name: tc.Function.Name,
					}

					inputJSON, _ := json.Marshal(tc.Function.Arguments)
					events <- &queryengine.ToolUseDeltaEvent{
						Input: string(inputJSON),
					}

					events <- &queryengine.ToolUseEndEvent{}
				}
			}

			if msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
				events <- &queryengine.MessageEndEvent{
					Usage: queryengine.TokenUsage{
						InputTokens:  msg.ResponseMeta.Usage.PromptTokens,
						OutputTokens: msg.ResponseMeta.Usage.CompletionTokens,
					},
					StopReason: mapStopReason(msg.ResponseMeta.FinishReason),
				}
			}
		}
	}()

	return events
}

func (p *ClaudeProvider) CountTokens(messages []queryengine.Message, tools []queryengine.ToolSchema, modelName string) (int, error) {
	ctx := context.Background()

	agenticMessages := convertToMessages(messages)

	result, err := p.chatModel.Generate(ctx, agenticMessages, model.WithModel(modelName))
	if err != nil {
		return 0, fmt.Errorf("failed to count tokens: %w", err)
	}

	if result.ResponseMeta != nil && result.ResponseMeta.Usage != nil {
		return result.ResponseMeta.Usage.PromptTokens, nil
	}

	return 0, nil
}

func convertToMessages(messages []queryengine.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))

	for _, msg := range messages {
		var agenticMsg *schema.Message

		switch msg.Role {
		case queryengine.MessageRoleSystem:
			if msg.Content != "" {
				agenticMsg = schema.SystemMessage(msg.Content)
			}
		case queryengine.MessageRoleUser:
			if msg.Content != "" {
				agenticMsg = schema.UserMessage(msg.Content)
			}
		case queryengine.MessageRoleAssistant:
			if msg.ToolCalls != nil && len(msg.ToolCalls) > 0 {
				toolCalls := make([]schema.ToolCall, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					var argsJSON string
					if tc.Input != nil {
						argsBytes, _ := json.Marshal(tc.Input)
						argsJSON = string(argsBytes)
					}
					toolCalls = append(toolCalls, schema.ToolCall{
						ID: tc.ID,
						Function: schema.FunctionCall{
							Name:      tc.Name,
							Arguments: argsJSON,
						},
					})
				}
				agenticMsg = schema.AssistantMessage(msg.Content, toolCalls)
			} else if msg.Content != "" {
				agenticMsg = schema.AssistantMessage(msg.Content, nil)
			}
		case queryengine.MessageRoleTool:
			if msg.Content != "" && msg.ToolCallID != "" {
				agenticMsg = schema.ToolMessage(msg.Content, msg.ToolCallID)
			}
		}

		if agenticMsg != nil {
			result = append(result, agenticMsg)
		}
	}
	return result
}

func mapStopReason(reason string) queryengine.StopReason {
	switch reason {
	case "tool_calls":
		return queryengine.StopReasonToolUse
	case "length":
		return queryengine.StopReasonMaxTokens
	default:
		return queryengine.StopReasonEndTurn
	}
}