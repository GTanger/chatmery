package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	// 提煉（短期→長期）：短期條數超過 rollover 才觸發；一次送進模型的條數上限、每條最多幾字
	RefineRolloverLimit   int   // 短期事實超過此數才提煉，預設 20
	RefineBatchMaxItems   int   // 提煉時最多取幾條短期，預設 15
	RefineMaxRunesPerItem int   // 提煉時每條截斷字數，預設 120
	Timezone              string // IANA 時區（如 Asia/Taipei），空則用主機當地；對齊 OpenClaw 的 userTimezone
}

func Load() *Config {
	workspace := os.Getenv("CHATMERY_WORKSPACE")
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	if workspace == "" {
		exe, _ := os.Executable()
		workspace = filepath.Dir(exe)
	}
	cfg := &Config{
		Workspace:             workspace,
		SoulPath:              filepath.Join(workspace, "SOUL.md"),
		MemoryPath:            filepath.Join(workspace, "MEMORY.md"),
		ArchivalPath:          filepath.Join(workspace, "memory", "archival.jsonl"),
		Token:                 tokenFromEnv(),
		Model:                 getEnv("CHATMERY_MODEL", "qwen-4b-slim:latest"),
		OllamaURL:             getEnv("OLLAMA_HOST", "http://localhost:11434"),
		SearchBackend:         toLower(getEnv("CHATMERY_SEARCH", "brave")),
		BraveAPIKey:           os.Getenv("BRAVE_API_KEY"),
		TavilyAPIKey:          os.Getenv("TAVILY_API_KEY"),
		SearchKeywords:        []string{"最新", "新聞", "消息", "訊息", "上網", "搜尋", "查詢", "pixel", "202", "什麼是"},
		MemoryLongTermK:       3,
		MemorySessionK:        2,
		WebSearchMaxResults:   5,
		SnippetMaxRunes:       120,
		RefineRolloverLimit:   20,
		RefineBatchMaxItems:   15,
		RefineMaxRunesPerItem: 120,
		Timezone:              "",
	}
	applyTuningFile(cfg, filepath.Join(workspace, "chatmery.tuning"))
	applyEnvOverrides(cfg)
	if cfg.Timezone == "" {
		cfg.Timezone = os.Getenv("TZ")
	}
	return cfg
}

// applyTuningFile 從工作區內 chatmery.tuning 讀取 KEY=VALUE（# 註解、空行略過），覆寫 cfg 的數值；不處理 token/API key。
func applyTuningFile(cfg *Config, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if i := strings.Index(val, "#"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		if val == "" {
			continue
		}
		switch key {
		case "CHATMERY_MODEL":
			cfg.Model = val
		case "OLLAMA_HOST":
			cfg.OllamaURL = val
		case "CHATMERY_SEARCH":
			cfg.SearchBackend = toLower(val)
		case "CHATMERY_MEMORY_LONG_K":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.MemoryLongTermK = n
			}
		case "CHATMERY_MEMORY_SESSION_K":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.MemorySessionK = n
			}
		case "CHATMERY_WEB_SEARCH_MAX":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.WebSearchMaxResults = n
			}
		case "CHATMERY_SNIPPET_MAX":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.SnippetMaxRunes = n
			}
		case "CHATMERY_REFINE_ROLLOVER":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.RefineRolloverLimit = n
			}
		case "CHATMERY_REFINE_BATCH_MAX":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.RefineBatchMaxItems = n
			}
		case "CHATMERY_REFINE_RUNES_PER_ITEM":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.RefineMaxRunesPerItem = n
			}
		case "CHATMERY_TZ", "TZ":
			cfg.Timezone = val
		}
	}
}

// applyEnvOverrides 環境變數覆寫 cfg（同 key 時 env 優先）。
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CHATMERY_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("OLLAMA_HOST"); v != "" {
		cfg.OllamaURL = v
	}
	if v := os.Getenv("CHATMERY_SEARCH"); v != "" {
		cfg.SearchBackend = toLower(v)
	}
	if n := intEnv("CHATMERY_MEMORY_LONG_K", -1); n >= 0 {
		cfg.MemoryLongTermK = n
	}
	if n := intEnv("CHATMERY_MEMORY_SESSION_K", -1); n >= 0 {
		cfg.MemorySessionK = n
	}
	if n := intEnv("CHATMERY_WEB_SEARCH_MAX", -1); n >= 0 {
		cfg.WebSearchMaxResults = n
	}
	if n := intEnv("CHATMERY_SNIPPET_MAX", -1); n >= 0 {
		cfg.SnippetMaxRunes = n
	}
	if n := intEnv("CHATMERY_REFINE_ROLLOVER", -1); n >= 0 {
		cfg.RefineRolloverLimit = n
	}
	if n := intEnv("CHATMERY_REFINE_BATCH_MAX", -1); n >= 0 {
		cfg.RefineBatchMaxItems = n
	}
	if n := intEnv("CHATMERY_REFINE_RUNES_PER_ITEM", -1); n >= 0 {
		cfg.RefineMaxRunesPerItem = n
	}
	if v := os.Getenv("CHATMERY_TZ"); v != "" {
		cfg.Timezone = v
	} else if v := os.Getenv("TZ"); v != "" && cfg.Timezone == "" {
		cfg.Timezone = v
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
