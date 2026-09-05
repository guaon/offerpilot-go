package queryengine

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
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

// 找对的AI、处理流式回复、自动重试，最后给出完整的答案
func (qe *QueryEngine) Query(params QueryParams) (ParsedResponse, error) {
	log := logger.DefaultLogger

	model := ""
	if params.Model != nil {
		model = *params.Model
	}
	result, err := qe.router.Resolve(model)
	if err != nil {
		return ParsedResponse{}, NewQueryEngineError(err.Error(), ErrorCategoryUnavailable, false, 0)
	}

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
		"model":        result.Model,
		"provider":     result.Provider.Name(),
		"messageCount": len(params.Messages),
		"toolCount":    len(tools),
		"maxTokens":    maxTokens,
		"component":    "QueryEngine",
	})

	startTime := time.Now()

	// 内联重试循环，支持 OnRetry 回调通知上层
	maxRetries := defaultMaxRetries
	baseDelay := defaultBaseDelay
	maxDelay := defaultMaxDelay
	if qe.retryOpts != nil {
		if qe.retryOpts.MaxRetries > 0 {
			maxRetries = qe.retryOpts.MaxRetries
		}
		if qe.retryOpts.BaseDelay > 0 {
			baseDelay = qe.retryOpts.BaseDelay
		}
		if qe.retryOpts.MaxDelay > 0 {
			maxDelay = qe.retryOpts.MaxDelay
		}
	}

	var lastQueryErr *QueryEngineError
	for attempt := 0; attempt <= maxRetries; attempt++ {
		collector := NewStreamCollector()

		stream := result.Provider.Stream(StreamParams{
			Model:          result.Model,
			Messages:       params.Messages,
			Tools:          tools,
			MaxTokens:      maxTokens,
			Temperature:    temperature,
			SystemPrompt:   systemPrompt,
			ResponseFormat: params.ResponseFormat,
			AbortSignal:    params.Context,
		})

		for event := range stream {
			if errorEvent, ok := event.(*ErrorEvent); ok {
				if errorEvent.Err != nil {
					log.Error("QueryEngine stream error", map[string]interface{}{
						"error":     errorEvent.Err.Error(),
						"component": "QueryEngine",
					})
					lastQueryErr = ClassifyError(errorEvent.Err)
					break
				}
				lastQueryErr = ClassifyError(errors.New("model stream returned an unknown error"))
				break
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

		// 流错误重试
		if lastQueryErr != nil {
			if !lastQueryErr.Retryable || attempt == maxRetries {
				return ParsedResponse{}, lastQueryErr
			}
			if params.OnRetry != nil {
				params.OnRetry(attempt+1, maxRetries, lastQueryErr.Message)
			}
			delay := retryDelay(attempt, baseDelay, maxDelay, lastQueryErr.RetryAfterMs)
			time.Sleep(delay)
			lastQueryErr = nil
			continue
		}

		resp := collector.Result()
		elapsed := time.Since(startTime)

		// 空流检测：没有任何内容也没有工具调用 → 触发重试
		if resp.Type == "text" && resp.Content == nil && len(collector.ToolCalls) == 0 {
			log.Warn("QueryEngine received empty stream", map[string]interface{}{
				"duration":  elapsed.String(),
				"component": "QueryEngine",
			})
			emptyErr := &QueryEngineError{Message: "model returned empty response", Category: ErrorCategoryUnknown, Retryable: true}
			if attempt == maxRetries {
				return ParsedResponse{}, emptyErr
			}
			if params.OnRetry != nil {
				params.OnRetry(attempt+1, maxRetries, emptyErr.Message)
			}
			delay := retryDelay(attempt, baseDelay, maxDelay, 2000)
			time.Sleep(delay)
			continue
		}

		log.Info("QueryEngine request completed", map[string]interface{}{
			"responseType": resp.Type,
			"duration":     elapsed.String(),
			"inputTokens":  resp.Usage.InputTokens,
			"outputTokens": resp.Usage.OutputTokens,
			"component":    "QueryEngine",
		})

		if resp.Type == "tool_use" && resp.ToolCalls != nil {
			toolNames := make([]string, len(*resp.ToolCalls))
			for i, tc := range *resp.ToolCalls {
				toolNames[i] = tc.Name
			}
			log.Info("QueryEngine returning tool calls", map[string]interface{}{
				"toolCount": len(*resp.ToolCalls),
				"toolNames": strings.Join(toolNames, ", "),
				"component": "QueryEngine",
			})
		} else if resp.Type == "text" && resp.Content != nil {
			content := *resp.Content
			preview := content
			if len(content) > 200 {
				preview = content[:200] + "..."
			}
			log.Info("QueryEngine returning text response", map[string]interface{}{
				"contentLength":  len(content),
				"contentPreview": preview,
				"component":      "QueryEngine",
			})
		}

		return resp, nil
	}

	return ParsedResponse{}, lastQueryErr
}

func (qe *QueryEngine) StreamRaw(ctx context.Context, params QueryParams) <-chan StreamEvent {
	output := make(chan StreamEvent, 1)
	model := ""
	if params.Model != nil {
		model = *params.Model
	}
	result, err := qe.router.Resolve(model)
	if err != nil {
		output <- &ErrorEvent{Err: err}
		close(output)
		return output
	}

	stream := result.Provider.Stream(StreamParams{
		Model:          result.Model,
		Messages:       params.Messages,
		Tools:          *params.Tools,
		MaxTokens:      *params.MaxTokens,
		Temperature:    *params.Temperature,
		SystemPrompt:   *params.SystemPrompt,
		ResponseFormat: params.ResponseFormat,
		AbortSignal:    ctx,
	})

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
	result, err := qe.router.Resolve(params.Model)
	if err != nil {
		return 0, err
	}
	return result.Provider.CountTokens(params.Messages, params.Tools, result.Model)
}

// retryDelay 计算重试等待时间
func retryDelay(attempt int, baseDelay, maxDelay time.Duration, retryAfterMs int) time.Duration {
	if retryAfterMs > 0 {
		return time.Duration(retryAfterMs) * time.Millisecond
	}
	jitter := rand.Float64()*0.3 + 0.85
	delay := time.Duration(float64(baseDelay) * jitter * math.Pow(2, float64(attempt)))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

func (qe *QueryEngine) ListProviders() []string {
	return qe.router.ListProviders()
}

func (qe *QueryEngine) Available() bool {
	return len(qe.router.Configs) > 0
}
