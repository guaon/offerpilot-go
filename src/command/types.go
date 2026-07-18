package command

type CommandResult struct {
	Output         string                 `json:"output"`
	ShouldContinue bool                   `json:"shouldContinue"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

type CommandContext struct {
	SessionID string      `json:"sessionId"`
	App       interface{} `json:"app,omitempty"`
}

type CommandHandler interface {
	Name() string
	Aliases() []string //返回命令的别名
	Description() string
	Execute(args []string, ctx CommandContext) CommandResult
}
