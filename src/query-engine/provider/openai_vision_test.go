package provider

import (
	"testing"

	queryengine "MyOfferPilot/src/query-engine"
	openai "github.com/sashabaranov/go-openai"
)

func TestBuildMessagesIncludesVisionParts(t *testing.T) {
	provider := &OpenAIProvider{}
	content := "diagnose this resume"
	messages := provider.buildMessages([]queryengine.Message{{
		Role: queryengine.MessageRoleUser, Content: &content,
		Images: []queryengine.ImageInput{{URL: "data:image/jpeg;base64,page1", Detail: "high"}},
	}})
	if len(messages) != 1 || len(messages[0].MultiContent) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].MultiContent[0].Type != openai.ChatMessagePartTypeText ||
		messages[0].MultiContent[1].Type != openai.ChatMessagePartTypeImageURL ||
		messages[0].MultiContent[1].ImageURL.Detail != openai.ImageURLDetailHigh {
		t.Fatalf("multi content = %#v", messages[0].MultiContent)
	}
}
