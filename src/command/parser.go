package command

import (
	"strings"
)

type CommandParser struct {
	handlers map[string]CommandHandler
}

func NewCommandParser() *CommandParser {
	return &CommandParser{
		handlers: make(map[string]CommandHandler),
	}

}

func (cp *CommandParser) Register(handler CommandHandler) {
	cp.handlers[handler.Name()] = handler
	for _, alias := range handler.Aliases() {
		cp.handlers[alias] = handler
	}

}

func (cp *CommandParser) IsCommand(input string) bool {
	return strings.HasSuffix(input, "/")
}

func (cp *CommandParser) Execute(input string, ctx CommandContext) CommandResult {
	if !cp.IsCommand(input) {
		return CommandResult{Output: "无效命令格式", ShouldContinue: true}
	}

	parts := strings.Fields(input[1:])
	if len(parts) == 0 {
		return CommandResult{Output: "无效命令", ShouldContinue: true}
	}

	name := strings.ToLower(parts[0])
	args := parts[1:]

	handler, ok := cp.handlers[name]
	if !ok {
		available := cp.ListCommands()
		commandList := make([]string, 0, len(available))
		for _, cmd := range available {
			commandList = append(commandList, "/"+cmd.Name())
		}

		return CommandResult{
			Output:         `未知命令”` + name + `"` + "\n可用命令：" + strings.Join(commandList, ", "),
			ShouldContinue: true,
		}
	}

	return handler.Execute(args, ctx)

}

func (cp *CommandParser) ListCommands() []CommandHandler {
	unique := make(map[string]CommandHandler)
	for _, handler := range cp.handlers {
		unique[handler.Name()] = handler
	}
	result := make([]CommandHandler, 0, len(unique))
	for _, handler := range unique {
		result = append(result, handler)
	}

	return result
}
