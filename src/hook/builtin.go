package hooks

import (
	"math"
	"strings"
)

type InputSanitizerHook struct{}

func (h *InputSanitizerHook) Name() string     { return "input-sanitizer" }
func (h *InputSanitizerHook) Stage() HookStage { return HookStagePreTool }
func (h *InputSanitizerHook) Priority() int    { return 10 }
func (h *InputSanitizerHook) Execute(ctx HookContext) HookResult {
	sanitized := make(map[string]interface{})
	for key, value := range ctx.Input {
		if s, ok := value.(string); ok {
			sanitized[key] = strings.TrimSpace(s)[:min(len(strings.TrimSpace(s)), 10000)]
		} else {
			sanitized[key] = value
		}
	}
	return HookResult{Action: HookActionModify, ModifiedInput: sanitized}

}

type TokenCounterHook struct{}

func (h *TokenCounterHook) Name() string     { return "token_counter" }
func (h *TokenCounterHook) Stage() HookStage { return HookStagePostTool }
func (h *TokenCounterHook) Priority() int    { return 100 }
func (h *TokenCounterHook) Execute(ctx HookContext) HookResult {
	if ctx.Result == nil {
		return HookResult{Action: HookActionContinue}
	}

	outputLength := len(ctx.Result.Output)
	estimateTokens := int(math.Ceil(float64(outputLength) / 3.5))

	metadata := make(map[string]interface{})
	for k, v := range ctx.Result.Metadata {
		metadata[k] = v
	}

	metadata["estimateOutputTokens"] = estimateTokens

	return HookResult{
		Action: HookActionModify,
		ModifiedResult: &ToolResult{
			Output:   ctx.Result.Output,
			Success:  ctx.Result.Success,
			IsError:  ctx.Result.IsError,
			Metadata: metadata,
		},
	}
}
