package provider

import (
	"encoding/json"
	"testing"

	queryengine "MyOfferPilot/src/query-engine"
	openai "github.com/sashabaranov/go-openai"
)

func TestApplyOpenAITokenLimitUsesCompletionTokensForReasoningModels(t *testing.T) {
	models := []string{"gpt-5.5", "gpt-5", "o1-mini", "o3", "o4-mini"}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			request := openai.ChatCompletionRequest{}
			applyOpenAITokenLimit(&request, model, 4096)
			if request.MaxCompletionTokens != 4096 || request.MaxTokens != 0 {
				t.Fatalf("token limits = max_tokens:%d max_completion_tokens:%d", request.MaxTokens, request.MaxCompletionTokens)
			}
		})
	}
}

func TestApplyOpenAITokenLimitPreservesLegacyCompatibleModels(t *testing.T) {
	models := []string{"gpt-4o", "gpt-4-turbo", "deepseek-chat"}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			request := openai.ChatCompletionRequest{}
			applyOpenAITokenLimit(&request, model, 4096)
			if request.MaxTokens != 4096 || request.MaxCompletionTokens != 0 {
				t.Fatalf("token limits = max_tokens:%d max_completion_tokens:%d", request.MaxTokens, request.MaxCompletionTokens)
			}
		})
	}
}

func TestOpenAIResponseFormatUsesStrictJSONSchema(t *testing.T) {
	format := toOpenAIResponseFormat(&queryengine.ResponseFormat{
		Type: "json_schema", Name: "resume_diagnosis", Strict: true,
		Schema: map[string]interface{}{"type": "object"},
	})
	if format == nil || format.Type != openai.ChatCompletionResponseFormatTypeJSONSchema || format.JSONSchema == nil {
		t.Fatalf("response format = %#v", format)
	}
	if format.JSONSchema.Name != "resume_diagnosis" || !format.JSONSchema.Strict {
		t.Fatalf("json schema = %#v", format.JSONSchema)
	}
	payload, err := json.Marshal(format)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"type":"json_schema","json_schema":{"name":"resume_diagnosis","schema":{"type":"object"},"strict":true}}` {
		t.Fatalf("payload = %s", payload)
	}
}
