// Chatmery Web：以 sw.ygggt.com/chatmery 掛載的對話殼，與奇點同架構（Go + web/ 靜態檔）。
// 執行時請在專案根目錄（含 web/ 與 chatmery.tuning），或設 CHATMERY_WORKSPACE。
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tanger/chatmery/internal/config"
	"github.com/tanger/chatmery/internal/document"
	"github.com/tanger/chatmery/internal/llm"
	"github.com/tanger/chatmery/internal/memory"
	"github.com/tanger/chatmery/internal/search"
)

const defaultWebPort = "1721"

// ensureMemoryFiles 建立記憶目錄與預設 SOUL.md / MEMORY.md（若不存在），方便使用者找到檔案。
func ensureMemoryFiles(cfg *config.Config) {
	memDir := filepath.Dir(cfg.ArchivalPath)
	_ = os.MkdirAll(memDir, 0755)
	if _, err := os.Stat(cfg.SoulPath); err != nil && os.IsNotExist(err) {
		_ = os.WriteFile(cfg.SoulPath, []byte("# SOUL.md — 助手身份與人設（可編輯）\nYou are a helpful assistant.\n"), 0644)
	}
	if _, err := os.Stat(cfg.MemoryPath); err != nil && os.IsNotExist(err) {
		_ = os.WriteFile(cfg.MemoryPath, []byte("# MEMORY.md — 背景摘要（可編輯，留空亦可）\n"), 0644)
	}
}

