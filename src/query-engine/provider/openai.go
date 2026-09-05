package provider

import (
	queryengine "MyOfferPilot/src/query-engine"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sashabaranov/go-openai"
)

type OpenAIProvider struct {
	name   string
	client *openai.Client
	model  string
}

type OpenAIConfig struct {
	APIKey      string
	Model       string
	BaseURL     string
	Name        string
	MaxTokens   int
	Temperature float64
}

type openAIJSONSchema map[string]interface{}

func (schema openAIJSONSchema) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(schema))
}

func NewOpenAIProvider(ctx context.Context, config *OpenAIConfig) (*OpenAIProvider, error) {
	clientConfig := openai.DefaultConfig(config.APIKey)
	if config.BaseURL != "" {
		clientConfig.BaseURL = config.BaseURL
	}

	name := config.Name
	if name == "" {
		name = "openai"
	}

	return &OpenAIProvider{
		name:   name,
		client: openai.NewClientWithConfig(clientConfig),
		model:  config.Model,
	}, nil
}

func (p *OpenAIProvider) Name() string {
	return p.name
}

func (p *OpenAIProvider) Stream(params queryengine.StreamParams) <-chan queryengine.StreamEvent {
	events := make(chan queryengine.StreamEvent)

	go func() {
		defer close(events)

		ctx := context.Background()
		if params.AbortSignal != nil {
			ctx = params.AbortSignal
		}

		openaiMessages := p.buildMessages(params.Messages)

		if params.SystemPrompt != "" {
			openaiMessages = append([]openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: params.SystemPrompt},
			}, openaiMessages...)
		}

		request := openai.ChatCompletionRequest{
			Model:    params.Model,
			Messages: openaiMessages,
			Stream:   true,
		}

		applyOpenAITokenLimit(&request, params.Model, params.MaxTokens)
		if params.Temperature != 0 && !isOpenAIReasoningModel(params.Model) {
			request.Temperature = float32(params.Temperature)
		}
		if len(params.Tools) > 0 {
			request.Tools = p.toOpenAITools(params.Tools)
			request.ToolChoice = "auto"
		}
		request.ResponseFormat = toOpenAIResponseFormat(params.ResponseFormat)

		stream, err := p.client.CreateChatCompletionStream(ctx, request)
		if err != nil {
			events <- &queryengine.ErrorEvent{Err: err}
			return
		}

		defer stream.Close()

		var currentToolId string
		var currentToolName string

		for {
			response, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					events <- &queryengine.ErrorEvent{Err: err}
				}
				return
			}

			for _, choice := range response.Choices {
				if choice.Delta.ReasoningContent != "" {
					events <- &queryengine.ThinkingDeltaEvent{Content: choice.Delta.ReasoningContent}
				}
				if choice.Delta.Content != "" {
					events <- &queryengine.TextDeltaEvent{Content: choice.Delta.Content}
				}

				if len(choice.Delta.ToolCalls) > 0 {
					for _, tc := range choice.Delta.ToolCalls {
						if tc.ID != "" && tc.Function.Name != "" {
							if currentToolId != "" {
								events <- &queryengine.ToolUseEndEvent{}
							}
							currentToolId = tc.ID
							currentToolName = tc.Function.Name
							events <- &queryengine.ToolUseStartEvent{ID: currentToolId, Name: currentToolName}
						}
						if tc.Function.Arguments != "" {
							events <- &queryengine.ToolUseDeltaEvent{Input: tc.Function.Arguments}
						}
					}
				}

				if choice.FinishReason != "" {
					if currentToolId != "" {
						events <- &queryengine.ToolUseEndEvent{}
						currentToolId = ""
					}
					usage := queryengine.TokenUsage{}
					if response.Usage != nil {
						usage = queryengine.TokenUsage{InputTokens: response.Usage.PromptTokens, OutputTokens: response.Usage.CompletionTokens}
					}
					events <- &queryengine.MessageEndEvent{
						Usage:      usage,
						StopReason: p.mapStopReason(string(choice.FinishReason)),
					}
				}
			}
		}
	}()

	return events
}

func (p *OpenAIProvider) CountTokens(messages []queryengine.Message, tools []queryengine.ToolSchema, modelName string) (int, error) {
	// 使用字符估算而非真实 API 调用，避免浪费配额。
	// 粗略估算：英文约 4 字符/ token，中文约 1.5 字符/ token。
	// 取折中值 2.5 字符/ token。
	total := 0
	for _, m := range messages {
		if m.Content != nil {
			total += len(*m.Content)
		}
	}
	// 工具定义的 JSON schema 也计入
	for _, t := range tools {
		if t.Description != "" {
			total += len(t.Description)
		}
	}
	return total * 2 / 5, nil // 字符数 × 0.4 ≈ token 数
}

