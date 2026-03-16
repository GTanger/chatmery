package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient 呼叫 OpenAI 相容 API（OpenAI、Azure、其他相容端點）。
type OpenAIClient struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
}

// NewOpenAIClient 建立 OpenAI 相容 client。baseURL 例：https://api.openai.com/v1，空則用此預設。
func NewOpenAIClient(baseURL, model, apiKey string) *OpenAIClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &OpenAIClient{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// ChatStream 實作 Provider；使用 /chat/completions stream=true，解析 SSE。
func (c *OpenAIClient) ChatStream(messages []Message, onChunk func(text string)) (full string, err error) {
	reqBody := map[string]interface{}{
		"model":    c.Model,
		"messages": messages,
		"stream":   true,
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai chat status %d: %s", resp.StatusCode, string(rb))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace(line[6:])
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		for i := range chunk.Choices {
			if chunk.Choices[i].Delta.Content != "" {
				full += chunk.Choices[i].Delta.Content
				if onChunk != nil {
					onChunk(chunk.Choices[i].Delta.Content)
				}
			}
		}
	}
	return full, sc.Err()
}

// Generate 實作 Provider；使用 /chat/completions stream=false。
func (c *OpenAIClient) Generate(prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":    c.Model,
		"messages": []Message{{Role: "user", Content: prompt}},
		"stream":   false,
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai generate status %d: %s", resp.StatusCode, string(rb))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", nil
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
