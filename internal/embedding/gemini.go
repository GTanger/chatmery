package embedding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GeminiClient 呼叫 Google Gemini embedContent API。
type GeminiClient struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
}

// NewGeminiClient 建立 Gemini embedding client。baseURL 例：https://generativelanguage.googleapis.com/v1beta，空則用此預設。
func NewGeminiClient(baseURL, model, apiKey string) *GeminiClient {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if model != "" && !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	return &GeminiClient{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed 回傳單一文字的向量；實作 memory.Embedder。
func (c *GeminiClient) Embed(text string) ([]float32, error) {
	reqBody := map[string]interface{}{
		"content": map[string]interface{}{
			"parts": []map[string]string{{"text": text}},
		},
	}
	body, _ := json.Marshal(reqBody)
	path := c.BaseURL + "/" + c.Model + ":embedContent"
	reqURL := path + "?key=" + url.QueryEscape(c.APIKey)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini embed status %d", resp.StatusCode)
	}
	var out struct {
		Embedding struct {
			Values []float64 `json:"values"`
		} `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embedding.Values) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	vec := make([]float32, len(out.Embedding.Values))
	for i, v := range out.Embedding.Values {
		vec[i] = float32(v)
	}
	return vec, nil
}
