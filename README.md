# Chatmery

專案名 = **chat** + **memory** 合併（非「聊天室」；若丟 Google 翻譯會誤翻成聊天室，此為自造專案名）。  
對話外殼（Web）+ 記憶系統（背版 + archival），Go 實作。**預設專為小模型設計（約 4B 參數等級）**：輕量、迅速、不過度消耗 context，適合本地 4B 左右模型日常對談。記憶不靠「MD 對話紀錄 + 硬式檢索」（那樣使用者會等了又等），改為「抽出的要旨 → 少數條目注入」；可獨立運行，之後可接回遊戲或給 OpenClaw 當記憶後端。

**對話殼優先級**：上網檢索的能力與精準度最重要（模型知識截止於訓練日，必須能查新訊息）；其次為寫文檔、紀錄討論。見 [對話殼—OpenClaw 讀寫本地檔案與文檔](doc/對話殼—OpenClaw%20讀寫本地檔案與文檔.md)。

## 架構

- **internal/config** — 環境變數與路徑（WORKSPACE、搜尋後端、模型與 API、四池容量與答覆注入量）
- **internal/memory** — 背版（SOUL.md、MEMORY.md）、四池一魂（短期／長期／核心／當前 + 靈魂）、關鍵字檢索與持久化（見 [記憶階層—四池一魂](doc/記憶階層—四池一魂提案.md)）
- **internal/search** — 搜尋關鍵字萃取、Brave / Tavily API（Go 版未實作 DuckDuckGo）
- **internal/ollama** — Ollama chat 串流與 generate
- **cmd/chatmery-web** — Web 殼入口：近期對話上下文（最近 10 輪）、組 prompt（靈魂 + 時間 + 記憶 + 即時 + 附檔 + 背景）、呼叫記憶與搜尋、提供 /chatmery 靜態與 API（SSE）

## 敏感資訊：獨檔管理、不寫進程式碼

API key **一律用環境變數**，且**只寫在 `.env`**（已加入 .gitignore，不會被 push）；其餘設定（模型、後端、端點、記憶條數等）請寫在工作區 **chatmery.tuning**，見 `chatmery.tuning.example`。

```bash
cp .env.example .env
# 編輯 .env 僅填入金鑰：BRAVE_API_KEY / TAVILY_API_KEY、OPENAI_API_KEY / GEMINI_API_KEY / OPENROUTER_API_KEY（依使用之後端）
cp chatmery.tuning.example chatmery.tuning
# 編輯 chatmery.tuning 設定 CHATMERY_PROVIDER、CHATMERY_MODEL、CHATMERY_SEARCH 等
```

程式碼與 repo 內**禁止**寫入真實 token/key；`.env.example` 僅列出金鑰欄位，可提交。

## 設定說明

**`.env`（僅金鑰）**：`BRAVE_API_KEY` / `TAVILY_API_KEY`、`OPENAI_API_KEY`、`GEMINI_API_KEY`、`OPENROUTER_API_KEY`（依使用的後端擇填）。

**`chatmery.tuning`（對照 chatmery.tuning.example）**：模型、聊天/搜尋/embed 後端、端點 URL、記憶條數、提煉門檻、時區、placeholder 等；環境變數可覆寫此檔。

