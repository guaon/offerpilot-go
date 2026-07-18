package command

import "strings"

type HelpCommand struct{}

func (h *HelpCommand) Name() string        { return "help" }
func (h *HelpCommand) Aliases() []string   { return []string{"h", "?"} }
func (h *HelpCommand) Description() string { return "显示帮助信息" }
func (h *HelpCommand) Execute(args []string, ctx CommandContext) CommandResult {
	return CommandResult{
		Output: `可用命令：
  /help       显示帮助
  /status     查看当前会话状态
  /dimensions 列出所有考察维度
  /score      查看当前得分
  /report     生成会话报告
  /reset      重新开始会话
  /model      切换模型
  /quit       退出`,
		ShouldContinue: true,
	}
}

type StatusCommand struct{}

func (s *StatusCommand) Name() string        { return "status" }
func (s *StatusCommand) Aliases() []string   { return []string{"s"} }
func (s *StatusCommand) Description() string { return "查看当前会话状态" }
func (s *StatusCommand) Execute(args []string, ctx CommandContext) CommandResult {
	return CommandResult{
		Output:         "会话ID: " + ctx.SessionID,
		ShouldContinue: true,
		Metadata:       map[string]interface{}{"command": "status"},
	}
}

type DimensionsCommand struct{}

func (d *DimensionsCommand) Name() string        { return "dimension" }
func (d *DimensionsCommand) Aliases() []string   { return []string{"dim", "d"} }
func (d *DimensionsCommand) Description() string { return "列出所有考察维度" }
func (d *DimensionsCommand) Execute(args []string, ctx CommandContext) CommandResult {
	dims := []string{
		"1. 架构设计 (architecture)",
		"2. Harness 工程 (engineering)",
		"3. 模型能力 (model)",
		"4. RAG 知识增强 (rag)",
		"5. 多 Agent (multi-agent)",
		"6. 评测 (evaluation)",
		"7. 全栈工程 (full-stack)",
	}

	return CommandResult{
		Output:         "考察维度：\n" + strings.Join(dims, "\n"),
		ShouldContinue: true,
	}

}

type QuitCommand struct{}

func (q *QuitCommand) Name() string        { return "quit" }
func (q *QuitCommand) Aliases() []string   { return []string{"exit", "q"} }
func (q *QuitCommand) Description() string { return "退出会话" }
func (q *QuitCommand) Execute(args []string, ctx CommandContext) CommandResult {
	return CommandResult{
		Output:         "会话结束",
		ShouldContinue: true,
	}
}

type ResetCommand struct{}

func (r *ResetCommand) Name() string        { return "reset" }
func (r *ResetCommand) Aliases() []string   { return []string{"restart"} }
func (r *ResetCommand) Description() string { return "重新开始对话" }
func (r *ResetCommand) Execute(args []string, ctx CommandContext) CommandResult {
	return CommandResult{
		Output:         "会话已重置",
		ShouldContinue: true,
		Metadata:       map[string]interface{}{"action": "reset"},
	}
}
