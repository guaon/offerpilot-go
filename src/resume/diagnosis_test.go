package resume

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	queryengine "MyOfferPilot/src/query-engine"
)

type scriptedQueryClient struct {
	results []Result
	calls   []queryengine.QueryParams
}

type delayedRepairClient struct {
	calls int
}

func (client *delayedRepairClient) Query(params queryengine.QueryParams) (queryengine.ParsedResponse, error) {
	select {
	case <-params.Context.Done():
		return queryengine.ParsedResponse{}, params.Context.Err()
	case <-time.After(60 * time.Millisecond):
	}
	client.calls++
	result := validDiagnosis("text_only")
	if client.calls == 1 {
		result.OverallScore = 101
	}
	data, _ := json.Marshal(result)
	content := string(data)
	return queryengine.ParsedResponse{Type: queryengine.TextResponse, Content: &content}, nil
}

type responseFormatFallbackClient struct {
	calls []queryengine.QueryParams
}

func (client *responseFormatFallbackClient) Query(params queryengine.QueryParams) (queryengine.ParsedResponse, error) {
	client.calls = append(client.calls, params)
	if len(client.calls) == 1 {
		return queryengine.ParsedResponse{}, errors.New("response_format json_schema is unsupported")
	}
	data, _ := json.Marshal(validDiagnosis("text_only"))
	content := string(data)
	return queryengine.ParsedResponse{Type: queryengine.TextResponse, Content: &content}, nil
}

func (client *scriptedQueryClient) Query(params queryengine.QueryParams) (queryengine.ParsedResponse, error) {
	client.calls = append(client.calls, params)
	result := client.results[0]
	client.results = client.results[1:]
	data, _ := json.Marshal(result)
	content := string(data)
	return queryengine.ParsedResponse{Type: queryengine.TextResponse, Content: &content}, nil
}

func TestDiagnoseUsesImagesAndReturnsValidatedResult(t *testing.T) {
	client := &scriptedQueryClient{results: []Result{validDiagnosis("multimodal")}}
	diagnostician := NewDiagnostician(client, 0)
	request := Request{
		Content: strings.Repeat("项目经历：负责 Agent Runtime，延迟降低 30%。\n", 20),
		Images:  []string{"data:image/jpeg;base64,page1"}, Model: "gpt-4o",
	}
	result, err := diagnostician.Diagnose(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent != "resume_diagnostician" || len(client.calls) != 1 {
		t.Fatalf("result=%#v calls=%d", result, len(client.calls))
	}
	message := client.calls[0].Messages[0]
	if len(message.Images) != 1 || message.Images[0].Detail != "high" {
		t.Fatalf("message images = %#v", message.Images)
	}
}

func TestDiagnoseRepairsInvalidResultOnce(t *testing.T) {
	invalid := validDiagnosis("text_only")
	invalid.OverallScore = 101
	client := &scriptedQueryClient{results: []Result{invalid, validDiagnosis("text_only")}}
	diagnostician := NewDiagnostician(client, 0)
	_, err := diagnostician.Diagnose(t.Context(), Request{Content: strings.Repeat("完整简历内容。", 50)})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 || !strings.Contains(*client.calls[1].Messages[0].Content, "校验错误") {
		t.Fatalf("repair calls = %#v", client.calls)
	}
}

func TestDiagnoseGivesRepairCallAnIndependentTimeout(t *testing.T) {
	client := &delayedRepairClient{}
	diagnostician := NewDiagnostician(client, 100*time.Millisecond)
	started := time.Now()
	_, err := diagnostician.Diagnose(t.Context(), Request{Content: strings.Repeat("完整简历内容。", 50)})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 || time.Since(started) < 120*time.Millisecond {
		t.Fatalf("calls=%d elapsed=%s", client.calls, time.Since(started))
	}
}

func TestDiagnoseFallsBackWhenJSONSchemaIsUnsupported(t *testing.T) {
	client := &responseFormatFallbackClient{}
	diagnostician := NewDiagnostician(client, time.Second)
	_, err := diagnostician.Diagnose(t.Context(), Request{Content: strings.Repeat("完整简历内容。", 50)})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("calls=%d", len(client.calls))
	}
	if client.calls[0].ResponseFormat == nil || client.calls[0].ResponseFormat.Type != "json_schema" {
		t.Fatalf("first response format = %#v", client.calls[0].ResponseFormat)
	}
	if client.calls[1].ResponseFormat == nil || client.calls[1].ResponseFormat.Type != "json_object" {
		t.Fatalf("fallback response format = %#v", client.calls[1].ResponseFormat)
	}
}

func validDiagnosis(mode string) Result {
	layout := LayoutAssessment{Summary: "未提供页面图片，本次仅诊断文字内容。"}
	if mode == "multimodal" {
		layout.Score = 8
		layout.Summary = "版式层级清晰，但项目区域信息密度略高。"
	}
	sections := []SectionDiagnosis{
		{Section: "项目经历", Score: 8, Evidence: []string{"负责 Agent Runtime，延迟降低 30%"}, Suggestions: []string{"补充压测口径和职责边界"}, Rewrite: "负责 Agent Runtime 核心链路，将延迟降低 30%，并补充压测环境与个人职责。"},
		{Section: "专业技能", Score: 7, Evidence: []string{"使用 Go 开发运行时组件"}, Suggestions: []string{"将技能绑定到代表项目"}, Rewrite: "Go：用于 Agent Runtime 核心组件开发，并通过项目指标验证工程效果。"},
	}
	return Result{
		OverallScore: 80, Summary: "候选人的 Agent 工程主线清晰，已有量化结果，但职责和验证口径仍需补充。",
		Strengths: []string{"工程主线清晰", "具备量化结果"}, Risks: []string{"职责边界不够明确"},
		Diagnosis: sections, Layout: layout, Mode: mode,
	}
}
