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
	Model          string
	OllamaURL      string
	// 聊天後端：ollama（預設）| openai | gemini | openrouter
	Provider           string // ollama | openai | gemini | openrouter
	OpenAIBaseURL      string
	OpenAIAPIKey       string
	GeminiBaseURL      string // 例：https://generativelanguage.googleapis.com/v1beta；空則用預設
	GeminiAPIKey       string // 僅從環境變數讀取
	OpenRouterBaseURL  string // 例：https://openrouter.ai/api/v1
	OpenRouterAPIKey   string // 僅從環境變數讀取
	SearchBackend       string
	BraveAPIKey         string
	TavilyAPIKey        string
	UseLLMForSearchQuery bool // true：用 LLM 依使用者句意產出搜尋 query（Cursor 式）；false：用 BuildQuery 關鍵字
	SearchKeywords      []string
	// 小模型優化：控制注入量，節省 context、加快回覆
	MemoryLongTermK   int // 長期記憶最多幾條（答覆時），預設 3
	MemorySessionK    int // 短期記憶最多幾條（答覆時），預設 5
	WebSearchMaxResults int // 網搜最多幾條，預設 3
	SnippetMaxRunes   int // 每條記憶/搜尋結果最多幾字（0=不截），預設 120
	// 四池一魂：答覆注入條數 當前1+靈魂1+核心2+長期3+短期5
	MemoryCurrentK int // 當前 1
	MemorySoulK    int // 靈魂 1
	MemoryCoreK    int // 核心 2
	MemoryLongK    int // 長期 3
	MemoryShortK   int // 短期 5
	// 四池容量與濃縮：短期 cap→濃縮進長期；長期 cap→濃縮進核心；核心 cap→濃縮進靈魂
	ShortTermCap    int // 短期池上限 200
	ShortCondenseTo int // 短期滿後濃縮成幾句進長期 50
	LongTermCap     int // 長期池上限 100
	LongCondenseTo  int // 長期滿後濃縮成幾句進核心 5
	CoreCap         int // 核心池上限 20
	CoreCondenseTo  int // 核心滿後濃縮成 1 句進靈魂
	// 舊提煉參數（相容用，四池啟用時以 SnippetMaxRunes 截斷）
	RefineRolloverLimit   int
	RefineBatchMaxItems   int
	RefineMaxRunesPerItem int
	Timezone              string // IANA 時區（如 Asia/Taipei），空則用主機當地；對齊 OpenClaw 的 userTimezone
	OutputLang            string // 回覆一律使用的語言（如「繁體中文」）；留空=不限制，預設 繁體中文
	// 向量檢索（語意相似）：空則用關鍵字檢索
	EmbedModel    string // 模型名（ollama: nomic-embed-text；openai: text-embedding-3-small；gemini: text-embedding-004）；空=關閉
	EmbedURL      string // Ollama embed URL，預設同 OLLAMA_HOST（僅 EmbedProvider=ollama 時用）
	EmbedProvider string // ollama（預設）| openai | gemini；openai/gemini 時用對應 API key 與 base URL
	// 等候回覆時的 placeholder 文案；多句時每次隨機選一句，空則用內建預設
	PlaceholderMessages []string
	// 聊天／串流請求逾時（秒），含讀取 body；預設 300，避免慢模型觸發 context deadline exceeded
	ChatTimeoutSec int
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
		Model:                 getEnv("CHATMERY_MODEL", "gemma3:4b"),
		OllamaURL:             getEnv("OLLAMA_HOST", "http://localhost:11434"),
		Provider:              toLower(getEnv("CHATMERY_PROVIDER", "ollama")),
		OpenAIBaseURL:         getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIAPIKey:          os.Getenv("OPENAI_API_KEY"),
		GeminiBaseURL:         getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		GeminiAPIKey:          os.Getenv("GEMINI_API_KEY"),
		OpenRouterBaseURL:     getEnv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
		OpenRouterAPIKey:      os.Getenv("OPENROUTER_API_KEY"),
		SearchBackend:         toLower(getEnv("CHATMERY_SEARCH", "brave")),
		BraveAPIKey:           os.Getenv("BRAVE_API_KEY"),
		TavilyAPIKey:          os.Getenv("TAVILY_API_KEY"),
		SearchKeywords:        []string{"最新", "新聞", "消息", "訊息", "上網", "搜尋", "查詢", "幫我查", "幫我找", "什麼是", "如何", "怎麼", "為什麼", "哪裡", "最近", "現在", "pixel", "202"},
		MemoryLongTermK:       3,
		MemorySessionK:        5,
		WebSearchMaxResults:   5,
		SnippetMaxRunes:       120,
		MemoryCurrentK:        1,
		MemorySoulK:           1,
		MemoryCoreK:           2,
		MemoryLongK:           3,
		MemoryShortK:          5,
		ShortTermCap:          200,
		ShortCondenseTo:       50,
		LongTermCap:           100,
		LongCondenseTo:        5,
		CoreCap:               20,
		CoreCondenseTo:        1,
		RefineRolloverLimit:   20,
		RefineBatchMaxItems:   15,
		RefineMaxRunesPerItem: 120,
		Timezone:              "",
		OutputLang:            getEnv("CHATMERY_OUTPUT_LANG", "繁體中文"),
		EmbedModel:            getEnv("CHATMERY_EMBED_MODEL", ""),
		EmbedURL:              getEnv("CHATMERY_EMBED_URL", ""),
		EmbedProvider:         toLower(getEnv("CHATMERY_EMBED_PROVIDER", "ollama")),
		PlaceholderMessages:   nil, // 由 tuning/env 填；nil 或空則 main 用內建預設
		ChatTimeoutSec:        intEnv("CHATMERY_CHAT_TIMEOUT_SEC", 300),
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
		if key == "CHATMERY_OUTPUT_LANG" {
			cfg.OutputLang = val
			continue
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
		case "CHATMERY_SEARCH_QUERY_LLM":
			cfg.UseLLMForSearchQuery = (val == "true" || val == "1" || val == "yes")
		case "CHATMERY_MEMORY_LONG_K":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.MemoryLongTermK = n
				cfg.MemoryLongK = n
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
		case "CHATMERY_MEMORY_CORE_K":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.MemoryCoreK = n
			}
		case "CHATMERY_MEMORY_SHORT_K":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.MemoryShortK = n
			}
		case "CHATMERY_SHORT_TERM_CAP":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.ShortTermCap = n
			}
		case "CHATMERY_SHORT_CONDENSE_TO":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.ShortCondenseTo = n
			}
		case "CHATMERY_LONG_TERM_CAP":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.LongTermCap = n
			}
		case "CHATMERY_LONG_CONDENSE_TO":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.LongCondenseTo = n
			}
		case "CHATMERY_CORE_CAP":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.CoreCap = n
			}
		case "CHATMERY_CORE_CONDENSE_TO":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.CoreCondenseTo = n
			}
		case "CHATMERY_TZ", "TZ":
			cfg.Timezone = val
		case "CHATMERY_EMBED_MODEL":
			cfg.EmbedModel = val
		case "CHATMERY_EMBED_URL":
			cfg.EmbedURL = val
		case "CHATMERY_EMBED_PROVIDER":
			cfg.EmbedProvider = toLower(val)
		case "CHATMERY_PLACEHOLDER":
			cfg.PlaceholderMessages = splitPlaceholders(val)
		case "CHATMERY_PROVIDER":
			cfg.Provider = toLower(val)
		case "OPENAI_BASE_URL":
			if val != "" {
				cfg.OpenAIBaseURL = val
			}
		case "GEMINI_BASE_URL":
			if val != "" {
				cfg.GeminiBaseURL = val
			}
		case "OPENROUTER_BASE_URL":
			if val != "" {
				cfg.OpenRouterBaseURL = val
			}
		case "CHATMERY_CHAT_TIMEOUT_SEC":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.ChatTimeoutSec = n
			}
		}
	}
}


