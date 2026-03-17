# 知識庫 — 借鑒 OpenClaw 記憶與技能

**目的**：Chatmery 知識庫實際運作漏洞較多；參考 `~/.openclaw` 與 OpenClaw 內建 memory / skills，列出可借鑒的設計與實作要點，供後續改版對照。

---

## 一、OpenClaw 記憶與知識怎麼做

### 1.1 來源 = 純 Markdown，寫進磁碟才算數

- **MEMORY.md**：精煉的長期記憶，只給主 session 讀。
- **memory/YYYY-MM-DD.md**：每日一檔、只追加；開 session 時讀「今天 + 昨天」。
- 文件明寫：*The model only "remembers" what gets written to disk.*

與 Chatmery 的差別：我們只有 chunks.jsonl 的關鍵字檢索，沒有「一份人類可讀、每次開局都載入」的精煉摘要檔。

### 1.2 文章／長文怎麼存（AGENTS.md）

- 文章先存成 **memory/archives/YYYYMMDD_title.md**。
- 然後 **把關鍵洞察、與使用者有關的要點寫進 MEMORY.md**。
- 原則：*Text > Brain — If it's worth knowing, it MUST be in MEMORY.md.*

與 Chatmery 的差別：我們只做「整篇切 chunk 寫 chunks.jsonl」，沒有「再抽一層精華寫進單一摘要」的流程，容易漏或只撿到零散片段。

### 1.3 兩支工具：memory_search、memory_get

