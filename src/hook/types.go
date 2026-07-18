package hooks

type HookStage string

const (
	HookStagePreTool  HookStage = "pre_tool"
	HookStagePostTool HookStage = "post_tool"
)

type HookAction string

const (
	HookActionContinue HookAction = "continue"
	HookActionSkip     HookAction = "skip"
	HookActionModify   HookAction = "modify"
)

type ToolResult struct {
	Output   string                 `json:"output"`
	Success  bool                   `json:"success"`
	IsError  bool                   `json:"isError"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type HookContext struct {
	SessionID string                 `json:"sessionId"`
	ToolName  string                 `json:"toolName"`
	Input     map[string]interface{} `json:"input"`
	Result    *ToolResult            `json:"result,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type HookResult struct {
	Action         HookAction             `json:"action"`
	ModifiedInput  map[string]interface{} `json:"modifiedInput,omitempty"`
	ModifiedResult *ToolResult            `json:"modifiedResult,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
}

type Hook interface {
	Name() string
	Staga() HookStage
	Priority() int
	Execute(ctx HookContext) HookResult
}
