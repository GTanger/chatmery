# 對話殼 — OpenClaw 如何讓模型讀取本地檔案與撰寫文檔

> 整理自 OpenClaw 官方文件與搜尋結果，供對照 Chatmery 對話殼的擴充（例如是否要加「讀本地檔／寫文檔」能力）。  
> 文件參考：[OpenClaw Tools](https://docs.openclaw.ai/tools)、[PDF Tool](https://docs.openclaw.ai/tools/pdf)、[Memory](https://docs.openclaw.ai/concepts/memory)。

---

## 一、核心做法：工具 + 路徑（工作區為可選限制）

OpenClaw 讓模型讀寫本地檔案的方式是：

- **給模型一組「工具」（tools）**，由模型在對話中決定何時呼叫。
- 工具接受**你傳入的路徑**（本機絕對路徑、`file://`、或工作區內相對路徑）。**只要路徑在 process 有權限存取的範圍內，就可以讀**——實務上你若給它「工作區外的完整路徑」（例如 `/home/user/Documents/xxx.pdf`），它一樣能讀。
- **工作區（workspace）** 是預設的「建議目錄」（預設 `~/.openclaw/workspace`），不是硬性邊界。可選政策 **workspace-only** 開啟時，才會把讀寫限制在該目錄樹下；**若未開啟或你未開 sandbox，工作區外路徑照樣可用**。

也就是：**讀寫能力 = 工具 API + 你給的路徑**；工作區只是「可選的安全邊界」，不是「只能讀這裡」。

---

## 二、讀取本地檔案

### 2.1 一般檔案：`read` 工具（group:fs）

- 工具名稱：`read`（與 `write`、`edit`、`apply_patch` 同屬 `group:fs`）。
- 行為：讀取**你傳入之路徑**的檔案內容。
- 路徑：**預設可傳任意 process 可讀的本機路徑**（含工作區外）。只有當設定 `tools.fs.workspaceOnly` 等政策時，才會拒絕工作區外路徑。
- 所以你只要在對話裡給**詳細的工作區外檔案位置與檔名**，模型就會把該路徑傳給 `read`，一樣能讀。

### 2.2 PDF：專用 `pdf` 工具

- 工具名稱：`pdf`（必要時可配 `pdfs` 一次多檔，最多 10 個）。
- 支援的參考形式：
  - 本機路徑（支援 `~` 展開）— **含工作區外路徑**
  - `file://` URL
  - `http://`、`https://` URL
- **Workspace-only 為可選**：文件寫「With workspace-only file policy enabled, local file paths outside allowed roots are rejected」——亦即**沒開**時，工作區外路徑一樣可讀。
- 行為：
  - **Native 模式**（Anthropic、Google）：把 PDF 位元組直接送給 provider API，模型原生理解。
  - **Fallback 模式**（其他 provider）：先擷取文字，若太短再擷取指定頁面成圖片送給模型。
- 範例：你說「讀 `/home/me/Documents/report.pdf` 並用五點總結」→ 模型呼叫 `pdf` 傳入該路徑即可。

所以：**讀一般檔靠 `read`，讀 PDF 靠 `pdf`**；**給詳細路徑（含工作區外）就能讀**，除非你主動開了 workspace-only 或 sandbox 限制。

---

## 三、撰寫文檔

### 3.1 一般寫檔：`write`、`edit`、`apply_patch`（group:fs）

- **`write`**：把內容寫入指定路徑（覆寫或 append，依參數）。
- **`edit`**：對檔案做編輯（例如指定區段替換）。
- **`apply_patch`**：套用結構化 patch（多檔、多段修改）；可設 `workspaceOnly: true`，只允許在工作區內寫入／刪除。

同樣可限制在工作區內，模型在對話中根據使用者意圖呼叫（例如「幫我寫一份會議紀錄到 notes/meeting-03.md」）。

### 3.2 記憶／文檔的「寫哪裡」：Memory 設計

OpenClaw 的 **Memory** 是「給人類看的文檔」＋「模型會寫入的目標」：

- 檔案就是 **Markdown**，放在 workspace 下：
  - **`MEMORY.md`**：長期、整理過的重要資訊（決策、偏好、持久事實）。
  - **`memory/YYYY-MM-DD.md`**：每日日誌（append-only）。
- 模型**不會自動寫**；要持久化就要**在對話裡提醒模型寫進 memory**：
  - 例如：「請把這段記下來」→ 模型應寫入 `MEMORY.md` 或 `memory/YYYY-MM-DD.md`。
- 另有 **memory 工具**：
  - **`memory_get`**：讀取指定 MD 檔或行範圍。
  - **`memory_search`**：對已索引的記憶片段做語意搜尋。
- 可選 **自動 memory flush**：在 context 快滿、要做壓縮前，系統觸發一輪「請把該留的寫進 memory」，模型寫完後再壓縮，避免重要內容只存在對話裡。

所以：**撰寫文檔** = 模型透過 **`write`／`edit`** 寫到 workspace 裡的任意路徑；**「記憶」文檔** = 約定寫到 `MEMORY.md` 與 `memory/YYYY-MM-DD.md`，並用 `memory_get`／`memory_search` 讀取。

---

## 四、流程整理（對應你的問題）

| 能力 | OpenClaw 做法 |
|------|----------------|
| **讀取電腦本地檔案** | 提供 `read`（一般檔）、`pdf`（PDF）。**你只要給詳細路徑（含工作區外）**，模型就會把路徑傳給工具並讀取；只有當開啟 workspace-only 或 sandbox 政策時，才會限制在工作區內。 |
| **撰寫文檔** | 提供 `write`、`edit`、`apply_patch`；寫入範圍同樣依政策（未開 workspace-only 時可寫到工作區外路徑）。若為「記憶」則約定寫入 `MEMORY.md`、`memory/YYYY-MM-DD.md`，並用 memory 工具讀取。 |

關鍵點：**實際行為 = 工具 API + 你傳的路徑 + 主機權限**；workspace 是**可選**的安全邊界（開啟 workspace-only 才限制在目錄內），**沒開時給工作區外路徑一樣能讀**。

### 附：session_status 與當前時間

**session_status** 是 OpenClaw 提供給 agent 的**工具（tool）**：agent 呼叫後會回傳目前 session 的狀態，其中包含**一筆即時時間戳**。所以 OpenClaw 的設計是：system prompt 只放「時區 + 當前日期」（方便 prompt 快取），若 agent 需要**精確時刻**就呼叫 `session_status` 取得。Chatmery 沒有這支 tool，所以改為在 system 與（當使用者問日期／時間時）user 訊息裡直接注入當前日期與時刻。

---

## 五、若要在 Chatmery（Telegram + Ollama）做類似的事

Chatmery 目前是 Telegram 對話殼 ＋ Ollama ＋ 記憶（短期／長期 JSON），**沒有**「讀本地檔／寫文檔」工具。若未來要加：

- **概念上**可以對齊 OpenClaw：
  - OpenClaw 實務上**未開 workspace-only 時，給工作區外路徑也能讀**；若 Chatmery 要加讀檔，可選擇（a）只允許 workspace 內路徑（較安全），或（b）允許使用者指定任意可讀路徑（較彈性，需注意權限與安全）。
  - 在收到使用者訊息後，若判斷為「讀檔／寫檔」意圖，由 **後端**（Go）依路徑讀檔或寫檔，再把結果或確認訊息回傳給 Telegram；或讓 Ollama 在一個「有工具」的流程裡決定要讀哪個檔、寫什麼內容（需在 prompt 或 function calling 裡描述工具）。
- **實作選項**：
  - **簡易版**：在 Telegram 用關鍵字或簡單指令（例如「讀 workspace 的 xxx.md」），後端直接讀取 workspace 內該路徑、把內容摘要或前 N 字回傳；寫入同理（例如「寫入 yyy.md：內容…」），由後端寫入 workspace。不做完整 tool-calling，只做指令解析。
  - **完整版**：為 Ollama 設計「工具」介面（read_file、write_file、list_dir 等），模型在回覆前可選擇呼叫哪些工具；後端在 workspace 內執行並把結果塞回 context，再讓模型生成最終回覆。這就接近 OpenClaw 的 read/write 模式，但需處理 Telegram 的單輪／多輪與 token 限制。

目前先釐清 OpenClaw 的做法，實作可依需求再選簡易或完整路線。

**本專案對話殼優先級**：
- **最重要：上網檢索的能力與精準度**。模型知識截止於訓練日，被鎖死在成型那天，**能否查詢到新訊息**直接決定對話是否有時效價值；實作與調校時優先保證檢索觸發合理、結果相關、必要時可換後端或加 rerank。
- **其次：能自己寫文檔**，例如把討論的內容紀錄下來（寫入可先限定在 workspace，如 `CHATMERY_WORKSPACE` 下的 `notes/` 或依日期命名的 MD）。

---

## 六、參考連結

- [OpenClaw Tools 總覽](https://docs.openclaw.ai/tools)
- [PDF Tool](https://docs.openclaw.ai/tools/pdf)
- [Memory（記憶與 MD 文檔）](https://docs.openclaw.ai/concepts/memory)
- [OpenClaw Setup — PDF Tool 說明](https://openclawsetup.dev/blog/openclaw-pdf-analysis-tool)

---

*對話殼—OpenClaw 讀寫本地檔案與文檔 v1*