func (p *OpenAIProvider) buildMessages(messages []queryengine.Message) []openai.ChatCompletionMessage {
	result := make([]openai.ChatCompletionMessage, 0, len(messages))

	for _, msg := range messages {
		var role string
		switch msg.Role {
		case queryengine.MessageRoleSystem:
			role = openai.ChatMessageRoleSystem
		case queryengine.MessageRoleUser:
			role = openai.ChatMessageRoleUser
		case queryengine.MessageRoleAssistant:
			role = openai.ChatMessageRoleAssistant
		case queryengine.MessageRoleTool:
			role = openai.ChatMessageRoleTool
		default:
			continue
		}

		content := ""
		if msg.Content != nil {
			content = *msg.Content
		}

		if msg.Role == queryengine.MessageRoleTool && msg.ToolCallID != nil {
			result = append(result, openai.ChatCompletionMessage{
				Role:       role,
				Content:    content,
				ToolCallID: *msg.ToolCallID,
			})
		} else if msg.Role == queryengine.MessageRoleAssistant && msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			toolCalls := make([]openai.ToolCall, 0, len(*msg.ToolCalls))
			for _, tc := range *msg.ToolCalls {
				var argsJSON string
				if tc.Input != nil && len(tc.Input) > 0 {
					argsBytes, _ := json.Marshal(tc.Input)
					argsJSON = string(argsBytes)
				} else {
					argsJSON = "{}"
				}
				toolCalls = append(toolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: argsJSON,
					},
				})
			}
			result = append(result, openai.ChatCompletionMessage{
				Role:      role,
				Content:   content,
				ToolCalls: toolCalls,
			})
		} else {
			message := openai.ChatCompletionMessage{Role: role, Content: content}
			if msg.Role == queryengine.MessageRoleUser && len(msg.Images) > 0 {
				parts := []openai.ChatMessagePart{{Type: openai.ChatMessagePartTypeText, Text: content}}
				for _, image := range msg.Images {
					detail := openai.ImageURLDetailAuto
					switch image.Detail {
					case "high":
						detail = openai.ImageURLDetailHigh
					case "low":
						detail = openai.ImageURLDetailLow
					}
					parts = append(parts, openai.ChatMessagePart{
						Type:     openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{URL: image.URL, Detail: detail},
					})
				}
				message.Content = ""
				message.MultiContent = parts
			}
			result = append(result, message)
		}
	}

	return result
}

func (p *OpenAIProvider) toOpenAITools(tools []queryengine.ToolSchema) []openai.Tool {
	result := make([]openai.Tool, 0, len(tools))
	for _, t := range tools {
		result = append(result, openai.Tool{
			Type: "function",
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return result
}

func (p *OpenAIProvider) mapStopReason(reason string) queryengine.StopReason {
	switch reason {
	case "tool_calls":
		return queryengine.StopReasonToolUse
	case "length":
		return queryengine.StopReasonMaxTokens
	default:
		return queryengine.StopReasonEndTurn
	}
}

func isOpenAIReasoningModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-5") ||
		strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") ||
		strings.HasPrefix(model, "o4")
}

func applyOpenAITokenLimit(request *openai.ChatCompletionRequest, model string, limit int) {
	if limit <= 0 {
		return
	}
	if isOpenAIReasoningModel(model) {
		request.MaxCompletionTokens = limit
		return
	}
	request.MaxTokens = limit
}

func toOpenAIResponseFormat(format *queryengine.ResponseFormat) *openai.ChatCompletionResponseFormat {
	if format == nil {
		return nil
	}
	switch format.Type {
	case "json_schema":
		return &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name: format.Name, Schema: openAIJSONSchema(format.Schema), Strict: format.Strict,
			},
		}
	case "json_object":
		return &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject}
	default:
		return nil
	}
}

func (p *OpenAIProvider) Validate(ctx context.Context) error {
	req := openai.ChatCompletionRequest{
		Model:    p.model,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "ping"}},
	}
	applyOpenAITokenLimit(&req, p.model, 1)
	_, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return fmt.Errorf("openai validation failed: %w", err)
	}
	return nil
}
