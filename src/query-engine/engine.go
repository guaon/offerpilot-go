package queryengine

import (
	"context"
	"errors"
	"strings"
	"time"

	"MyOfferPilot/src/logger"
)

type QueryEngineOptions struct {
	Providers       []ProviderConfig
	DefaultProvider string
	Retry           *RetryOptions
}

type QueryEngine struct {
	router    *ProviderRouter
	retryOpts *RetryOptions
}

func NewQueryEngine(opts QueryEngineOptions) *QueryEngine {
	router := NewProviderRouter()
	for _, config := range opts.Providers {
		router.Register(config)
	}
	return &QueryEngine{
		router:    router,
		retryOpts: opts.Retry,
	}

}

//找对的AI、处理流式回复、自动重试，最后给出完整的答案
func (qe *QueryEngine) Query(params QueryParams) (ParsedResponse, error) {
	log := logger.DefaultLogger

	model := ""
	if params.Model != nil {
		model = *params.Model
	}
	result := qe.router.Resolve(model)

	tools := []ToolSchema{}
	if params.Tools != nil {
		tools = *params.Tools
	}

	maxTokens := 0
	if params.MaxTokens != nil {
		maxTokens = *params.MaxTokens
	}

	temperature := float64(0)
	if params.Temperature != nil {
		temperature = *params.Temperature
	}

	systemPrompt := ""
	if params.SystemPrompt != nil {
		systemPrompt = *params.SystemPrompt
	}

	log.Info("QueryEngine processing request", map[string]interface{}{
		"model":         result.Model,
		"provider":      result.Provider.Name(),
		"messageCount": len(params.Messages),
		"toolCount":    len(tools),
		"maxTokens":    maxTokens,
		"component":    "QueryEngine",
	})

	startTime := time.Now()

	return WithRetryResult(func() (ParsedResponse, error) {
		collector := NewStreamCollector()

		stream := result.Provider.Stream(StreamParams{
			Model:        result.Model,
			Messages:     params.Messages,
			Tools:        tools,
			MaxTokens:    maxTokens,
			Temperature:  temperature,
			SystemPrompt: systemPrompt,
		})

		for event := range stream {
			if errorEvent, ok := event.(*ErrorEvent); ok {
				if errorEvent.Err != nil {
					log.Error("QueryEngine stream error", map[string]interface{}{
						"error":     errorEvent.Err.Error(),
						"component": "QueryEngine",
					})
					return ParsedResponse{}, errorEvent.Err
				}
				return ParsedResponse{}, errors.New("model stream returned an unknown error")
			}
			collector.Feed(event)
			if event.GetType() == "text_delta" && params.OnTextDelta != nil {
				if textDelta, ok := event.(*TextDeltaEvent); ok {
					params.OnTextDelta(textDelta.Content)
				}
			} else if event.GetType() == "thinking_delta" && params.OnThinkingDelta != nil {
				if thinkingDelta, ok := event.(*ThinkingDeltaEvent); ok {
					params.OnThinkingDelta(thinkingDelta.Content)
				}
			}

		}

		result := collector.Result()
		elapsed := time.Since(startTime)

		log.Info("QueryEngine request completed", map[string]interface{}{
			"responseType": result.Type,
			"duration":     elapsed.String(),
			"inputTokens":  result.Usage.InputTokens,
			"outputTokens": result.Usage.OutputTokens,
			"component":    "QueryEngine",
		})

		if result.Type == "tool_use" && result.ToolCalls != nil {
			toolNames := make([]string, len(*result.ToolCalls))
			for i, tc := range *result.ToolCalls {
				toolNames[i] = tc.Name
			}
			log.Info("QueryEngine returning tool calls", map[string]interface{}{
				"toolCount": len(*result.ToolCalls),
				"toolNames": strings.Join(toolNames, ", "),
				"component": "QueryEngine",
			})
		} else if result.Type == "text" && result.Content != nil {
			content := *result.Content
			preview := content
			if len(content) > 200 {
				preview = content[:200] + "..."
			}
			log.Info("QueryEngine returning text response", map[string]interface{}{
				"contentLength": len(content),
				"contentPreview": preview,
				"component":    "QueryEngine",
			})
		}

		return result, nil
	}, qe.retryOpts)
}

func (qe *QueryEngine) StreamRaw(ctx context.Context, params QueryParams) <-chan StreamEvent {
	result := qe.router.Resolve(*params.Model)

	stream := result.Provider.Stream(StreamParams{
		Model:        result.Model,
		Messages:     params.Messages,
		Tools:        *params.Tools,
		MaxTokens:    *params.MaxTokens,
		Temperature:  *params.Temperature,
		SystemPrompt: *params.SystemPrompt,
		AbortSignal:  ctx,
	})

	output := make(chan StreamEvent)

	go func() {
		defer close(output)

		for event := range stream {
			select {
			case <-ctx.Done():
				return
			case output <- event:

			}
		}
	}()

	return output
}

func (qe *QueryEngine) CountTokens(params struct {
	Model    string
	Messages []Message
	Tools    []ToolSchema
}) (int, error) {
	result := qe.router.Resolve(params.Model)
	return result.Provider.CountTokens(params.Messages, params.Tools, result.Model)
}

func (qe *QueryEngine) ListProviders() []string {
	return qe.router.ListProviders()
}
