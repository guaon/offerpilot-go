package knowledge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type EmbeddingProvider interface {
	Embed(text string) ([]float64, error)
	EmbedBatch(text []string) ([][]float64, error)
	Dimension() int
	Model() string
}

type OpenAIEmbeddingProvider struct {
	dimensions int
	model      string
	apiKey     string
	baseURL    string
}

func NewOpenAIEmbeddingProvider(opts ...func(*OpenAIEmbeddingProvider)) *OpenAIEmbeddingProvider {
	p := &OpenAIEmbeddingProvider{
		dimensions: 1536,
		model:      "text-embedding-3-small",
		apiKey:     os.Getenv("OPENAI_API_KEY"),
		baseURL:    os.Getenv("OPENAI_BASE_URL"),
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.model == "text-embedding-3-large" {
		p.dimensions = 3072
	}

	return p
}

func (p *OpenAIEmbeddingProvider) Dimension() int        { return p.dimensions }
func (p *OpenAIEmbeddingProvider) Model() string         { return p.model }
func (p *OpenAIEmbeddingProvider) Dimensions() int       { return p.dimensions }
func (p *OpenAIEmbeddingProvider) APIKey() string        { return p.apiKey }
func (p *OpenAIEmbeddingProvider) SetBaseURL(url string) { p.baseURL = url }

func (p *OpenAIEmbeddingProvider) Embed(text string) ([]float64, error) {
	results, err := p.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

// maxTokensPerBatch is the maximum input length for text-embedding-3-small (8192 tokens).
// We use a conservative char estimate (~3 chars/token) to stay safely under the limit.
const maxCharsPerBatch = 8192 * 3 // ~24KB per batch

func (p *OpenAIEmbeddingProvider) EmbedBatch(texts []string) ([][]float64, error) {
	type embeddingResponse struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}

	var allVectors [][]float64
	for i := 0; i < len(texts); i++ {
		// chunk texts until we reach the size limit
		chunk := []string{texts[i]}
		batchLen := len(texts[i])
		for j := i + 1; j < len(texts) && batchLen+len(texts[j]) < maxCharsPerBatch; j++ {
			chunk = append(chunk, texts[j])
			batchLen += len(texts[j])
			i = j
		}

		reqBody, err := json.Marshal(map[string]interface{}{
			"model": p.model,
			"input": chunk,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request:%w", err)
		}

		req, err := http.NewRequest("POST", p.baseURL+"/embeddings", bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request:%w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request:%w", err)
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response:%w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("embedding API error (%d):%s", resp.StatusCode, string(body))
		}

		var result embeddingResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response:%w", err)
		}

		for _, item := range result.Data {
			allVectors = append(allVectors, item.Embedding)
		}
	}

	return allVectors, nil
}
