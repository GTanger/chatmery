# Chatmery 接 OpenClaw（技能）設計

**狀態**：設計稿  
**目標**：讓 Chatmery（Go Web UI）在需要時呼叫本機 OpenClaw Gateway，取得技能執行結果（搜網、執行程式、讀寫檔等），並注入當輪答覆 context，不取代既有 Chatmery 模型與記憶。

---

## 一、目標與範圍

- **目標**：使用者在 Chatmery 輸入的訊息，在特定條件下可「委派」給本機 OpenClaw 執行技能，並將 OpenClaw 回傳的結果當成「即時／附檔」般的區塊注入，由 Chatmery 的模型統一回覆。
- **範圍**：
  - 僅與**本機** OpenClaw Gateway 通訊（預設 `localhost:18789`），不涉及 Telegram / Discord 等載體。
  - 不取代 Chatmery 現有網搜、知識庫、記憶、讀寫檔；可與其並存，由設定或觸發條件決定是否呼叫 OpenClaw。
  - 本文件不實作「OpenClaw 反過來呼叫 Chatmery」或雙向同步。

---

## 二、背景

### 2.1 Chatmery 現有能力

- **對話**：Ollama / OpenAI / Gemini / OpenRouter，串流回覆。
- **即時**：網搜（Brave / Tavily），結果注入 prompt，並附引用來源。
- **記憶**：四池一魂、SOUL/MEMORY、archival。
- **知識庫**：RAG 攝入與檢索、chunks.jsonl。
- **讀**：本機檔（讀／讀取 路徑）、網頁 URL。
- **寫**：寫／寫入 路徑（工作區內）、加入知識庫。

### 2.2 OpenClaw 與 Gateway

