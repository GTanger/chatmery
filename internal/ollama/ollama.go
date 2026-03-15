package ollama

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Client 呼叫本機 Ollama API。
type Client struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewClient 建立 Ollama client。
func NewClient(baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Model:   model,
		Client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// ChatMessage 單則訊息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatStream 發送 chat 並以 callback 接收串流片段；回傳完整回覆與錯誤。
func (c *Client) ChatStream(messages []ChatMessage, onChunk func(text string)) (full string, err error) {
	reqBody := map[string]interface{}{
		"model":    c.Model,
		"messages": messages,
		"stream":   true,
	}
	body, _ := json.Marshal(reqBody)
	resp, err := c.Client.Post(c.BaseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama chat status %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1024*1024)
	for sc.Scan() {
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if json.Unmarshal(sc.Bytes(), &chunk) != nil {
			continue
		}
		if chunk.Message.Content != "" {
			full += chunk.Message.Content
			if onChunk != nil {
				onChunk(chunk.Message.Content)
			}
		}
	}
	return full, sc.Err()
}

// Generate 單次 generate（用於 deconstruct 抽取事實），非串流。
func (c *Client) Generate(prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":  c.Model,
		"prompt": prompt,
		"stream": false,
	}
	body, _ := json.Marshal(reqBody)
	resp, err := c.Client.Post(c.BaseURL+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		log.Printf("[ollama] generate status %d: %s", resp.StatusCode, string(rb))
		return "", fmt.Errorf("ollama generate status %d", resp.StatusCode)
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Response), nil
}
