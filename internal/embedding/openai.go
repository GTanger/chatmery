package embedding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient 呼叫 OpenAI 相容 /embeddings API（OpenAI、Azure 等）。
type OpenAIClient struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
}

// NewOpenAIClient 建立 OpenAI 相容 embedding client。baseURL 例：https://api.openai.com/v1，空則用此預設。
func NewOpenAIClient(baseURL, model, apiKey string) *OpenAIClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &OpenAIClient{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed 回傳單一文字的向量；實作 memory.Embedder。
func (c *OpenAIClient) Embed(text string) ([]float32, error) {
	reqBody := map[string]interface{}{
		"model": c.Model,
		"input": text,
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed status %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	vec := make([]float32, len(out.Data[0].Embedding))
	for i, v := range out.Data[0].Embedding {
		vec[i] = float32(v)
	}
	return vec, nil
}