- **OpenClaw**：開源個人 AI 助理框架（[github.com/openclaw/openclaw](https://github.com/openclaw/openclaw)），主體為 **TypeScript**，非 Python；無 `openclaw/core` Python 目錄。
- **Gateway**：本機常駐服務，預設 **port 18789**，負責 session、routing、技能調度；設定在 `~/.openclaw/openclaw.json`（含 `gateway.port`、`gateway.auth` 等）。
- **技能**：由 skills / extensions 提供（搜網、fetch、shell、檔案等）；使用者透過「與 Agent 對話」觸發，Gateway 負責呼叫模型與工具。
- **通訊方式**：
  - **WebSocket**：`ws://localhost:18789`，JSON-RPC 2.0 風格，可送 chat 請求、收串流或完整回應。
  - **HTTP**：若開啟 OpenResponses 相容端點（如 `POST /v1/responses`），可採 stateless 請求；需在 Gateway 設定中啟用。

---

## 三、整合模式選項

| 模式 | 說明 | 優點 | 缺點 |
|------|------|------|------|
| **A. 並行雙引擎** | 每則使用者訊息同時送 Chatmery 與 OpenClaw，再合併結果 | 能力最完整 | 延遲與資源雙倍、合併邏輯複雜 |
| **B. 委派** | 依關鍵字／意圖判斷「需技能」時才轉發 OpenClaw，結果當附檔注入 Chatmery 再回覆 | 延遲可控、責任單一 | 需定義觸發條件與逾時 |
| **C. 僅查詢** | 不自動委派，僅提供「呼叫龍蝦」按鈕或指令，手動觸發 | 實作最簡單 | 需使用者主動選擇 |

**建議**：先採 **B（委派）**，並可選配 **C**（例如「龍蝦」按鈕或「問龍蝦：…」前綴）。A 留作後續進階。

---

## 四、建議方案：委派 + 注入

### 4.1 觸發條件（可配置）

當下列任一成立時，視為「需要 OpenClaw 技能」並發送請求至 Gateway：

- **關鍵字**：使用者訊息包含預設或設定的關鍵字（例如「幫我執行」「跑腳本」「問龍蝦」「openclaw」等），或
- **前綴**：使用者訊息以特定前綴開頭（例如 `龍蝦 `、`/claw `），或
- **意圖**：未來可接 LLM 意圖判斷（需額外呼叫、延遲較高，可選）。

未觸發時，維持現有 Chatmery 流程（不呼叫 Gateway）。

### 4.2 流程概覽

```
使用者輸入
    ↓
[ 觸發 OpenClaw? ]  ← 關鍵字／前綴／(意圖)
    ├─ 否 → 現有 Chatmery 流程（網搜／記憶／知識庫／模型）
    └─ 是 → 向 OpenClaw Gateway 發送 chat 請求（含使用者訊息）
                ↓
            [ 取得 OpenClaw 回覆 ]（逾時與錯誤見 4.4）
                ↓
            將回覆當成「## OpenClaw 技能結果」區塊注入 Chatmery 當輪 system prompt
                ↓
            Chatmery 模型依「既有 context + OpenClaw 結果」產出最終回覆（串流）
```

### 4.3 注入格式

在當輪組裝的 system prompt 中，若本輪有呼叫 OpenClaw 且成功取得內容，新增一區塊，例如：

```markdown
## OpenClaw 技能結果
（以下為本機 OpenClaw 對你問題的執行結果，請依此與其他區塊一併回答。）

<OpenClaw 回覆全文或截斷後內容>
```

- 可設定最大注入字數（如 `OpenClawContextMaxRunes`），避免壓縮過多其他 context。
- 若 Gateway 逾時或錯誤，可選擇不注入，或注入一句「OpenClaw 本次未回覆（逾時或錯誤），請依其他資訊回答。」

### 4.4 逾時與錯誤

- **逾時**：對 Gateway 的請求建議獨立逾時（例如 30–60 秒），不與 Chatmery 整則聊天逾時綁在一起；逾時後視同「未取得結果」，不阻塞 Chatmery 回覆。
- **錯誤**：連線失敗、Gateway 回傳錯誤、解析失敗時，log 並 fallback 為「不注入 OpenClaw 區塊」或注入簡短錯誤說明（可配置）。
- **Gateway 未啟用**：若設定為啟用 OpenClaw 但連線失敗（例如未啟動 OpenClaw），可於啟動時或首次呼叫時 log 一次，之後該輪不重試，避免每則訊息都報錯。

---

## 五、介面設計

### 5.1 設定（config）

建議新增（可放在 `internal/config` 與 `chatmery.tuning` / 環境變數）：

| 設定項 | 說明 | 預設 |
|--------|------|------|
| `CHATMERY_OPENCLAW_ENABLED` | 是否啟用 OpenClaw 委派 | `false` |
| `CHATMERY_OPENCLAW_GATEWAY_URL` | Gateway 位址（WebSocket 或 HTTP base） | `http://127.0.0.1:18789` 或 `ws://127.0.0.1:18789` |
| `CHATMERY_OPENCLAW_TIMEOUT_SEC` | 單次請求逾時（秒） | `60` |
| `CHATMERY_OPENCLAW_TRIGGER_PREFIX` | 觸發前綴（例如 `龍蝦 `），空則僅依關鍵字 | 空 |
| `CHATMERY_OPENCLAW_TRIGGER_KEYWORDS` | 觸發關鍵字（逗號分隔） | 可預設一組（如 `問龍蝦,openclaw,幫我執行`） |
| `CHATMERY_OPENCLAW_CONTEXT_MAX_RUNES` | 注入 prompt 的 OpenClaw 回覆最大字數（0=不限制） | `2000` |
| `CHATMERY_OPENCLAW_AUTH_TOKEN` | 若 Gateway 啟用 token 驗證，則帶入 | 空 |

（實際 key 名稱可與既有 `CHATMERY_*` 命名一致。）

### 5.2 與 Gateway 的契約（參考）

- **WebSocket**（以常見文件為準，實作時需對照官方最新說明）：
  - 連線：`ws://127.0.0.1:18789`（或設定之 host:port）。
  - 送出一筆「chat」請求（JSON-RPC 2.0 或該 Gateway 約定格式），例如 `method: "chat.send"` 或等同之 method，params 含使用者訊息、可選 session id。
  - 接收：串流或單一完整回應；需解析出「助手回覆文字」再注入 Chatmery。
- **HTTP**（若啟用 OpenResponses 相容）：
  - `POST /v1/responses`，body 含 messages 或等效結構；回傳為串流或 JSON。
  - 需在 OpenClaw 端開啟對應 endpoint（如 `gateway.http.endpoints.responses.enabled`）。

實作時應以 OpenClaw 官方文件為準（[docs.openclaw.ai](https://docs.openclaw.ai)、Gateway 章節），並處理版本差異。

### 5.3 模組邊界

- **internal/openclawclient**（或類似名稱）：封裝「是否觸發」「組請求」「發送 WebSocket 或 HTTP」「解析回應」「截斷與錯誤處理」。不依賴 `main` 或 HTTP handler。
- **cmd/chatmery-web**：在 `handleChat` 內，依 config 與使用者訊息決定是否呼叫 `openclawclient`；若取得內容，則附加到當輪 system prompt 的「OpenClaw 技能結果」區塊，其餘流程不變。

---

## 六、實作階段建議

1. **Phase 1（最小可行）**
   - 新增 config 欄位與 tuning/env 讀取。
   - 實作 `internal/openclawclient`：僅支援一種通訊方式（建議先 WebSocket，因多數本機預設即開），觸發條件僅「前綴」或「關鍵字」。
   - 在 `handleChat` 內：若啟用且觸發，呼叫 client、取得字串、寫入 prompt 區塊；逾時或錯誤則不注入並 log。
2. **Phase 2**
   - 支援 HTTP OpenResponses（若 Gateway 已啟用）。
   - 可配置錯誤時是否注入簡短說明。
   - 觸發關鍵字可從 config 讀取（逗號分隔）。
3. **Phase 3（可選）**
   - 前端「龍蝦」按鈕或「問龍蝦」快捷，明確送出一則帶前綴的訊息，走同一委派流程。
   - 可選：意圖判斷（LLM）決定是否委派，需權衡延遲與準確度。

---

## 七、風險與取捨

- **依賴本機 OpenClaw**：Gateway 未跑或 port 不符時，委派失敗；建議優雅降級與清楚 log，不影響 Chatmery 主流程。
- **雙模型**：Chatmery 用 A 模型、OpenClaw 用 B 模型時，成本與延遲為兩次推理；可文件說明「委派時會多一次 OpenClaw 呼叫」。
- **安全**：Gateway 若僅 bind loopback 且僅本機使用，風險較低；若未來改為遠端，需考慮 auth（token）與 TLS。
- **版本**：OpenClaw 協定可能隨版本變動，建議在文件或程式註解標註對應之 OpenClaw 版本（如 2026.3.x）。

---

## 八、參考

- [OpenClaw GitHub](https://github.com/openclaw/openclaw)
- [OpenClaw Docs – Agent Workspace](https://docs.openclaw.ai/concepts/agent-workspace)
- [OpenClaw Gateway / WebSocket / HTTP API]（以官方 docs 為準）
- 本專案：[龍蝦載體替代方案與效能瓶頸](龍蝦載體替代方案與效能瓶頸.md)、[對話殼—OpenClaw 讀寫本地檔案與文檔](對話殼—OpenClaw%20讀寫本地檔案與文檔.md)
