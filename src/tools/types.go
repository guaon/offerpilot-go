package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelCritical RiskLevel = "critical"
)

type ToolContext struct {
	SessionId   string
	UserID      string
	AgentConfig interface{}
}

type toolContextKey struct{}

func NewContextWithToolContext(parent context.Context, tc ToolContext) context.Context {
	return context.WithValue(parent, toolContextKey{}, tc)
}

func ToolContextFromContext(ctx context.Context) ToolContext {
	if tc, ok := ctx.Value(toolContextKey{}).(ToolContext); ok {
		return tc
	}
	return ToolContext{}
}

type ToolResult struct {
	Success  bool
	Output   string
	IsError  bool
	Metadata map[string]interface{}
}

type ToolDefinition struct {
	Schema    *schema.ToolInfo
	RiskLevel RiskLevel
	Execute   func(input map[string]interface{}, ctx ToolContext) ToolResult
}

type EinoToolAdapter struct {
	def *ToolDefinition
}

func (t *EinoToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.def.Schema, nil
}

func (t *EinoToolAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", err
	}

	tc := ToolContextFromContext(ctx)

	result := t.def.Execute(input, tc)
	if !result.Success || result.IsError {
		return result.Output, fmt.Errorf(result.Output)
	}
	return result.Output, nil
}
