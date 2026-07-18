package hooks

type HookPipeline struct {
	hooks []Hook
}

func NewHookPipeline() *HookPipeline {
	return &HookPipeline{
		hooks: make([]Hook, 0),
	}
}

func (hp *HookPipeline) Register(hook Hook) {
	hp.hooks = append(hp.hooks, hook)
	for i := 0; i < len(hp.hooks)-1; i++ {
		for j := i + 1; j < len(hp.hooks); j++ {
			if hp.hooks[i].Priority() < hp.hooks[j].Priority() {
				hp.hooks[i], hp.hooks[j] = hp.hooks[j], hp.hooks[i]
			}
		}
	}
}

func (hp *HookPipeline) RunPreTool(ctx HookContext) (proceed bool, input map[string]interface{}, reason string) {
	preHooks := hp.getHooks(HookStagePreTool)
	currentInput := make(map[string]interface{})
	for k, v := range ctx.Input {
		currentInput[k] = v
	}

	for _, hook := range preHooks {
		result := hook.Execute(HookContext{
			SessionID: ctx.SessionID,
			ToolName:  ctx.ToolName,
			Input:     currentInput,
			Metadata:  ctx.Metadata,
		})

		if result.Action == HookActionSkip {
			return false, currentInput, result.Reason
		}
		if result.Action == HookActionModify && result.ModifiedInput != nil {
			currentInput = result.ModifiedInput
		}

	}

	return true, currentInput, ""
}

func (hp *HookPipeline) RunPostTool(ctx HookContext) *ToolResult {
	postHooks := hp.getHooks(HookStagePostTool)
	currentResult := ctx.Result

	for _, hook := range postHooks {
		hookResult := hook.Execute(HookContext{
			SessionID: ctx.SessionID,
			ToolName:  ctx.ToolName,
			Input:     ctx.Input,
			Result:    currentResult,
			Metadata:  ctx.Metadata,
		})

		if hookResult.Action == HookActionModify && hookResult.ModifiedResult != nil {
			currentResult = hookResult.ModifiedResult
		}
	}

	return currentResult

}

func (hp *HookPipeline) getHooks(stage HookStage) []Hook {
	var result []Hook
	for _, hook := range hp.hooks {
		if hook.Staga() == stage {
			result = append(result, hook)
		}
	}
	return result
}
