package search

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// Backend 搜尋後端：brave | tavily。Go 版不實作 DuckDuckGo（需第三方 scraper）。
type Backend struct {
	Kind         string
	BraveAPIKey  string
	TavilyAPIKey string
}

// Result 單筆搜尋結果，供注入 prompt 與附上引用連結。
type Result struct {
	Title   string
	URL     string
	Content string
}

// Search 執行搜尋，回傳最多 maxResults 條結果（含標題、URL、摘要）。
func (b *Backend) Search(query string, maxResults int) []Result {
	if maxResults <= 0 {
		maxResults = 5
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	switch b.Kind {
	case "brave":
		return b.brave(q, maxResults)
	case "tavily":
		return b.tavily(q, maxResults)
	default:
		log.Printf("[search] backend %q not implemented in Go (use brave or tavily)", b.Kind)
		return nil
	}
}

func (b *Backend) brave(query string, count int) []Result {
	if b.BraveAPIKey == "" {
		log.Println("[search] BRAVE_API_KEY not set")
		return nil
	}
	url := "https://api.search.brave.com/res/v1/web/search?q=" + strings.ReplaceAll(query, " ", "+")
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Printf("[search] brave req: %v", err)
		return nil
	}
	req.Header.Set("X-Subscription-Token", b.BraveAPIKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[search] brave: %v", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[search] brave status %d", resp.StatusCode)
		return nil
	}
	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("[search] brave decode: %v", err)
		return nil
	}
	var out []Result
	for i, r := range data.Web.Results {
		if i >= count {
			break
		}
		content := r.Description
		if content == "" {
			content = r.Title
		}
		if content != "" || r.URL != "" {
			out = append(out, Result{Title: r.Title, URL: r.URL, Content: content})
		}
	}
	return out
}

func (b *Backend) tavily(query string, maxResults int) []Result {
	if b.TavilyAPIKey == "" {
		log.Println("[search] TAVILY_API_KEY not set")
		return nil
	}
	body := map[string]interface{}{
		"api_key":      b.TavilyAPIKey,
		"query":        query,
		"max_results":  maxResults,
		"search_depth": "basic",
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(raw))
	if err != nil {
		log.Printf("[search] tavily req: %v", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[search] tavily: %v", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[search] tavily status %d", resp.StatusCode)
		return nil
	}
	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("[search] tavily decode: %v", err)
		return nil
	}
	var out []Result
	for i, r := range data.Results {
		if i >= maxResults {
			break
		}
		content := r.Content
		if content == "" {
			content = r.Title
		}
		if content != "" || r.URL != "" {
			out = append(out, Result{Title: r.Title, URL: r.URL, Content: content})
		}
	}
	return out
}
