// Chatmery（chat + memory）：對話外殼（Telegram + Ollama）+ 記憶系統（背版 + archival）。
// 架構：memory 與 search 可獨立抽換，之後可接回遊戲或給 OpenClaw 呼叫。
package main

import (
	"log"
	"strings"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/tanger/chatmery/internal/config"
	"github.com/tanger/chatmery/internal/memory"
	"github.com/tanger/chatmery/internal/ollama"
	"github.com/tanger/chatmery/internal/search"
)

func main() {
	cfg := config.Load()
	if cfg.Token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}
	bot.Debug = false
	log.Printf("Chatmery 啟動 - WORKSPACE=%s SEARCH=%s", cfg.Workspace, cfg.SearchBackend)

	backstory := memory.NewBackstory(cfg.SoulPath, cfg.MemoryPath)
	archival := memory.NewArchival(cfg.ArchivalPath, 20)
	searchBackend := &search.Backend{
		Kind:         cfg.SearchBackend,
		BraveAPIKey:  cfg.BraveAPIKey,
		TavilyAPIKey: cfg.TavilyAPIKey,
	}
	ollamaClient := ollama.NewClient(cfg.OllamaURL, cfg.Model)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil || update.Message.Text == "" {
			continue
		}
		userText := update.Message.Text
		chatID := update.Message.Chat.ID
		log.Printf("msg from %d: %s", update.Message.From.ID, trunc(userText, 40))

		// 小模型優化：注入條數與每條長度由 config 控制，預設輕量
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

		// 1) 是否觸發網搜
		var webLines []string
		if hasSearchIntent(userText, cfg.SearchKeywords) {
			q := search.BuildQuery(userText)
			results := searchBackend.Search(q, cfg.WebSearchMaxResults)
			for _, r := range results {
				webLines = append(webLines, "- [搜尋] "+capRune(r))
			}
		}
		webContext := strings.Join(webLines, "\n")
		if webContext == "" {
			webContext = "(無)"
		}

		// 2) 記憶檢索
		longTerm := archival.SearchPool(userText, cfg.MemoryLongTermK)
		sessionHits := archival.SessionHits(userText, cfg.MemorySessionK)
		var memLines []string
		for _, s := range longTerm {
			memLines = append(memLines, "- [長期] "+capRune(s))
		}
		for _, s := range sessionHits {
			memLines = append(memLines, "- [當前] "+capRune(s))
		}
		memoryContext := strings.Join(memLines, "\n")
		if memoryContext == "" {
			memoryContext = "(無)"
		}

		// 3) 背版（SOUL / MEMORY 建議保持簡短，見 README）
		soul := backstory.GetSoul()
		summary := backstory.GetMemory()
		nowStr := time.Now().Format("2006-01-02 15:04:05 MST")
		// 指令精簡，減少 system token；注入當前日期時間讓模型能正確回答「今天幾號」「現在幾點」
		systemPrompt := soul + "\n\n## 當前時間\n" + nowStr +
			"\n\n## 記憶\n" + memoryContext +
			"\n\n## 即時\n" + webContext +
			"\n\n## 背景\n" + summary +
			"\n\n回覆要點：依記憶與即時資訊簡短回答；搜尋無關或為空就說沒找到、不猜不長篇。像人類簡短對談、不列點。"

		// 4) 先回「正在聯網並思考...」
		placeholder, err := bot.Send(tgbotapi.NewMessage(chatID, "正在聯網並思考..."))
		if err != nil {
			log.Printf("send placeholder: %v", err)
			continue
		}

		// 5) Ollama 串流，邊收邊編輯（不傳對話歷史：記憶靠「抽出的要旨」注入 system，不靠最近 N 輪原文）
		messages := []ollama.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userText},
		}
		var fullResponse string
		editMsg := tgbotapi.NewEditMessageText(chatID, placeholder.MessageID, "")
		fullResponse, err = ollamaClient.ChatStream(messages, func(chunk string) {
			fullResponse += chunk
			if len(fullResponse)%30 < len(chunk) {
				txt := fullResponse + "..."
				if len(txt) > 4000 {
					txt = txt[:3997] + "..."
				}
				editMsg.Text = txt
				_, _ = bot.Request(editMsg)
			}
		})
		if err != nil {
			log.Printf("ollama: %v", err)
			editMsg.Text = "錯誤: " + err.Error()
			_, _ = bot.Request(editMsg)
			continue
		}
		editMsg.Text = fullResponse
		if len(editMsg.Text) > 4000 {
			editMsg.Text = editMsg.Text[:3997] + "..."
		}
		if _, err := bot.Request(editMsg); err != nil {
			_, _ = bot.Send(tgbotapi.NewMessage(chatID, fullResponse))
		}

		// 6) 背景：deconstruct 抽事實寫入 session；若觸發提煉則 C+B+A 一次寫入長期
		go deconstructAndSave(cfg, ollamaClient, userText, fullResponse, archival)
	}
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

func deconstructAndSave(cfg *config.Config, client *ollama.Client, userMsg, aiReply string, archival *memory.Archival) {
	prompt := "請從以下對話中提取出一句「關鍵事實」或「使用者偏好」，不准有廢話、不准重複對話。\n使用者：" + userMsg + "\n助手：" + aiReply
	fact, err := client.Generate(prompt)
	if err != nil {
		log.Printf("deconstruct: %v", err)
		return
	}
	fact = strings.TrimSpace(strings.Trim(strings.TrimSpace(fact), "「」"))
	if len(fact) < 5 || strings.Contains(fact, "對話") {
		return
	}
	log.Printf("Deconstructed: %s", trunc(fact, 60))
	batch := archival.AddSessionFact(fact, cfg.RefineBatchMaxItems, cfg.RefineMaxRunesPerItem)
	if len(batch) > 0 {
		runRefine(client, batch, archival)
	}
}

// runRefine 提煉：C+B+A 一次 Generate，產出 1～3 條寫入長期；失敗則 fallback 最舊一筆直接 Insert。
func runRefine(client *ollama.Client, batch []string, archival *memory.Archival) {
	const refinePromptPrefix = `以下是近期對話抽出的要旨（每行一條）。請做三件事：(1) 篩選：只保留值得長期記住的；(2) 合併相似：同主題或同類型合併成一條；(3) 摘要：若多條在講同一件事，用一句話總結。產出 1～3 條人物背版事實，每條一句話、不重複、不廢話。若都無長期價值就回傳「無」。偏好：使用者偏好、重要決定、與使用者的關係或承諾。

`
	prompt := refinePromptPrefix + strings.Join(batch, "\n")
	out, err := client.Generate(prompt)
	if err != nil {
		log.Printf("refine: %v, fallback insert oldest", err)
		if len(batch) > 0 {
			_ = archival.Insert(batch[0], "event")
		}
		return
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.TrimSpace(out) == "無" {
		return
	}
	lines := strings.Split(out, "\n")
	var inserted int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "無" {
			continue
		}
		if utf8.RuneCountInString(line) < 5 {
			continue
		}
		if err := archival.Insert(line, "refined"); err != nil {
			log.Printf("refine insert: %v", err)
			continue
		}
		inserted++
		log.Printf("Refined → long-term: %s", trunc(line, 50))
		if inserted >= 3 {
			break
		}
	}
	if inserted == 0 && len(batch) > 0 {
		_ = archival.Insert(batch[0], "event")
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
