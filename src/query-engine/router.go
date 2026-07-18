package queryengine

import (
	"errors"
)

type ProviderConfig struct {
	Provider     LLMProvider
	Models       []string
	DefaultModel string
}

type ProviderRouter struct {
	Configs  []ProviderConfig
	ModelMap map[string]LLMProvider
}

type ResolveResult struct {
	Provider LLMProvider
	Model    string
}

func NewProviderRouter() *ProviderRouter {
	return &ProviderRouter{
		Configs:  make([]ProviderConfig, 0),
		ModelMap: make(map[string]LLMProvider),
	}
}

func (pr *ProviderRouter) Register(config ProviderConfig) {
	pr.Configs = append(pr.Configs, config)
	for _, model := range config.Models {
		pr.ModelMap[model] = config.Provider
	}
}

// 根据输入的模型名称或提供者名称，返回对应的提供者（Provider）和模型
func (pr *ProviderRouter) Resolve(modelOrProvider string) *ResolveResult {
	if modelOrProvider != "" {
		if byModel, ok := pr.ModelMap[modelOrProvider]; ok {
			return &ResolveResult{
				Provider: byModel,
				Model:    modelOrProvider,
			}
		}

		for _, config := range pr.Configs {
			if config.Provider.Name() == modelOrProvider {
				return &ResolveResult{
					Provider: config.Provider,
					Model:    config.DefaultModel,
				}
			}
		}
	}

	if len(pr.Configs) == 0 {
		panic(errors.New("no providers registered"))
	}

	first := pr.Configs[0]
	return &ResolveResult{
		Provider: first.Provider,
		Model:    first.DefaultModel,
	}
}

func (pr *ProviderRouter) ListProviders() []string {
	result := make([]string, 0, len(pr.Configs))
	for _, config := range pr.Configs {
		result = append(result, config.Provider.Name())
	}
	return result
}
