package queryengine

import "context"

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
	result := qe.router.Resolve(*params.Model)

	return WithRetryResult(func() (ParsedResponse, error) {
		collector := NewStreamCollector()

		stream := result.Provider.Stream(StreamParams{
			Model:        result.Model,
			Messages:     params.Messages,
			Tools:        *params.Tools,
			MaxTokens:    *params.MaxTokens,
			Temperature:  *params.Temperature,
			SystemPrompt: *params.SystemPrompt,
		})

		for event := range stream {
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

		return collector.Result(), nil
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
