package queryengine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

type retryOnceProvider struct {
	mu       sync.Mutex
	attempts map[string]int
}

type responseFormatProvider struct {
	seen *ResponseFormat
}

func (p *responseFormatProvider) Name() string { return "response-format" }
func (p *responseFormatProvider) Stream(params StreamParams) <-chan StreamEvent {
	p.seen = params.ResponseFormat
	events := make(chan StreamEvent, 2)
	events <- &TextDeltaEvent{Content: "{}"}
	events <- &MessageEndEvent{StopReason: StopReasonEndTurn}
	close(events)
	return events
}
func (p *responseFormatProvider) CountTokens([]Message, []ToolSchema, string) (int, error) {
	return 0, nil
}
func (p *responseFormatProvider) Validate(context.Context) error { return nil }

func (p *retryOnceProvider) Name() string { return "retry-once" }

func (p *retryOnceProvider) Stream(params StreamParams) <-chan StreamEvent {
	events := make(chan StreamEvent, 3)
	key := params.Model
	if len(params.Messages) > 0 && params.Messages[len(params.Messages)-1].Content != nil {
		key += *params.Messages[len(params.Messages)-1].Content
	}
	p.mu.Lock()
	p.attempts[key]++
	attempt := p.attempts[key]
	p.mu.Unlock()
	if attempt == 1 {
		events <- &ErrorEvent{Err: NewQueryEngineError("retry", ErrorCategoryNetwork, true, 1)}
	} else {
		events <- &TextDeltaEvent{Content: "ok"}
		events <- &MessageEndEvent{StopReason: StopReasonEndTurn}
	}
	close(events)
	return events
}

func (p *retryOnceProvider) CountTokens([]Message, []ToolSchema, string) (int, error) {
	return 0, nil
}

func (p *retryOnceProvider) Validate(context.Context) error { return nil }

func TestQueryReturnsUnavailableWithoutProvider(t *testing.T) {
	engine := NewQueryEngine(QueryEngineOptions{})
	_, err := engine.Query(QueryParams{})
	queryErr, ok := err.(*QueryEngineError)
	if !ok || queryErr.Category != ErrorCategoryUnavailable {
		t.Fatalf("expected unavailable error, got %#v", err)
	}
	if engine.Available() {
		t.Fatal("empty engine should not report itself available")
	}
}

func TestRetryCallbacksAreRequestScoped(t *testing.T) {
	provider := &retryOnceProvider{attempts: make(map[string]int)}
	engine := NewQueryEngine(QueryEngineOptions{Providers: []ProviderConfig{{
		Provider: provider, Models: []string{"test"}, DefaultModel: "test",
	}}})

	var callbackA atomic.Int32
	var callbackB atomic.Int32
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	run := func(message string, callback *atomic.Int32) {
		defer wg.Done()
		model := "test"
		_, err := engine.Query(QueryParams{
			Model:    &model,
			Messages: []Message{*NewMessageWithContent(MessageRoleUser, message)},
			OnRetry: func(int, int, string) {
				callback.Add(1)
			},
		})
		errors <- err
	}

	wg.Add(2)
	go run("a", &callbackA)
	go run("b", &callbackB)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("query failed: %v", err)
		}
	}
	if callbackA.Load() != 1 || callbackB.Load() != 1 {
		t.Fatalf("callbacks crossed requests: a=%d b=%d", callbackA.Load(), callbackB.Load())
	}
}

func TestStreamRawReportsMissingProvider(t *testing.T) {
	engine := NewQueryEngine(QueryEngineOptions{})
	event, ok := <-engine.StreamRaw(context.Background(), QueryParams{})
	if !ok {
		t.Fatal("expected an error event")
	}
	if _, ok := event.(*ErrorEvent); !ok {
		t.Fatalf("expected ErrorEvent, got %T", event)
	}
}

func TestQueryPassesStructuredResponseFormatToProvider(t *testing.T) {
	provider := &responseFormatProvider{}
	engine := NewQueryEngine(QueryEngineOptions{Providers: []ProviderConfig{{
		Provider: provider, Models: []string{"test"}, DefaultModel: "test",
	}}})
	format := &ResponseFormat{Type: "json_schema", Name: "resume_diagnosis", Strict: true, Schema: map[string]interface{}{"type": "object"}}
	if _, err := engine.Query(QueryParams{ResponseFormat: format}); err != nil {
		t.Fatal(err)
	}
	if provider.seen != format {
		t.Fatalf("response format = %#v", provider.seen)
	}
}