func main() {
	cfg := config.Load()
	ensureMemoryFiles(cfg)
	log.Printf("[記憶] 工作目錄=%s | SOUL=%s | MEMORY=%s | archival=%s", cfg.Workspace, cfg.SoulPath, cfg.MemoryPath, cfg.ArchivalPath)
	searchKey := "未設定"
	if cfg.SearchBackend == "brave" && cfg.BraveAPIKey != "" {
		searchKey = "brave（已設 key）"
	} else if cfg.SearchBackend == "tavily" && cfg.TavilyAPIKey != "" {
		searchKey = "tavily（已設 key）"
	} else if cfg.SearchBackend == "brave" {
		searchKey = "brave（未設 BRAVE_API_KEY，無法上網）"
	} else if cfg.SearchBackend == "tavily" {
		searchKey = "tavily（未設 TAVILY_API_KEY，無法上網）"
	} else {
		searchKey = cfg.SearchBackend + "（不支援或未設 key）"
	}
	log.Printf("[網搜] %s", searchKey)
	backstory := memory.NewBackstory(cfg.SoulPath, cfg.MemoryPath)
	memoryDir := filepath.Dir(cfg.ArchivalPath)
	tiers := memory.NewTiers(memoryDir, cfg.ShortTermCap, cfg.ShortCondenseTo, cfg.LongTermCap, cfg.LongCondenseTo, cfg.CoreCap, cfg.CoreCondenseTo, cfg.SnippetMaxRunes)
	var recentMu sync.Mutex
	var recentTurns []struct{ User, Assistant string }
	var condenseBuffer []struct{ User, Assistant string }
	const maxRecentTurns = 20
	const condenseRounds = 20
	searchBackend := &search.Backend{
		Kind:         cfg.SearchBackend,
		BraveAPIKey:  cfg.BraveAPIKey,
		TavilyAPIKey: cfg.TavilyAPIKey,
	}
	chatTimeout := time.Duration(cfg.ChatTimeoutSec) * time.Second
	if chatTimeout <= 0 {
		chatTimeout = 300 * time.Second
	}
	var chatProvider llm.Provider
	switch cfg.Provider {
	case "openai":
		if cfg.OpenAIAPIKey != "" {
			chatProvider = llm.NewOpenAIClient(cfg.OpenAIBaseURL, cfg.Model, cfg.OpenAIAPIKey)
		} else {
			chatProvider = llm.NewOllama(cfg.OllamaURL, cfg.Model, chatTimeout)
		}
	case "gemini":
		if cfg.GeminiAPIKey != "" {
			chatProvider = llm.NewGeminiClient(cfg.GeminiBaseURL, cfg.Model, cfg.GeminiAPIKey)
		} else {
			chatProvider = llm.NewOllama(cfg.OllamaURL, cfg.Model, chatTimeout)
		}
	case "openrouter":
		if cfg.OpenRouterAPIKey != "" {
			chatProvider = llm.NewOpenAIClient(cfg.OpenRouterBaseURL, cfg.Model, cfg.OpenRouterAPIKey)
		} else {
			chatProvider = llm.NewOllama(cfg.OllamaURL, cfg.Model, chatTimeout)
		}
	default:
		chatProvider = llm.NewOllama(cfg.OllamaURL, cfg.Model, chatTimeout)
	}

	webRoot := filepath.Join(cfg.Workspace, "web")
	if _, err := os.Stat(webRoot); err != nil {
		webRoot = "web"
	}
	fs := http.FileServer(http.Dir(webRoot))

	http.HandleFunc("/chatmery", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chatmery" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/chatmery/", http.StatusFound)
	})

	http.Handle("/chatmery/", http.StripPrefix("/chatmery/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "" || p == "/" || p == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
			http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})))

	handleChat := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var userText string
		docContext := ""

		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, "invalid multipart", http.StatusBadRequest)
				return
			}
			userText = strings.TrimSpace(r.FormValue("text"))
			file, header, err := r.FormFile("file")
			if err == nil && header != nil && header.Filename != "" {
				defer file.Close()
				ext := filepath.Ext(header.Filename)
				if ext == "" {
					ext = ".txt"
				}
				tmp, err := os.CreateTemp("", "chatmery-*"+ext)
				if err != nil {
					http.Error(w, "無法暫存上傳檔案", http.StatusBadRequest)
					return
				}
				tmpPath := tmp.Name()
				defer os.Remove(tmpPath)
				if _, err := io.Copy(tmp, file); err != nil {
					tmp.Close()
					http.Error(w, "無法寫入暫存檔", http.StatusBadRequest)
					return
				}
				if err := tmp.Close(); err != nil {
					http.Error(w, "無法關閉暫存檔", http.StatusBadRequest)
					return
				}
				extracted, err := document.ExtractText(tmpPath)
				if err != nil || extracted == "" {
					http.Error(w, "無法擷取該檔案文字（支援 PDF、txt、md、docx、xlsx、odt、ods）", http.StatusBadRequest)
					return
				}
				docContext = "## 附檔內容（上傳檔案）\n（以下為使用者上傳檔案的擷取文字，請直接依內容回答。）\n\n" + extracted + "\n\n"
				if userText == "" {
					userText = "請根據上述附檔內容回答。"
				}
			} else if userText == "" {
				http.Error(w, "text or file required", http.StatusBadRequest)
				return
			}
		}
		webSearchOn := false
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if v := r.FormValue("web_search"); v == "true" || v == "1" {
				webSearchOn = true
			}
		} else {
			var body struct {
				Text      string `json:"text"`
				WebSearch bool   `json:"web_search"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			userText = strings.TrimSpace(body.Text)
			webSearchOn = body.WebSearch
			if userText == "" {
				http.Error(w, "text required", http.StatusBadRequest)
				return
			}
			if url, rest, ok := parseReadURLIntent(userText); ok {
				extracted, err := document.ExtractTextFromURL(url)
				if err != nil || extracted == "" {
					writeSSEErr(w, "無法讀取該網頁（請確認網址可連線）。")
					return
				}
				docContext = "## 附檔內容（網頁）\n（以下為程式已抓取的正文，請直接依內容回答，勿說「正在聯網」「正在讀取」「正在思考」等。）\n\n" + extracted + "\n\n"
				if strings.TrimSpace(rest) != "" {
					userText = strings.TrimSpace(rest)
				} else {
					userText = "請根據上述網頁內容回答。"
				}
			}
		}

		// 讀本機檔：若尚無附檔內容，檢查「讀 路徑」／「讀取 路徑」
		if docContext == "" {
			if localPath, rest, ok := parseReadLocalFileIntent(userText); ok {
				extracted, err := document.ExtractText(localPath)
				if err != nil || extracted == "" {
					writeSSEErr(w, "無法讀取該本機檔案（路徑需可存取；支援 PDF、txt、md、docx、xlsx、odt、ods）。")
					return
				}
				docContext = "## 附檔內容（本機檔案）\n（以下為程式從本機路徑擷取的文字，請直接依內容回答。）\n\n" + extracted + "\n\n"
				if strings.TrimSpace(rest) != "" {
					userText = strings.TrimSpace(rest)
				} else {
					userText = "請根據上述檔案內容回答。"
				}
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		loc := time.Local
		if cfg.Timezone != "" {
			if l, err := time.LoadLocation(cfg.Timezone); err == nil {
				loc = l
			}
		}
		now := time.Now().In(loc)
		dateStr := now.Format("2006-01-02")
		hour, min := now.Hour(), now.Minute()
		naturalDate := now.Format("2006年1月2日")
		if asksForDateOrTime(userText) && utf8.RuneCountInString(userText) <= 60 {
			reply := naturalDate + "，現在 " + strconv.Itoa(hour) + " 點 " + strconv.Itoa(min) + " 分。"
			writeSSEText(w, reply)
			return
		}

		maxRunes := cfg.SnippetMaxRunes
		capRune := func(s string) string {
			if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
				return s
			}
			n := 0
			for i := range s {
				if n >= maxRunes {
					return s[:i] + "…"
				}
				n++
			}
			return s
		}

		var webLines []string
		didSearch := false
		if webSearchOn || hasSearchIntent(userText, cfg.SearchKeywords) {
			didSearch = true
			q := search.BuildQuery(userText)
			if q != "" {
				q = dateStr + " " + q
			} else {
				q = dateStr
			}
			for _, r := range searchBackend.Search(q, cfg.WebSearchMaxResults) {
				webLines = append(webLines, "- [搜尋] "+capRune(r))
			}
		}
		var webContext string
		if len(webLines) > 0 {
			webContext = "本則對話的當前時間：" + naturalDate + " " + strconv.Itoa(hour) + "點" + strconv.Itoa(min) + "分。\n" + strings.Join(webLines, "\n")
		} else if didSearch {
			webContext = "（本次網搜無結果，請簡短回覆「沒有搜尋到即時結果」即可。）"
		} else {
			webContext = "(無)"
		}

		// 答覆 context = 當前1 + 靈魂1 + 核心2 + 長期3 + 短期5
		currentFact := tiers.GetCurrentFact()
		soul := backstory.GetSoul()
		coreHits := tiers.CoreHits(userText, cfg.MemoryCoreK)
		longHits := tiers.LongTermHits(userText, cfg.MemoryLongK)
		shortHits := tiers.ShortTermHits(userText, cfg.MemoryShortK)
		var memLines []string
		if currentFact != "" && cfg.MemoryCurrentK > 0 {
			memLines = append(memLines, "- [當前] "+capRune(currentFact))
		}
		if soul != "" && cfg.MemorySoulK > 0 {
			memLines = append(memLines, "- [靈魂] "+capRune(soul))
		}
		for _, s := range coreHits {
			memLines = append(memLines, "- [核心] "+capRune(s))
		}
		for _, s := range longHits {
			memLines = append(memLines, "- [長期] "+capRune(s))
		}
		for _, s := range shortHits {
			memLines = append(memLines, "- [短期] "+capRune(s))
		}
		memoryContext := strings.Join(memLines, "\n")
		if memoryContext == "" {
			memoryContext = "(無)"
		}

		summary := backstory.GetMemory()
		zoneName, _ := now.Zone()
		zoneLine := "時區: " + zoneName
		if cfg.Timezone != "" {
			zoneLine += " (" + cfg.Timezone + ")"
		}
		timeStr := now.Format("15:04")
		nowSentence := "本則對話的當前時間：" + naturalDate + " " + strconv.Itoa(hour) + "點" + strconv.Itoa(min) + "分。\n"
		timeBlock := "## 當前日期與時間\n" + nowSentence + zoneLine + "\n當前日期: " + dateStr + "\n當前時刻: " + timeStr + "（24小時制）\n"
		systemPrompt := soul + "\n\n" + timeBlock + "## 記憶\n" + memoryContext +
			"\n\n## 即時\n" + webContext +
			"\n\n" + docContext +
			"## 背景\n" + summary +
			"\n\n## 能力邊界\n你可依本則對話、記憶、即時搜尋與上方的「附檔內容」（若有）回答。\n**讀本機檔**：你可以讀取使用者電腦上的檔案。當使用者輸入「讀 路徑」或「讀取 路徑」（例如：讀 /home/xxx/file.pdf），系統會將該檔內容注入「附檔內容」供你回答。若使用者問「你能讀取我電腦的檔案嗎」「看得到我電腦的檔案嗎」等，請明確回答「可以，請輸入「讀」或「讀取」加上本機路徑，例如：讀 /path/to/file」勿回答「不可以」或「沒有此能力」。\n讀網頁、寫檔、搜尋：同上，依系統注入的附檔與即時區塊回答。若問「你能做什麼」請簡短說明讀檔（讀/讀取+路徑）、讀網頁、寫檔、搜尋即可，勿輸出「正在聯網」「正在思考」等語句。回覆要點：僅依「即時」與「附檔內容」區塊回答，沒寫到的不要猜。簡短對談、不列點。若使用者只發「？？」「蛤」「啥」等極短句，簡短確認即可，勿反嗆。"

		recentMu.Lock()
		turns := make([]struct{ User, Assistant string }, len(recentTurns))
		copy(turns, recentTurns)
		recentMu.Unlock()
		messages := []llm.Message{{Role: "system", Content: systemPrompt}}
		for _, t := range turns {
			messages = append(messages, llm.Message{Role: "user", Content: t.User}, llm.Message{Role: "assistant", Content: t.Assistant})
		}
		messages = append(messages, llm.Message{Role: "user", Content: userText})
		flusher, _ := w.(http.Flusher)
		fullResponse, err := chatProvider.ChatStream(messages, func(chunk string) {
			writeSSEText(w, chunk)
			if flusher != nil {
				flusher.Flush()
			}
		})
		if err != nil {
			log.Printf("[chat] stream error: %v", err)
			writeSSEErr(w, err.Error())
			return
		}
		if fullResponse == "" && cfg.Provider == "gemini" {
			writeSSEErr(w, "Gemini 回傳空內容，請換一則訊息或改用其他模型。")
			return
		}
		writeSSE(w, "data: [DONE]\n\n")
		recentMu.Lock()
		pair := struct{ User, Assistant string }{userText, fullResponse}
		recentTurns = append(recentTurns, pair)
		if len(recentTurns) > maxRecentTurns {
			recentTurns = recentTurns[len(recentTurns)-maxRecentTurns:]
		}
		condenseBuffer = append(condenseBuffer, pair)
		var toCondense []struct{ User, Assistant string }
		if len(condenseBuffer) >= condenseRounds {
			toCondense = make([]struct{ User, Assistant string }, condenseRounds)
			copy(toCondense, condenseBuffer[len(condenseBuffer)-condenseRounds:])
			condenseBuffer = nil
		}
		recentMu.Unlock()
		appendConversationLog(memoryDir, userText, fullResponse)
		if len(toCondense) == condenseRounds {
			go condense20RoundsAndPush(cfg, chatProvider, toCondense, tiers, backstory)
		}
	}
	handleModel := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"model": cfg.Model})
	}
	http.HandleFunc("/chatmery/api/chat", handleChat)
	http.HandleFunc("/chatmery/api/model", handleModel)
	http.HandleFunc("/api/chat", handleChat)
	http.HandleFunc("/api/model", handleModel)

	// 當 Cloudflare Tunnel 依 path 轉發時會剝掉前綴，請求會以 / 送達；根路徑也提供首頁與靜態檔
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" || p == "" || p == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
			http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})

	port := os.Getenv("CHATMERY_WEB_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = defaultWebPort
	}
	log.Printf("Chatmery Web 已啟動（系統重啟），監聽 :%s (base path /chatmery)", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func writeSSE(w http.ResponseWriter, s string) {
	_, _ = w.Write([]byte(s))
}

func writeSSEText(w http.ResponseWriter, text string) {
	escaped := strings.ReplaceAll(text, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\\r")
	writeSSE(w, "data: {\"text\":\""+escaped+"\"}\n\n")
}

func writeSSEErr(w http.ResponseWriter, errMsg string) {
	writeSSEText(w, "錯誤："+errMsg)
}

func appendConversationLog(memoryDir, user, assistant string) {
	path := filepath.Join(memoryDir, "conversation.jsonl")
	entry := struct {
		User      string `json:"user"`
		Assistant string `json:"assistant"`
		Ts        string `json:"ts"`
	}{User: user, Assistant: assistant, Ts: time.Now().UTC().Format(time.RFC3339)}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	f.Close()
}

func hasSearchIntent(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func asksForDateOrTime(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	dateTimeKeywords := []string{
		"現在日期", "現在時間", "今天日期", "今天時間", "今天幾號", "現在幾點",
		"幾點", "幾號", "告訴我現在", "現在幾", "今天幾", "當前日期", "當前時間",
		"日期和", "時間和", "日期與", "時間與", "說一下今天", "說一下現在", "說一下日期", "說一下時間",
	}
	for _, k := range dateTimeKeywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return strings.Contains(lower, "日期") && strings.Contains(lower, "時間")
}

func parseReadURLIntent(text string) (url, rest string, ok bool) {
	s := strings.TrimSpace(text)
	u := document.FindFirstURL(s)
	if u == "" {
		return "", "", false
	}
	lower := strings.ToLower(s)
	keywords := []string{"讀取網頁", "讀取網址", "讀取此網址", "讀取連結", "讀網頁", "讀網址", "讀連結"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			rest = strings.TrimSpace(strings.Replace(s, u, " ", 1))
			return u, rest, true
		}
	}
	without := strings.TrimSpace(strings.Replace(s, u, " ", 1))
	if utf8.RuneCountInString(without) <= 30 {
		rest = without
		return u, rest, true
	}
	return "", "", false
}

// parseReadLocalFileIntent 解析「讀 路徑」或「讀取 路徑」，支援 ~ 與引號包住含空格路徑；回傳展開後的本機路徑與剩餘文字。
func parseReadLocalFileIntent(text string) (localPath, rest string, ok bool) {
	s := strings.TrimSpace(text)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "讀取 ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "讀取 "))
	} else if strings.HasPrefix(lower, "讀 ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "讀 "))
	} else {
		return "", "", false
	}
	if s == "" {
		return "", "", false
	}
	var path string
	if strings.HasPrefix(s, "\"") {
		end := strings.Index(s[1:], "\"")
		if end < 0 {
			return "", "", false
		}
		path = s[1 : 1+end]
		rest = strings.TrimSpace(s[1+end+1:])
	} else {
		path = s
		rest = ""
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", false
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", false
		}
		if path == "~" {
			path = home
		} else if path == "~/" || strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		} else {
			path = filepath.Join(home, path[1:])
		}
	}
	path = filepath.Clean(path)
	return path, rest, true
}

// condense20RoundsAndPush 將 20 輪當前對話解構濃縮成 1 句，送入短期；若短期滿則觸發後續濃縮鏈。
func condense20RoundsAndPush(cfg *config.Config, provider llm.Provider, twentyRounds []struct{ User, Assistant string }, tiers *memory.Tiers, backstory *memory.Backstory) {
	var buf strings.Builder
	for i, p := range twentyRounds {
		buf.WriteString("使用者：")
		buf.WriteString(p.User)
		buf.WriteString("\n助手：")
		buf.WriteString(p.Assistant)
		if i < len(twentyRounds)-1 {
			buf.WriteString("\n\n")
		}
	}
	prompt := "以下是最近 20 輪對話（使用者與助手交替）。請解構後濃縮成「一句」關鍵事實或偏好，不准廢話、不准重複對話內容。產出恰好一句。\n\n" + buf.String()
	one, err := provider.Generate(prompt)
	if err != nil {
		log.Printf("condense 20→1: %v", err)
		return
	}
	one = strings.TrimSpace(strings.Trim(strings.TrimSpace(one), "「」"))
	if len(one) < 3 || strings.Contains(one, "對話") {
		return
	}
	if cfg.SnippetMaxRunes > 0 && utf8.RuneCountInString(one) > cfg.SnippetMaxRunes {
		n := 0
		for i := range one {
			if n >= cfg.SnippetMaxRunes {
				one = one[:i] + "…"
				break
			}
			n++
		}
	}
	tiers.SetCurrentFact(one)
	needShort, shortBatch := tiers.AddToShortTerm(one)
	if needShort && len(shortBatch) > 0 {
		condensed := runCondenseShortToLong(provider, shortBatch, cfg.ShortCondenseTo, cfg.SnippetMaxRunes)
		if len(condensed) > 0 {
			needLong, longBatch := tiers.AppendLongTerm(condensed)
			if needLong && len(longBatch) > 0 {
				condensedLong := runCondenseLongToCore(provider, longBatch, cfg.LongCondenseTo, cfg.SnippetMaxRunes)
				if len(condensedLong) > 0 {
					needCore, coreBatch := tiers.AppendCore(condensedLong)
					if needCore && len(coreBatch) > 0 {
						soulOne := runCondenseCoreToSoul(provider, coreBatch, cfg.SnippetMaxRunes)
						if soulOne != "" {
							runMergeSoul(provider, cfg.SoulPath, soulOne, backstory)
						}
					}
				}
			}
		}
	}
}

func runCondenseShortToLong(provider llm.Provider, batch []string, to int, maxRunes int) []string {
	if to <= 0 {
		to = 50
	}
	prompt := strings.Join(batch, "\n")
	if utf8.RuneCountInString(prompt) > 12000 {
		prompt = prompt[:3000] + "\n...\n" + prompt[utf8.RuneCountInString(prompt)-3000:]
	}
	msg := "請解構後濃縮成 " + strconv.Itoa(to) + " 句，每句一句話、保留重要資訊。產出恰好 " + strconv.Itoa(to) + " 行。\n\n" + prompt
	out, err := provider.Generate(msg)
	if err != nil {
		log.Printf("condense short→long: %v", err)
		return nil
	}
	return parseCondenseLines(out, to, maxRunes)
}

func runCondenseLongToCore(provider llm.Provider, batch []string, to int, maxRunes int) []string {
	if to <= 0 {
		to = 5
	}
	msg := "請解構後濃縮成 " + strconv.Itoa(to) + " 句，每句一句話、保留核心。產出恰好 " + strconv.Itoa(to) + " 行。\n\n" + strings.Join(batch, "\n")
	out, err := provider.Generate(msg)
	if err != nil {
		log.Printf("condense long→core: %v", err)
		return nil
	}
	return parseCondenseLines(out, to, maxRunes)
}

func runCondenseCoreToSoul(provider llm.Provider, batch []string, maxRunes int) string {
	msg := "以下是核心記憶（每行一條）。請解構後濃縮成 1 句，這一句將融入靈魂。產出恰好 1 行。\n\n" + strings.Join(batch, "\n")
	out, err := provider.Generate(msg)
	if err != nil {
		log.Printf("condense core→soul: %v", err)
		return ""
	}
	out = strings.TrimSpace(out)
	if maxRunes > 0 && utf8.RuneCountInString(out) > maxRunes {
		n := 0
		for i := range out {
			if n >= maxRunes {
				return out[:i] + "…"
			}
			n++
		}
	}
	return out
}

func runMergeSoul(provider llm.Provider, soulPath, one string, backstory *memory.Backstory) {
	soul := backstory.GetSoul()
	msg := "現有靈魂內容：\n" + soul + "\n\n新融入的一句：\n" + one + "\n\n請將上述合併成一個完整的靈魂敘事（一句到一檔都是我），保留並融合新資訊，產出更新後的靈魂全文。"
	out, err := provider.Generate(msg)
	if err != nil {
		log.Printf("merge soul: %v", err)
		return
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return
	}
	if err := os.WriteFile(soulPath, []byte(out), 0644); err != nil {
		log.Printf("write soul: %v", err)
	}
}

func parseCondenseLines(out string, want int, maxRunes int) []string {
	out = strings.TrimSpace(out)
	lines := strings.Split(out, "\n")
	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "無" {
			continue
		}
		if maxRunes > 0 && utf8.RuneCountInString(line) > maxRunes {
			n := 0
			for i := range line {
				if n >= maxRunes {
					line = line[:i] + "…"
					break
				}
				n++
			}
		}
		result = append(result, line)
		if len(result) >= want {
			break
		}
	}
	return result
}
