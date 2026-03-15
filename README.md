# Chatmery

專案名 = **chat** + **memory** 合併（非「聊天室」；若丟 Google 翻譯會誤翻成聊天室，此為自造專案名）。  
對話外殼（Telegram + Ollama）+ 記憶系統（背版 + archival），Go 實作。**預設專為小模型設計（約 4B 參數等級）**：輕量、迅速、不過度消耗 context，適合本地 4B 左右模型日常對談。記憶不靠「MD 對話紀錄 + 硬式檢索」（那樣使用者在 Tele 會等了又等），改為「抽出的要旨 → 少數條目注入」；可獨立運行，之後可接回遊戲或給 OpenClaw 當記憶後端。

**對話殼優先級**：上網檢索的能力與精準度最重要（模型知識截止於訓練日，必須能查新訊息）；其次為寫文檔、紀錄討論。見 [對話殼—OpenClaw 讀寫本地檔案與文檔](doc/對話殼—OpenClaw%20讀寫本地檔案與文檔.md)。

## 架構

- **internal/config** — 環境變數與路徑（WORKSPACE、TOKEN、搜尋後端）
- **internal/memory** — 背版（SOUL.md、MEMORY.md）、archival（JSONL + 關鍵字檢索）、session facts 與 rollover
- **internal/search** — 搜尋關鍵字萃取、Brave / Tavily API（Go 版未實作 DuckDuckGo）
- **internal/ollama** — Ollama chat 串流與 generate
- **cmd/chatmery** — Telegram bot 入口，組 prompt、呼叫記憶與搜尋、deconstruct 寫回

## 敏感資訊：獨檔管理、不寫進程式碼

Token 與 API key **一律用環境變數**，可寫在 `.env`（已加入 .gitignore，不會被 push）：

```bash
cp .env.example .env
# 編輯 .env 填入 TELEGRAM_BOT_TOKEN、BRAVE_API_KEY 等
```

程式碼與 repo 內**禁止**寫入真實 token/key；`.env.example` 僅為欄位說明，可提交。

## 環境變數（對照 .env.example）

| 變數 | 說明 |
|------|------|
| `TELEGRAM_BOT_TOKEN` 或 `CHATMERY_TELEGRAM_TOKEN` | Telegram bot token（必填） |
| `CHATMERY_WORKSPACE` | 資料目錄，預設為當前工作目錄 |
| `CHATMERY_MODEL` | Ollama 模型，預設 `qwen-4b-slim:latest` |
| `OLLAMA_HOST` | Ollama URL，預設 `http://localhost:11434` |
| `CHATMERY_SEARCH` | 搜尋後端：`brave` 或 `tavily`（預設 brave） |
| `BRAVE_API_KEY` | Brave Search API key |
| `TAVILY_API_KEY` | Tavily API key |
| `CHATMERY_MEMORY_LONG_K` | 長期記憶最多幾條（預設 3） |
| `CHATMERY_MEMORY_SESSION_K` | 當前 session 記憶最多幾條（預設 2） |
| `CHATMERY_WEB_SEARCH_MAX` | 網搜最多幾條（預設 3） |
| `CHATMERY_SNIPPET_MAX` | 每條記憶/搜尋結果最多幾字，0=不截（預設 120） |

Workspace 下需有 `SOUL.md`（可選，無則用預設）、`MEMORY.md`（可選）。`memory/archival.jsonl` 會自動建立。

### 小模型使用建議（預設約 4B 等級）

- 保持 **SOUL.md**、**MEMORY.md** 簡短（各約數十字～百字），避免塞滿 context。
- 預設注入量針對 4B 左右模型（長期 3 條、session 2 條、網搜 3 條、每條最多 120 字）；更大模型可調大上述環境變數。
- 預設模型為 `qwen-4b-slim:latest`；Ollama 端可設較小 `num_ctx`（如 2048）以加快推理。

## 一鍵跑

```bash
cp .env.example .env
# 編輯 .env 填入 TELEGRAM_BOT_TOKEN 等

./run.sh --install   # 第一次：安裝開機自啟（寫入 systemd user 服務）
./run.sh             # 之後：由 systemd 在後台啟動，確認後提示「可關閉終端」
```

- **第一次**：先執行 `./run.sh --install`，會寫入 systemd 服務並 enable（開機自啟）。
- **之後**：執行 `./run.sh` 會呼叫 `systemctl --user start chatmery`，等約 2 秒確認已在後台跑，再提示「Chatmery 已在後台執行，可關閉終端」。
- 本機需已啟動 Ollama。查狀態：`systemctl --user status chatmery`。
- 若未安裝過開機自啟就執行 `./run.sh`，會提示請先執行 `./run.sh --install`。
- **`--background`**：不用 systemd，改為 nohup 後台跑（log 寫入 `chatmery.log`），適合未做 `--install` 時用。

## 手動建置與執行

```bash
go build -o chatmery ./cmd/chatmery
# 先載入 .env：source .env 或 export TELEGRAM_BOT_TOKEN=...
./chatmery
```

## 文檔

設計與彙整文件在 `doc/`：

- [記憶流程—概念定稿](doc/記憶流程—概念定稿.md)（雙池、無 MD、回覆只讀短期＋長期）
- [對話記憶與背版—設計](doc/對話記憶與背版—設計.md)
- [對話記憶與背版—實作步驟與檔案流程](doc/對話記憶與背版—實作步驟與檔案流程.md)
- [對話記憶系統—彙整與探討](doc/對話記憶系統—彙整與探討.md)
- [網上常見的記憶機制—彙整](doc/網上常見的記憶機制—彙整.md)（對照本專案取捨）
- [對話殼—OpenClaw 讀寫本地檔案與文檔](doc/對話殼—OpenClaw%20讀寫本地檔案與文檔.md)

背版：SOUL = identity 概念，MEMORY = summary 概念；archival = 長期記憶，session facts = 短期 pool，rollover 寫入 archival。之後可將 `internal/memory` 抽出為獨立服務（HTTP API），遊戲或 OpenClaw 以 client 呼叫。
