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

// Embedder 由呼叫端注入，用於語意向量檢索（不記得句子、記得內容）。
type Embedder interface {
	Embed(text string) ([]float32, error)
}

// ArchivalEntry 單條長期記憶，對齊設計文件 archival。
type ArchivalEntry struct {
	Content   string `json:"content"`
	Tag       string `json:"tag"`
	Timestamp string `json:"timestamp"`
}

// Archival 負責長期 archival 的讀寫；檢索可為關鍵字或向量（若有 Embedder 且已載入向量）。
type Archival struct {
	mu             sync.Mutex
	Path           string
	vectorsPath    string
	entries        []ArchivalEntry
	vectors        [][]float32   // 與 entries 同序，用於長期檢索
	sessionFacts   []ArchivalEntry
	sessionVectors [][]float32  // 與 sessionFacts 同序
	rolloverLimit  int
	embedder       Embedder
}

// NewArchival 建立 Archival；path 為 archival.jsonl 路徑，embedder 為 nil 則僅用關鍵字檢索。
func NewArchival(path string, rolloverLimit int, embedder Embedder) *Archival {
	if rolloverLimit <= 0 {
		rolloverLimit = 20
	}
	dir := filepath.Dir(path)
	a := &Archival{
		Path:          path,
		vectorsPath:   filepath.Join(dir, "archival_vectors.jsonl"),
		rolloverLimit: rolloverLimit,
		embedder:      embedder,
	}
	_ = a.Load()
	return a
}

// Load 從 JSONL 載入全部條目，若有向量檔且行數一致則一併載入。
func (a *Archival) Load() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.Open(a.Path)
	if err != nil {
		if os.IsNotExist(err) {
			a.entries = nil
			a.vectors = nil
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
	if err := sc.Err(); err != nil {
		return err
	}
	a.entries = entries
	a.vectors = nil
	if a.embedder != nil {
		vf, err := os.Open(a.vectorsPath)
		if err == nil {
			vsc := bufio.NewScanner(vf)
			var vecs [][]float32
			for vsc.Scan() {
				var arr []float32
				if json.Unmarshal(vsc.Bytes(), &arr) == nil {
					vecs = append(vecs, arr)
				}
			}
			vf.Close()
			if len(vecs) == len(entries) {
				a.vectors = vecs
			}
		}
	}
	return nil
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

// Insert 新增一筆並立即寫入 JSONL（append）；若有 embedder 會一併寫入向量檔。
func (a *Archival) Insert(content, tag string) error {
	if tag == "" {
		tag = "fact"
	}
	e := ArchivalEntry{
		Content:   content,
		Tag:       tag,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	var vec []float32
	if a.embedder != nil {
		v, err := a.embedder.Embed(content)
		if err == nil {
			vec = v
		}
	}
	a.mu.Lock()
	a.entries = append(a.entries, e)
	if vec != nil {
		a.vectors = append(a.vectors, vec)
	}
	path := a.Path
	vectorsPath := a.vectorsPath
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if vec != nil {
		vf, err := os.OpenFile(vectorsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		_ = json.NewEncoder(vf).Encode(vec)
		vf.Close()
	}
	return nil
}

// SearchPool 長期記憶檢索：若有 embedder 且已載入向量則用語意相似度，否則關鍵字檢索；回傳 topK 條。
func (a *Archival) SearchPool(query string, topK int) []string {
	a.mu.Lock()
	entries := make([]ArchivalEntry, len(a.entries))
	copy(entries, a.entries)
	vectors := make([][]float32, len(a.vectors))
	copy(vectors, a.vectors)
	embedder := a.embedder
	a.mu.Unlock()
	if embedder != nil && len(vectors) == len(entries) && len(entries) > 0 {
		qvec, err := embedder.Embed(query)
		if err != nil || len(qvec) == 0 {
			return searchPoolQuery(query, entries, topK)
		}
		return searchPoolVector(qvec, entries, vectors, topK)
	}
	return searchPoolQuery(query, entries, topK)
}

// SessionHits 短期 session 檢索：若有 embedder 且 session 有向量則用語意相似度，否則關鍵字；回傳 topK 條。
func (a *Archival) SessionHits(query string, topK int) []string {
	a.mu.Lock()
	session := make([]ArchivalEntry, len(a.sessionFacts))
	copy(session, a.sessionFacts)
	sessionVecs := make([][]float32, len(a.sessionVectors))
	copy(sessionVecs, a.sessionVectors)
	embedder := a.embedder
	a.mu.Unlock()
	if embedder != nil && len(sessionVecs) == len(session) && len(session) > 0 {
		qvec, err := embedder.Embed(query)
		if err != nil || len(qvec) == 0 {
			return searchPoolQuery(query, session, topK)
		}
		return searchPoolVector(qvec, session, sessionVecs, topK)
	}
	return searchPoolQuery(query, session, topK)
}

// searchPoolVector 用查詢向量與各條向量的內積（L2 正規化時等於餘弦相似度）排序取 topK。
func searchPoolVector(qvec []float32, entries []ArchivalEntry, vectors [][]float32, topK int) []string {
	type scored struct {
		score float32
		text  string
	}
	var list []scored
	for i, e := range entries {
		if i >= len(vectors) {
			break
		}
		v := vectors[i]
		if len(v) != len(qvec) {
			continue
		}
		var dot float32
		for j := range qvec {
			dot += qvec[j] * v[j]
		}
		list = append(list, scored{score: dot, text: e.Content})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].score > list[j].score })
	var out []string
	for i := 0; i < topK && i < len(list); i++ {
		out = append(out, list[i].text)
	}
	return out
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
	if a.embedder != nil {
		vec, err := a.embedder.Embed(content)
		if err == nil {
			a.sessionVectors = append(a.sessionVectors, vec)
		} else {
			a.sessionVectors = append(a.sessionVectors, nil)
		}
	}
	if len(a.sessionFacts) <= a.rolloverLimit {
		return nil
	}
	batch := a.takeAndRemoveBatchForRefine(refineBatchMax, refineRunesPerItem)
	if a.embedder != nil && len(a.sessionVectors) >= len(batch) {
		a.sessionVectors = a.sessionVectors[len(batch):]
	}
	return batch
}

// BackfillVectors 對尚未有向量的長期條目補算 embedding 並寫入向量檔（啟動時若啟用 embed 且條目多於向量可呼叫一次）。
func (a *Archival) BackfillVectors() error {
	a.mu.Lock()
	entries := make([]ArchivalEntry, len(a.entries))
	copy(entries, a.entries)
	vectors := make([][]float32, len(a.vectors))
	copy(vectors, a.vectors)
	embedder := a.embedder
	path := a.vectorsPath
	a.mu.Unlock()
	if embedder == nil || len(vectors) >= len(entries) {
		return nil
	}
	for i := len(vectors); i < len(entries); i++ {
		vec, err := embedder.Embed(entries[i].Content)
		if err != nil {
			continue
		}
		vectors = append(vectors, vec)
	}
	if len(vectors) == 0 {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, v := range vectors {
		_ = enc.Encode(v)
	}
	if err := f.Close(); err != nil {
		return err
	}
	a.mu.Lock()
	a.vectors = vectors
	a.mu.Unlock()
	return nil
}
