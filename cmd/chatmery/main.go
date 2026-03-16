// Chatmery（chat + memory）：對話外殼（Telegram + Ollama）+ 記憶系統（背版 + archival）。
// 架構：memory 與 search 可獨立抽換，之後可接回遊戲或給 OpenClaw 呼叫。
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/tanger/chatmery/internal/config"
	"github.com/tanger/chatmery/internal/document"
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
	archival := memory.NewArchival(cfg.ArchivalPath, cfg.RefineRolloverLimit)
	searchBackend := &search.Backend{
		Kind:         cfg.SearchBackend,
		BraveAPIKey:  cfg.BraveAPIKey,
		TavilyAPIKey: cfg.TavilyAPIKey,
	}
	ollamaClient := ollama.NewClient(cfg.OllamaURL, cfg.Model)

	// 啟動時若有上次對話的 chat，發「系統已重啟」（後台重啟完成後使用者可在 Telegram 看到）
	lastChatPath := filepath.Join(cfg.Workspace, ".chatmery_last_chat_id")
	if b, err := os.ReadFile(lastChatPath); err == nil {
		if id, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
			_, _ = bot.Send(tgbotapi.NewMessage(id, "系統已重啟"))
		}
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil {
			continue
		}
		chatID := update.Message.Chat.ID
		var userText string
		var docContext string
		if update.Message.Document != nil {
			doc := update.Message.Document
			userText = strings.TrimSpace(update.Message.Caption)
			if userText == "" {
				_, _ = bot.Send(tgbotapi.NewMessage(chatID, "請在附檔時加一句說明（例如：請總結、這段在講什麼）。"))
				continue
			}
			log.Printf("msg from %d: [附檔] %s", update.Message.From.ID, doc.FileName)
			localPath, err := downloadTelegramFile(bot, doc.FileID, doc.FileName)
			if err != nil {
				log.Printf("download file: %v", err)
				_, _ = bot.Send(tgbotapi.NewMessage(chatID, "無法下載附檔，請重試。"))
				continue
			}
			defer os.Remove(localPath)
			extracted, err := document.ExtractText(localPath)
			_ = os.Remove(localPath)
			if err != nil || extracted == "" {
				log.Printf("extract file: %v", err)
				_, _ = bot.Send(tgbotapi.NewMessage(chatID, "目前僅支援 PDF 與純文字檔（.txt, .md），且需可擷取文字。"))
				continue
			}
			docContext = "## 附檔內容\n" + extracted + "\n\n"
		} else if update.Message.Text != "" {
			userText = update.Message.Text
			log.Printf("msg from %d: %s", update.Message.From.ID, trunc(userText, 40))
			// 寫入意圖：「寫 path 內容」或「寫入 path 內容」（僅限工作區內）
			if path, content, ok := parseWriteFileIntent(userText); ok {
				absPath, err := resolveWritePath(cfg.Workspace, path)
				if err != nil {
					log.Printf("write path: %v", err)
					_, _ = bot.Send(tgbotapi.NewMessage(chatID, "寫入失敗（路徑須在工作區內）："+path))
					continue
				}
				if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
					log.Printf("write mkdir: %v", err)
					_, _ = bot.Send(tgbotapi.NewMessage(chatID, "寫入失敗：無法建立目錄。"))
					continue
				}
				if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
					log.Printf("write file: %v", err)
					_, _ = bot.Send(tgbotapi.NewMessage(chatID, "寫入失敗："+err.Error()))
					continue
				}
				_, _ = bot.Send(tgbotapi.NewMessage(chatID, "已寫入 "+path))
				continue
			}
			// 讀檔意圖：「讀 /path」或「讀取 /path」（不鎖定工作區，任意可讀路徑）
			if path, rest, ok := parseReadFileIntent(userText); ok {
				absPath, err := resolveReadPath(path)
				if err != nil {
					log.Printf("read file path: %v", err)
					_, _ = bot.Send(tgbotapi.NewMessage(chatID, "無法解析路徑或檔案不存在："+path))
					continue
				}
				extracted, err := document.ExtractText(absPath)
				if err != nil || extracted == "" {
					log.Printf("read file extract: %v", err)
					_, _ = bot.Send(tgbotapi.NewMessage(chatID, "無法讀取該檔案（路徑不存在或格式僅支援 PDF / .txt / .md）。"))
					continue
				}
				docContext = "## 附檔內容\n" + extracted + "\n\n"
				if strings.TrimSpace(rest) != "" {
					userText = strings.TrimSpace(rest)
				} else {
					userText = "請根據上述檔案內容回答。"
				}
			}
		} else {
			continue
		}

		// 記住最後對話的 chat，供下次啟動時發「系統已重啟」
		_ = os.WriteFile(lastChatPath, []byte(strconv.FormatInt(chatID, 10)), 0600)

		// /restart：觸發 systemd 重啟，回覆後由 systemd 殺掉本 process 再起新行程
		if isRestartCommand(userText) {
			_, _ = bot.Send(tgbotapi.NewMessage(chatID, "已重新啟動"))
			go func() {
				time.Sleep(300 * time.Millisecond)
				cmd := exec.Command("systemctl", "--user", "restart", "chatmery")
				if err := cmd.Run(); err != nil {
					log.Printf("restart: systemctl --user restart chatmery 失敗（若未用 systemd 管理則屬正常）: %v", err)
				}
			}()
			continue
		}

		// 當前日期與時刻（先算好：純問時間時直接回、網搜帶日期、system 注入）
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
		directTimeReply := naturalDate + "，現在 " + strconv.Itoa(hour) + " 點 " + strconv.Itoa(min) + " 分。"

		// 只問日期／時間 → 後端直接回，不經 LLM（提示辭靠不住，改由程式保證精準）
		if asksForDateOrTime(userText) && utf8.RuneCountInString(strings.TrimSpace(userText)) <= 60 {
			_, _ = bot.Send(tgbotapi.NewMessage(chatID, directTimeReply))
			continue
		}

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

		// 1) 是否觸發網搜（查詢帶入當前日期；有搜尋時主動告知 LLM 現在日期與時間，解讀「最新」「今天」等才準）
		var webLines []string
		didSearch := false
		if hasSearchIntent(userText, cfg.SearchKeywords) {
			didSearch = true
			q := search.BuildQuery(userText)
			if q != "" {
				q = dateStr + " " + q
			} else {
				q = dateStr
			}
			results := searchBackend.Search(q, cfg.WebSearchMaxResults)
			for _, r := range results {
				webLines = append(webLines, "- [搜尋] "+capRune(r))
			}
			if len(results) == 0 {
				log.Printf("[search] 已觸發網搜但無結果，query=%q（請確認 .env 已設 BRAVE_API_KEY 或 TAVILY）", q)
			}
		}
		var webContext string
		if len(webLines) > 0 {
			nowHint := "本則對話的當前時間：" + naturalDate + " " + strconv.Itoa(hour) + "點" + strconv.Itoa(min) + "分。解讀搜尋結果中的「最新」「今天」等請依此。\n"
			webContext = nowHint + strings.Join(webLines, "\n")
		} else if didSearch {
			webContext = "（本次網搜無結果，請簡短回覆「沒有搜尋到即時結果」即可，勿稱資料庫或內部資料。）"
		} else {
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
		if len(longTerm) > 0 || len(sessionHits) > 0 {
			log.Printf("[memory] 注入 長期=%d 當前=%d", len(longTerm), len(sessionHits))
		}

		// 3) 背版（SOUL / MEMORY 建議保持簡短，見 README）
		soul := backstory.GetSoul()
		summary := backstory.GetMemory()
		zoneName, _ := now.Zone()
		zoneLine := "時區: " + zoneName
		if cfg.Timezone != "" {
			zoneLine += " (" + cfg.Timezone + ")"
		}
		zoneLine += "\n"
		timeStr := now.Format("15:04")
		nowSentence := "本則對話的當前時間：" + naturalDate + " " + strconv.Itoa(hour) + "點" + strconv.Itoa(min) + "分。\n"
		timeBlock := "## 當前日期與時間\n" + nowSentence +
			zoneLine + "當前日期: " + dateStr + "\n當前時刻: " + timeStr + "（24小時制）\n"
		systemPrompt := soul + "\n\n" + timeBlock + "## 記憶\n" + memoryContext +
			"\n\n## 即時\n" + webContext +
			"\n\n" + docContext +
			"## 背景\n" + summary +
			"\n\n## 能力邊界\n你可依本則對話、記憶、即時搜尋與上方的「附檔內容」（若有）回答。使用者以「讀 /path」或「讀取 /path」可讀本機檔並放入「附檔內容」；以「寫 path 內容」或「寫入 path 內容」可將文字寫入工作區內檔案。若使用者問能力問題，請直接說明。\n\n回覆要點：僅依「即時」與「附檔內容」區塊回答，沒寫到的不要猜、不要延伸。若搜尋結果與使用者問題明顯不符或不足以回答，請直接說「搜尋結果與問題不太相符」或「目前沒有找到足夠相關資訊」，勿編造。簡短對談、不列點。"

		// 4) 先回「八百里加急吐字中🐎」
		placeholder, err := bot.Send(tgbotapi.NewMessage(chatID, "八百里加急吐字中🐎"))
		if err != nil {
			log.Printf("send placeholder: %v", err)
			continue
		}

		// 5) Ollama 串流，邊收邊編輯（日期／時間已改為純問時由後端直接回，此處不再靠提示辭）
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

func isRestartCommand(text string) bool {
	s := strings.TrimSpace(strings.ToLower(text))
	return s == "/restart" || strings.HasPrefix(s, "/restart@")
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

// asksForDateOrTime 判斷是否在問現在日期或時間；是則由後端直接回覆，不經 LLM
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

// parseWriteFileIntent 辨識「寫 path 內容」或「寫入 path 內容」；路徑可引號包住。回傳 path、要寫入的內容、是否為寫入意圖。
func parseWriteFileIntent(text string) (path, content string, ok bool) {
	s := strings.TrimSpace(text)
	for _, prefix := range []string{"寫入 ", "寫 "} {
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		after := strings.TrimSpace(s[len(prefix):])
		if after == "" {
			return "", "", false
		}
		var rest string
		if strings.HasPrefix(after, "\"") {
			i := strings.Index(after[1:], "\"")
			if i < 0 {
				return "", "", false
			}
			path = strings.TrimSpace(after[1 : 1+i])
			rest = strings.TrimSpace(after[1+i+1:])
		} else if strings.HasPrefix(after, "'") {
			i := strings.Index(after[1:], "'")
			if i < 0 {
				return "", "", false
			}
			path = strings.TrimSpace(after[1 : 1+i])
			rest = strings.TrimSpace(after[1+i+1:])
		} else {
			idx := strings.Index(after, " ")
			if idx <= 0 {
				return "", "", false
			}
			path = strings.TrimSpace(after[:idx])
			rest = strings.TrimSpace(after[idx+1:])
		}
		if path == "" || rest == "" {
			return "", "", false
		}
		return path, rest, true
	}
	return "", "", false
}

// resolveWritePath 將 path 解析為工作區內的絕對路徑，禁止 path traversal。
func resolveWritePath(workspace, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	// 禁止往上層
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path must be under workspace")
	}
	workspace = filepath.Clean(workspace)
	abs := filepath.Clean(filepath.Join(workspace, path))
	if workspace != abs && !strings.HasPrefix(abs, workspace+string(filepath.Separator)) {
		return "", fmt.Errorf("path must be under workspace")
	}
	return abs, nil
}

// parseReadFileIntent 辨識「讀 /path」或「讀取 /path」；可選引號包住路徑。回傳 path、剩餘文字、是否為讀檔意圖。
func parseReadFileIntent(text string) (path, rest string, ok bool) {
	s := strings.TrimSpace(text)
	for _, prefix := range []string{"讀取 ", "讀 "} {
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		after := strings.TrimSpace(s[len(prefix):])
		if after == "" {
			return "", "", false
		}
		if strings.HasPrefix(after, "\"") {
			i := strings.Index(after[1:], "\"")
			if i < 0 {
				return "", "", false
			}
			path = strings.TrimSpace(after[1 : 1+i])
			rest = strings.TrimSpace(after[1+i+1:])
		} else if strings.HasPrefix(after, "'") {
			i := strings.Index(after[1:], "'")
			if i < 0 {
				return "", "", false
			}
			path = strings.TrimSpace(after[1 : 1+i])
			rest = strings.TrimSpace(after[1+i+1:])
		} else {
			path = after
			rest = ""
		}
		return path, rest, true
	}
	return "", "", false
}

// resolveReadPath 解析為絕對路徑並確認檔案存在且為一般檔案（不鎖定工作區）。
func resolveReadPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	// 支援 ~ 為使用者目錄
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("is a directory")
	}
	return abs, nil
}

// downloadTelegramFile 依 FileID 下載 Telegram 檔案到暫存檔，回傳本機路徑；呼叫端需自行 os.Remove。
func downloadTelegramFile(bot *tgbotapi.BotAPI, fileID, fileName string) (string, error) {
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return "", err
	}
	url := file.Link(bot.Token)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}
	safeName := filepath.Base(fileName)
	if safeName == "" || safeName == "." {
		safeName = "attachment"
	}
	ext := filepath.Ext(safeName)
	tmp, err := os.CreateTemp("", "chatmery-*"+ext)
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
