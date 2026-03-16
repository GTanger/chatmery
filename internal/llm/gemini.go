package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GeminiClient 呼叫 Google Gemini API（generativelanguage.googleapis.com）。
type GeminiClient struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
}

// NewGeminiClient 建立 Gemini client。baseURL 例：https://generativelanguage.googleapis.com/v1beta，空則用此預設。
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
		Client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// geminiContent 對應 API 的 Content（role + parts）。
type geminiContent struct {
	Role  string        `json:"role,omitempty"`
	Parts []geminiPart  `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// ChatStream 實作 Provider；使用 :streamGenerateContent?alt=sse，解析 SSE。
func (c *GeminiClient) ChatStream(messages []Message, onChunk func(text string)) (full string, err error) {
	var systemInstruction *geminiContent
	var contents []geminiContent
	for _, m := range messages {
		if m.Role == "system" {
			systemInstruction = &geminiContent{Parts: []geminiPart{{Text: m.Content}}}
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: m.Content}}})
	}
	if len(contents) == 0 {
		contents = []geminiContent{{Parts: []geminiPart{{Text: ""}}}}
	}
	reqBody := map[string]interface{}{
		"contents": contents,
	}
	if systemInstruction != nil {
		reqBody["systemInstruction"] = systemInstruction
	}
	body, _ := json.Marshal(reqBody)
	path := c.BaseURL + "/" + c.Model + ":streamGenerateContent"
	reqURL := path + "?alt=sse&key=" + url.QueryEscape(c.APIKey)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini stream status %d: %s", resp.StatusCode, string(rb))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace(line[6:])
		if len(data) == 0 {
			continue
		}
		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		for _, cand := range chunk.Candidates {
			for _, p := range cand.Content.Parts {
				if p.Text != "" {
					full += p.Text
					if onChunk != nil {
						onChunk(p.Text)
					}
				}
			}
		}
	}
	return full, sc.Err()
}

// Generate 實作 Provider；使用 :generateContent 非串流。
func (c *GeminiClient) Generate(prompt string) (string, error) {
	contents := []geminiContent{{Role: "user", Parts: []geminiPart{{Text: prompt}}}}
	reqBody := map[string]interface{}{"contents": contents}
	body, _ := json.Marshal(reqBody)
	path := c.BaseURL + "/" + c.Model + ":generateContent"
	reqURL := path + "?key=" + url.QueryEscape(c.APIKey)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini generate status %d: %s", resp.StatusCode, string(rb))
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", nil
	}
	return strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text), nil
}
