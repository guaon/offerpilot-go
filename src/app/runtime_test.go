package app

import (
	"context"
	"sync"
	"testing"
	"time"

	hooks "MyOfferPilot/src/hook"
	"MyOfferPilot/src/memory"
	"MyOfferPilot/src/permission"
	queryengine "MyOfferPilot/src/query-engine"
	"MyOfferPilot/src/query-engine/provider"
	"MyOfferPilot/src/session"
	tool "MyOfferPilot/src/tools"
)

type blockingValidationProvider struct{}

func (blockingValidationProvider) Name() string { return "blocking" }
func (blockingValidationProvider) Stream(queryengine.StreamParams) <-chan queryengine.StreamEvent {
	return make(chan queryengine.StreamEvent)
}
func (blockingValidationProvider) CountTokens([]queryengine.Message, []queryengine.ToolSchema, string) (int, error) {
	return 0, nil
}
func (blockingValidationProvider) Validate(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func newTestRuntime(t *testing.T) *App {
	t.Helper()
	manager, _ := session.NewSessionManager(nil)
	store, _ := memory.NewMemoryStore(nil)
	registry := tool.NewToolRegistry()
	gate := permission.NewPermissionGate()
	gate.RegisterRule(permission.PermissionRule{ToolName: "limited", RateLimitPerMinute: 1})
	engine := queryengine.NewQueryEngine(queryengine.QueryEngineOptions{Providers: []queryengine.ProviderConfig{{
		Provider: provider.NewMockProvider(), Models: []string{"mock"}, DefaultModel: "mock",
	}}})
	pipeline := hooks.NewHookPipeline()
	return &App{
		SessionManager: manager,
		MemoryStore:    store,
		ToolRegistry:   registry,
		PermissionGate: gate,
		QueryEngine:    engine,
		HookPipeline:   pipeline,
	}
}

func TestNewRequestSharesRuntimeAndIsolatesAgents(t *testing.T) {
	runtime := newTestRuntime(t)
	requestA := runtime.NewRequest(&AppOptions{Model: "mock"})
	requestB := runtime.NewRequest(&AppOptions{Model: "mock"})

	if requestA.Agent == requestB.Agent {
		t.Fatal("requests must not share an AgentLoop")
	}
	if requestA.QueryEngine != requestB.QueryEngine || requestA.ToolRegistry != requestB.ToolRegistry ||
		requestA.PermissionGate != requestB.PermissionGate || requestA.SessionManager != requestB.SessionManager {
		t.Fatal("requests should reuse the server runtime")
	}

	first := requestA.PermissionGate.Check("limited", permission.RiskLevelLow, "session")
	second := requestB.PermissionGate.Check("limited", permission.RiskLevelLow, "session")
	if !first.Allowed || second.Allowed {
		t.Fatal("tool rate limits must persist across HTTP request agents")
	}
}

func TestConcurrentRequestCallbacksStayIsolated(t *testing.T) {
	runtime := newTestRuntime(t)
	var mu sync.Mutex
	var outputA, outputB string
	requestA := runtime.NewRequest(&AppOptions{Model: "mock", OnTextDelta: func(text string) {
		mu.Lock()
		outputA += text
		mu.Unlock()
	}})
	requestB := runtime.NewRequest(&AppOptions{Model: "mock", OnTextDelta: func(text string) {
		mu.Lock()
		outputB += text
		mu.Unlock()
	}})
	sessionA := runtime.SessionManager.Create("user-a")
	sessionB := runtime.SessionManager.Create("user-b")

	var wg sync.WaitGroup
	errors := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := requestA.Agent.Run(context.Background(), sessionA.ID, "user-a", "hello a")
		errors <- err
	}()
	go func() {
		defer wg.Done()
		_, err := requestB.Agent.Run(context.Background(), sessionB.ID, "user-b", "hello b")
		errors <- err
	}()
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("request agent failed: %v", err)
		}
	}
	if outputA == "" || outputB == "" {
		t.Fatalf("expected both callbacks to receive output: a=%q b=%q", outputA, outputB)
	}
}

func TestAgentUsageResetsForEveryRun(t *testing.T) {
	runtime := newTestRuntime(t)
	request := runtime.NewRequest(&AppOptions{Model: "mock"})
	sess := runtime.SessionManager.Create("user")

	if _, err := request.Agent.Run(context.Background(), sess.ID, "user", "first"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := request.Agent.GetUsage()
	if _, err := request.Agent.Run(context.Background(), sess.ID, "user", "second"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second := request.Agent.GetUsage()
	if first.InputTokens == 0 || second.InputTokens != first.InputTokens {
		t.Fatalf("usage accumulated across runs: first=%#v second=%#v", first, second)
	}
}

func TestProviderValidationHonorsTimeout(t *testing.T) {
	start := time.Now()
	err := validateProviderWithTimeout(blockingValidationProvider{}, 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected validation timeout")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("validation timeout was not enforced: %s", elapsed)
	}
}