| 變數（tuning 或 env） | 說明 |
|------|------|
| `CHATMERY_PROVIDER` | 聊天後端：`ollama`（預設）、`openai`、`gemini`、`openrouter` |
| `CHATMERY_MODEL` | 模型名稱（ollama 本機；openai 如 gpt-4o-mini；gemini 如 gemini-2.5-flash；openrouter 如 google/gemini-2.0-flash，見 [OpenRouter 模型](https://openrouter.ai/models)） |
| `OLLAMA_HOST` | Ollama URL（provider=ollama 時用） |
| `OPENAI_BASE_URL` | OpenAI 端點（provider=openai 時；預設 `https://api.openai.com/v1`） |
| `GEMINI_BASE_URL` | Gemini 端點（provider=gemini 時）；若 404 請改用 [Gemini 模型文件](https://ai.google.dev/gemini-api/docs/models/gemini) 所列型號 |
| `OPENROUTER_BASE_URL` | OpenRouter 端點（provider=openrouter 時；預設 `https://openrouter.ai/api/v1`） |
| `CHATMERY_SEARCH` | 網搜後端：`brave` 或 `tavily` |
| `CHATMERY_MEMORY_LONG_K` / `CHATMERY_MEMORY_SESSION_K` / `CHATMERY_MEMORY_CORE_K` / `CHATMERY_WEB_SEARCH_MAX` / `CHATMERY_SNIPPET_MAX` | 答覆時記憶注入量（結構：當前1+靈魂1+核心2+長期3+短期5） |
| `CHATMERY_SHORT_TERM_CAP` / `CHATMERY_SHORT_CONDENSE_TO` 等 | 四池容量與濃縮條數（見 chatmery.tuning 註解） |
| `CHATMERY_EMBED_MODEL` / `CHATMERY_EMBED_PROVIDER` / `CHATMERY_EMBED_URL` | 向量檢索（目前四池為關鍵字檢索；可擴充） |
| `CHATMERY_TZ` | 時區（IANA）；`CHATMERY_PLACEHOLDER` 等候回覆文案 |

Workspace 下需有 **SOUL.md**（可選，無則用預設）、**MEMORY.md**（可選）。`memory/` 目錄會自動建立，內含四池：`short_term.jsonl`、`long_term.jsonl`、`core.jsonl`、`current.txt`（靈魂寫入 SOUL.md）。

**對話上下文**：每次請求會帶上**最近 10 輪**使用者與助手的對話，模型依此接話，避免雞同鴨講。答覆時注入的記憶結構為：當前 1 + 靈魂 1 + 核心 2 + 長期 3 + 短期 5。

### 小模型使用建議（預設約 4B 等級）

- 保持 **SOUL.md**、**MEMORY.md** 簡短（各約數十字～百字），避免塞滿 context。
- 預設注入量針對 4B 左右模型（長期 3、短期 5、核心 2、網搜 5、每條最多 120 字）；更大模型可調大上述環境變數。
- 預設模型為 `qwen-4b-slim:latest`；Ollama 端可設較小 `num_ctx`（如 2048）以加快推理。

## 一鍵上線（Web 殼）

在專案根目錄，需有 `web/`、`chatmery.tuning`、`.env`（可選，僅搜尋/雲端後端需 key）：

```bash
chmod +x start-web
./start-web
```

瀏覽器開 `http://localhost:1721/chatmery/`。若要對外 **https://sw.ygggt.com/chatmery**，在 **Cloudflare Tunnel** 的 `config.yml` 的 `ingress` 裡加一筆（路徑優先於根路徑）。Chatmery Web 預設同埠 **1721**；若與奇點同機跑兩個 process，請設 `CHATMERY_WEB_PORT=1722` 讓 Chatmery 用 1722，或改由奇點掛 `/chatmery` 只開 1721。

```yaml
ingress:
  - hostname: sw.ygggt.com
    path: /chatmery
    service: http://localhost:1721   # Chatmery Web（與奇點同埠時改由奇點掛 /chatmery 或 Chatmery 用 1722）
  - hostname: sw.ygggt.com
    service: http://localhost:1721   # 奇點
```

存檔後重啟 `cloudflared`，即可用 https://sw.ygggt.com/chatmery/ 上線。

## 讀檔／寫檔／附檔（API 與 UI）

- **讀本機檔**：
  - 輸入「讀 *路徑*」或「讀取 *路徑*」可將該檔內容注入當輪對話；路徑**不鎖工作區**（任意 process 可讀路徑），支援 `~` 家目錄。
  - 路徑含空格可用雙引號或單引號包住，例如：`讀 "/path/檔 案名.pdf" 請總結`。
  - 支援格式：**PDF**、**.txt**、**.md** / .markdown、**.docx**（Word）、**.xlsx**（Excel）、**.odt** / **.ods**（OpenDocument）；單檔內容上限約 28000 字（超出會截斷）。**圖片**（OCR 或視覺模型）可依需求另行擴充，目前未內建。
  - **讀取網頁**：訊息中含 **http(s) 網址** 且含「讀取網頁」「讀取網址」「讀取連結」等關鍵字，或訊息幾乎只有網址時，會抓取該頁正文並注入「附檔內容」；例：`讀取網頁 https://example.com 請總結` 或直接貼網址。
- **寫入工作區**：輸入「寫 *路徑* *內容*」或「寫入 *路徑* *內容*」可將文字寫入工作區內檔案；路徑限工作區內（禁止 `..`），父目錄會自動建立。例：`寫 notes/總結.txt 今日結論：...`。

記憶存檔位置：**四池** `memory/short_term.jsonl`、`memory/long_term.jsonl`、`memory/core.jsonl`、`memory/current.txt`；**靈魂** `SOUL.md`（每次從核心濃縮 1 句融入後重寫）；背版摘要 `MEMORY.md`（見 [記憶階層—四池一魂](doc/記憶階層—四池一魂提案.md)）。  
目前四池檢索為**關鍵字**；可擴充為語意向量（需 CHATMERY_EMBED_*）。

## 手動建置與執行

```bash
go build -o chatmery-web ./cmd/chatmery-web
# 可選：source .env 載入 API keys
./chatmery-web
```

## 文檔

設計與彙整文件在 `doc/`：

- [專案進度](doc/專案進度.md)（已完成／未完成／模組一覽）
- [記憶階層—四池一魂提案](doc/記憶階層—四池一魂提案.md)（當前→短期→長期→核心→靈魂；濃縮與歸零；答覆結構 1+1+2+3+5）
- [記憶模式—設計對齊](doc/記憶模式—設計對齊.md)（短期每條入池、檢索與觸發對齊）
- [記憶流程—概念定稿](doc/記憶流程—概念定稿.md)（概念背景）
- [對話記憶與背版—設計](doc/對話記憶與背版—設計.md)
- [對話記憶與背版—實作步驟與檔案流程](doc/對話記憶與背版—實作步驟與檔案流程.md)
- [對話記憶系統—彙整與探討](doc/對話記憶系統—彙整與探討.md)
- [網上常見的記憶機制—彙整](doc/網上常見的記憶機制—彙整.md)（對照本專案取捨）
- [對話殼—OpenClaw 讀寫本地檔案與文檔](doc/對話殼—OpenClaw%20讀寫本地檔案與文檔.md)

背版：SOUL = 靈魂（一句到一檔、由核心濃縮融入），MEMORY = 背景摘要。四池（短期／長期／核心＋當前）滿池後解構濃縮進上一階並歸零；答覆時帶最近 10 輪對話與記憶區塊。之後可將 `internal/memory` 抽出為獨立服務（HTTP API），遊戲或 OpenClaw 以 client 呼叫。
