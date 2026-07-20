package provider

import (
	queryengine "MyOfferPilot/src/query-engine"
	"context"
)

type DeepSeekProvider struct {
	*OpenAIProvider
}

func NewDeepSeekProvider(ctx context.Context, apiKey, baseURL string) (*DeepSeekProvider, error) {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	config := &OpenAIConfig{
		APIKey:  apiKey,
		Model:   "deepseek-chat",
		BaseURL: baseURL,
		Name:    "deepseek",
	}

	openaiProvider, err := NewOpenAIProvider(ctx, config)
	if err != nil {
		return nil, err
	}

	return &DeepSeekProvider{OpenAIProvider: openaiProvider}, nil
}

func (p *DeepSeekProvider) Name() string {
	return "deepseek"
}

func (p *DeepSeekProvider) Stream(params queryengine.StreamParams) <-chan queryengine.StreamEvent {
	return p.OpenAIProvider.Stream(params)
}

func (p *DeepSeekProvider) CountTokens(messages []queryengine.Message, tools []queryengine.ToolSchema, modelName string) (int, error) {
	return p.OpenAIProvider.CountTokens(messages, tools, modelName)
}

func (p *DeepSeekProvider) Validate(ctx context.Context) error {
	return p.OpenAIProvider.Validate(ctx)
}