- **memory_search**：對 MEMORY.md + memory/*.md 做**語意檢索**（可開 BM25 混搭），回傳片段（路徑、行範圍、分數），不整檔吐出。
- **memory_get**：依路徑（+ 可選行範圍）讀某個記憶檔；**檔案不存在時回傳 `{ text: "", path }`，不丟錯**，方便 agent 繼續流程。

可借鑒：檢索失敗或空結果時，API 回傳空字串 + 路徑，不讓前端或 prompt 組裝因例外而崩。

### 1.4 檢索與索引（docs/concepts/memory.md）

- **Chunk**：約 400 token、80 token 重疊。
- **Hybrid**：向量 + BM25，權重可調（vectorWeight / textWeight）；兼顧「意思像」與「精確詞（ID、程式符號）」。
- **MMR**：結果做多樣性重排，減少重複片段。
- **Temporal decay**：依時間衰減分數（halfLifeDays），近期筆記權重高；MEMORY.md 與非日期檔不衰減。
- **Embedding cache**：chunk embedding 快取，避免重複算。
- 可選 **QMD**：BM25 + 向量 + rerank 的 sidecar，Markdown 仍是 source of truth。

與 Chatmery 的差別：我們目前只有關鍵字包含計分，沒有向量、沒有 BM25、沒有時間衰減、沒有多樣性重排，檢索容易偏、漏或重複。

### 1.5 開局必讀 + 壓縮前寫回

- Session 開始：**必讀 MEMORY.md**，再讀 memory/今天、昨天。
- 接近 context 壓縮前：**靜默一輪**提醒模型「把該留的寫進 memory/YYYY-MM-DD.md」，常用 NO_REPLY，使用者看不到。

可借鑒：知識庫也可有「每次答題前必載入」的**精煉摘要**（例如 knowledge/summary.md），而不只靠即時檢索 chunks。

### 1.6 每日記憶的結構（workspace 範例）

`memory/2026-03-15.md` 之類的檔案會寫成：

- 日期、狀態、早晨重點、記憶測試、學習與洞察、**文章收藏**（標題、作者、連結、核心內容、關鍵章節）。

可借鑒：攝入網頁／檔案時，除了寫 chunks.jsonl，可**多寫一筆結構化摘要**（標題、來源 URL、日期、3～5 點要點）到 knowledge/archives/ 或 knowledge/summary.md，方便人類查閱與檢索補強。

---

## 二、可借鑒的 OpenClaw skills（與知識庫相關）

- **summarize**：URL / 本地檔用外部 API 做摘要或轉錄；觸發語如 "summarize this URL"。Chatmery 可選：攝入時呼叫摘要 API 產出「精華段落」再寫入知識庫或 summary。
- **obsidian**：筆記用 wikilink、obsidian-cli 搜尋／建立／搬移。Chatmery 已有 `[[連結]]` 解析與擴散，可對齊「連結即來源」的用法與錯誤處理（連結斷掉時 graceful 降級）。
- **memory-core**（內建）：提供 memory_search / memory_get、索引 MEMORY.md + memory/*.md、hybrid、temporal、MMR。Chatmery 知識庫可逐步引進：向量（或現有 embed）+ 簡單 BM25 或關鍵字權重、時間衰減、空結果不報錯。

其餘 skills（healthcheck、nano-pdf、tmux、weather 等）與「知識庫」較無直接關係，可略。

---

## 三、對 Chatmery 知識庫的具體建議（對應漏洞）

| 漏洞／現象 | OpenClaw 做法 | 建議 |
|------------|----------------|------|
| 只存 chunk，沒有「精華摘要」 | 文章 → archives/*.md，再抽要點寫進 MEMORY.md | 攝入時可選：產出一段摘要寫入 `knowledge/summary.md` 或 `knowledge/archives/YYYYMMDD_標題.md`，並在組 prompt 時固定載入 summary（或最後 N 條摘要）。 |
| 檢索只靠關鍵字，漏檢或噪音大 | 向量 + BM25 hybrid、MMR、temporal decay | 有 embed 時做向量檢索；沒有則加強關鍵字權重或簡單 BM25；chunk 帶 created_at 時可做時間衰減；結果做簡易多樣性過濾。 |
| 空結果或漏檔就炸 | memory_get 缺檔回傳 `{ text: "", path }` | 檢索無結果時回傳空列表 + 可選 log，不讓呼叫端因 missing 崩潰；API 回傳格式固定。 |
| 存了什麼人類看不到 | MEMORY.md + memory/ 皆為 Markdown，可手動編輯 | 知識庫目錄下提供「來源列表 + 每則一筆摘要」的 Markdown（或既有 sources API 擴充一欄「最後摘要」），方便人工檢查與除錯。 |
| 加入知識庫的意圖搞錯 | AGENTS.md 明確寫「存哪裡、抽到哪裡」 | 能力邊界與 prompt 寫清楚：何時寫入 chunks、何時更新 summary、模型不可假稱「已存入」未執行的操作。 |

---

## 四、實作優先順序建議

1. **必讀摘要**：新增 `knowledge/summary.md`（或等價），攝入時可選寫入一筆「來源 + 3～5 點」；每輪答題前若存在則注入 prompt，減少「完全沒帶到」的漏。
2. **空結果不炸**：Retrieve / API 在無 chunk 或無摘要時回傳空結構，不拋錯；前端與 prompt 組裝都當「無知識庫內容」處理。
3. **時間衰減**：chunk 保留 created_at，Retrieve 時對分數做簡單衰減（例如依天數乘 decay），近期攝入權重較高。
4. **Hybrid / 向量**：若有 embed，做向量檢索並與關鍵字分數合併；沒有則先加強關鍵字權重或簡單 BM25 式計分。
5. **結構化存檔**：攝入 URL/檔時，多寫 `knowledge/archives/YYYYMMDD_標題.md` 或一筆 JSON 條目（標題、URL、日期、要點），供人類查與未來擴充檢索。

以上可直接對照現有 `internal/knowledge/store.go`、`cmd/chatmery-web/main.go` 的 ingest/retrieve 與 prompt 組裝逐步改，不需一次全做。

---

## 五、參考來源

- `~/.openclaw/workspace/AGENTS.md` — 存檔規則、MEMORY.md 與 archives。
- `~/.openclaw/workspace/memory/2026-03-15.md` — 每日記憶與文章收藏結構。
- OpenClaw 內建：`node_modules/openclaw/docs/concepts/memory.md`、`extensions/memory-core/index.ts`。
- 本專案：[模型知識庫—設計提案](模型知識庫—設計提案.md)、`internal/knowledge/store.go`。
