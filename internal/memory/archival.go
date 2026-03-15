package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ArchivalEntry 單條長期記憶，對齊設計文件 archival。
type ArchivalEntry struct {
	Content   string `json:"content"`
	Tag       string `json:"tag"`
	Timestamp string `json:"timestamp"`
}

// Archival 負責長期 archival 的讀寫與關鍵字檢索；SessionFacts 為短期 session 快取。
type Archival struct {
	mu            sync.Mutex
	Path          string
	entries       []ArchivalEntry
	sessionFacts  []ArchivalEntry
	rolloverLimit int
}

// NewArchival 建立 Archival，path 為 archival.jsonl 路徑。
func NewArchival(path string, rolloverLimit int) *Archival {
	if rolloverLimit <= 0 {
		rolloverLimit = 20
	}
	a := &Archival{Path: path, rolloverLimit: rolloverLimit}
	_ = a.Load()
	return a
}

// Load 從 JSONL 載入全部條目。
func (a *Archival) Load() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.Open(a.Path)
	if err != nil {
		if os.IsNotExist(err) {
			a.entries = nil
			return nil
		}
		return err
	}
	defer f.Close()
	var entries []ArchivalEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e ArchivalEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}
	a.entries = entries
	return sc.Err()
}

// Save 將 entries 寫回 JSONL（覆寫）。
func (a *Archival) Save() error {
	dir := filepath.Dir(a.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(a.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range a.entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// Insert 新增一筆並立即寫入 JSONL（append）。
func (a *Archival) Insert(content, tag string) error {
	if tag == "" {
		tag = "fact"
	}
	e := ArchivalEntry{
		Content:   content,
		Tag:       tag,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	a.mu.Lock()
	a.entries = append(a.entries, e)
	path := a.Path
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(e)
}

// SearchPool 關鍵字檢索：依 query 詞與內容重疊數排序，回傳 topK 條內容字串。
func (a *Archival) SearchPool(query string, topK int) []string {
	a.mu.Lock()
	entries := make([]ArchivalEntry, len(a.entries))
	copy(entries, a.entries)
	session := make([]ArchivalEntry, len(a.sessionFacts))
	copy(session, a.sessionFacts)
	a.mu.Unlock()
	return searchPoolQuery(query, entries, topK)
}

// SessionHits 短期 session 的關鍵字檢索，topK 條。
func (a *Archival) SessionHits(query string, topK int) []string {
	a.mu.Lock()
	session := make([]ArchivalEntry, len(a.sessionFacts))
	copy(session, a.sessionFacts)
	a.mu.Unlock()
	return searchPoolQuery(query, session, topK)
}

func searchPoolQuery(query string, entries []ArchivalEntry, topK int) []string {
	if len(entries) == 0 {
		return nil
	}
	qw := strings.Fields(strings.ToLower(query))
	type scored struct {
		score int
		text  string
	}
	var list []scored
	for _, e := range entries {
		text := e.Content
		lower := strings.ToLower(text)
		score := 0
		for _, w := range qw {
			if strings.Contains(lower, w) {
				score++
			}
		}
		list = append(list, scored{score: score, text: text})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].score > list[j].score })
	var out []string
	for i, s := range list {
		if i >= topK {
			break
		}
		if s.score > 0 || len(list) < topK {
			out = append(out, s.text)
		}
	}
	return out
}

// takeAndRemoveBatchForRefine 取出並移除最舊的至多 maxItems 條，每條 content 截斷為最多 maxRunes 字，供提煉用。呼叫前需持有 mu。
func (a *Archival) takeAndRemoveBatchForRefine(maxItems, maxRunes int) []string {
	if len(a.sessionFacts) == 0 {
		return nil
	}
	n := maxItems
	if n <= 0 {
		n = 15
	}
	if n > len(a.sessionFacts) {
		n = len(a.sessionFacts)
	}
	capRune := func(s string) string {
		if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
			return s
		}
		count := 0
		for i := range s {
			if count >= maxRunes {
				return s[:i] + "…"
			}
			count++
		}
		return s
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, capRune(a.sessionFacts[i].Content))
	}
	a.sessionFacts = a.sessionFacts[n:]
	return out
}

// AddSessionFact 加入一筆短期事實。若超過 rolloverLimit，會取出並移除一批最舊條目供提煉；
// 回傳該批內容（每條已截斷），由呼叫端負責提煉後寫入長期；若無需提煉則回傳 nil。
func (a *Archival) AddSessionFact(content string, refineBatchMax, refineRunesPerItem int) (batchForRefine []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := ArchivalEntry{
		Content:   content,
		Tag:       "event",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	a.sessionFacts = append(a.sessionFacts, e)
	if len(a.sessionFacts) <= a.rolloverLimit {
		return nil
	}
	return a.takeAndRemoveBatchForRefine(refineBatchMax, refineRunesPerItem)
}
