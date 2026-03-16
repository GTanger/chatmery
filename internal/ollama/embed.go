package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// EmbedClient 呼叫 Ollama /api/embed 取得文字向量（L2 正規化，適合用餘弦相似度／內積檢索）。
type EmbedClient struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewEmbedClient 建立 embedding client；model 為空則回傳 nil（表示關閉向量檢索）。
func NewEmbedClient(baseURL, model string) *EmbedClient {
	if model == "" {
		return nil
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &EmbedClient{
		BaseURL: baseURL,
		Model:   model,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed 回傳單一文字的向量；Ollama 回傳為 L2-normalized，可直接用內積當相似度。
func (c *EmbedClient) Embed(text string) ([]float32, error) {
	if c == nil {
		return nil, nil
	}
	reqBody := map[string]interface{}{
		"model": c.Model,
		"input": text,
	}
	body, _ := json.Marshal(reqBody)
	resp, err := c.Client.Post(c.BaseURL+"/api/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed status %d", resp.StatusCode)
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	vec := make([]float32, len(out.Embeddings[0]))
	for i, v := range out.Embeddings[0] {
		vec[i] = float32(v)
	}
	return vec, nil
}
