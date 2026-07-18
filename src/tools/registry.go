package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type ToolRegistry struct {
	tools map[string]*ToolDefinition
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]*ToolDefinition)}
}

func (r *ToolRegistry) Registry(def *ToolDefinition) {
	r.tools[def.Schema.Name] = def
}

func (r *ToolRegistry) Get(name string) *ToolDefinition {
	return r.tools[name]
}

func (r *ToolRegistry) Execute(name string, input map[string]interface{}, ctx ToolContext) ToolResult {
	if def := r.tools[name]; def != nil {
		return def.Execute(input, ctx)
	}

	return ToolResult{Success: false, Output: "Tool not found: " + name}
}

func (r *ToolRegistry) ExecuteJSON(ctx context.Context, name string, inputJSON string, tc ToolContext) (*ToolResult, error) {
	def := r.tools[name]
	if def == nil {
		return nil, fmt.Errorf("tool not found:%s", name)
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
		return nil, err
	}

	result := def.Execute(input, tc)
	return &result, nil
}

func (r *ToolRegistry) GetAllToolInfo() []*schema.ToolInfo {
	return r.ListSchemas()
}

func (r *ToolRegistry) ListSchemas() []*schema.ToolInfo {
	result := make([]*schema.ToolInfo, 0, len(r.tools))
	for _, def := range r.tools {
		result = append(result, def.Schema)
	}
	return result
}

func (r *ToolRegistry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

func (r *ToolRegistry) ToEinoTools() []tool.BaseTool {
	result := make([]tool.BaseTool, 0, len(r.tools))
	for _, def := range r.tools {
		result = append(result, &EinoToolAdapter{def: def})
	}
	return result
}
