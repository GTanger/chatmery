package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Workspace      string
	SoulPath       string
	MemoryPath     string
	ArchivalPath   string
	Token          string
	Model          string
	OllamaURL      string
	SearchBackend  string
	BraveAPIKey    string
	TavilyAPIKey   string
	SearchKeywords []string
	// 小模型優化：控制注入量，節省 context、加快回覆
	MemoryLongTermK   int // 長期記憶最多幾條，預設 3
	MemorySessionK    int // 當前 session 記憶最多幾條，預設 2
	WebSearchMaxResults int // 網搜最多幾條，預設 3
	SnippetMaxRunes   int // 每條記憶/搜尋結果最多幾字（0=不截），預設 120
}

func Load() *Config {
	workspace := os.Getenv("CHATMERY_WORKSPACE")
	if workspace == "" {
		// 預設為當前工作目錄（go run 時執行檔在暫存目錄，故不用 Executable）
		workspace, _ = os.Getwd()
	}
	if workspace == "" {
		exe, _ := os.Executable()
		workspace = filepath.Dir(exe)
	}
	return &Config{
		Workspace:          workspace,
		SoulPath:           filepath.Join(workspace, "SOUL.md"),
		MemoryPath:         filepath.Join(workspace, "MEMORY.md"),
		ArchivalPath:       filepath.Join(workspace, "memory", "archival.jsonl"),
		Token:              tokenFromEnv(),
		Model:              getEnv("CHATMERY_MODEL", "qwen-4b-slim:latest"),
		OllamaURL:          getEnv("OLLAMA_HOST", "http://localhost:11434"),
		SearchBackend:      toLower(getEnv("CHATMERY_SEARCH", "brave")),
		BraveAPIKey:        os.Getenv("BRAVE_API_KEY"),
		TavilyAPIKey:       os.Getenv("TAVILY_API_KEY"),
		SearchKeywords:     []string{"最新", "新聞", "消息", "搜尋", "查詢", "pixel", "202", "什麼是"},
		MemoryLongTermK:    intEnv("CHATMERY_MEMORY_LONG_K", 3),
		MemorySessionK:    intEnv("CHATMERY_MEMORY_SESSION_K", 2),
		WebSearchMaxResults: intEnv("CHATMERY_WEB_SEARCH_MAX", 3),
		SnippetMaxRunes:    intEnv("CHATMERY_SNIPPET_MAX", 120),
	}
}

func intEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func tokenFromEnv() string {
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		return v
	}
	return os.Getenv("CHATMERY_TELEGRAM_TOKEN")
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