// splitPlaceholders 依 | 切分，trim 每段，過濾空字串。
func splitPlaceholders(s string) []string {
	parts := strings.Split(s, "|")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
	if v := os.Getenv("CHATMERY_SEARCH_QUERY_LLM"); v != "" {
		cfg.UseLLMForSearchQuery = (v == "true" || v == "1" || v == "yes")
	}
	if n := intEnv("CHATMERY_MEMORY_LONG_K", -1); n >= 0 {
		cfg.MemoryLongTermK = n
		cfg.MemoryLongK = n
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
	if n := intEnv("CHATMERY_MEMORY_CORE_K", -1); n >= 0 {
		cfg.MemoryCoreK = n
	}
	if n := intEnv("CHATMERY_MEMORY_SHORT_K", -1); n >= 0 {
		cfg.MemoryShortK = n
	}
	if n := intEnv("CHATMERY_SHORT_TERM_CAP", -1); n > 0 {
		cfg.ShortTermCap = n
	}
	if n := intEnv("CHATMERY_SHORT_CONDENSE_TO", -1); n > 0 {
		cfg.ShortCondenseTo = n
	}
	if n := intEnv("CHATMERY_LONG_TERM_CAP", -1); n > 0 {
		cfg.LongTermCap = n
	}
	if n := intEnv("CHATMERY_LONG_CONDENSE_TO", -1); n > 0 {
		cfg.LongCondenseTo = n
	}
	if n := intEnv("CHATMERY_CORE_CAP", -1); n > 0 {
		cfg.CoreCap = n
	}
	if n := intEnv("CHATMERY_CORE_CONDENSE_TO", -1); n > 0 {
		cfg.CoreCondenseTo = n
	}
	if n := intEnv("CHATMERY_CHAT_TIMEOUT_SEC", -1); n > 0 {
		cfg.ChatTimeoutSec = n
	}
	if v := os.Getenv("CHATMERY_TZ"); v != "" {
		cfg.Timezone = v
	} else if v := os.Getenv("TZ"); v != "" && cfg.Timezone == "" {
		cfg.Timezone = v
	}
	if v, ok := os.LookupEnv("CHATMERY_OUTPUT_LANG"); ok {
		cfg.OutputLang = v
	}
	if v := os.Getenv("CHATMERY_EMBED_MODEL"); v != "" {
		cfg.EmbedModel = v
	}
	if v := os.Getenv("CHATMERY_EMBED_URL"); v != "" {
		cfg.EmbedURL = v
	}
	if v := os.Getenv("CHATMERY_EMBED_PROVIDER"); v != "" {
		cfg.EmbedProvider = toLower(v)
	}
	if cfg.EmbedURL == "" && cfg.EmbedModel != "" && cfg.EmbedProvider == "ollama" {
		cfg.EmbedURL = cfg.OllamaURL
	}
	if v := os.Getenv("CHATMERY_PLACEHOLDER"); v != "" {
		cfg.PlaceholderMessages = splitPlaceholders(v)
	}
	if v := os.Getenv("CHATMERY_PROVIDER"); v != "" {
		cfg.Provider = toLower(v)
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.OpenAIBaseURL = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.OpenAIAPIKey = v
	} else if v := os.Getenv("CHATMERY_OPENAI_API_KEY"); v != "" {
		cfg.OpenAIAPIKey = v
	}
	if v := os.Getenv("GEMINI_BASE_URL"); v != "" {
		cfg.GeminiBaseURL = v
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.GeminiAPIKey = v
	} else if v := os.Getenv("CHATMERY_GEMINI_API_KEY"); v != "" {
		cfg.GeminiAPIKey = v
	}
	if v := os.Getenv("OPENROUTER_API_KEY"); v != "" {
		cfg.OpenRouterAPIKey = v
	} else if v := os.Getenv("CHATMERY_OPENROUTER_API_KEY"); v != "" {
		cfg.OpenRouterAPIKey = v
	}
	if v := os.Getenv("OPENROUTER_BASE_URL"); v != "" {
		cfg.OpenRouterBaseURL = v
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
