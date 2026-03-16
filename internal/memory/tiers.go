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

// Tiers 四池一魂：短期、長期、核心、當前；靈魂由 Backstory/SOUL.md 負責。
type Tiers struct {
	mu sync.Mutex

	dir string

	shortTerm []ArchivalEntry
	longTerm  []ArchivalEntry
	core      []ArchivalEntry
	current   string // 當前 1 句（上一輪解構結果）

	shortCap    int
	shortTo     int
	longCap     int
	longTo      int
	coreCap     int
	coreTo      int
	maxRunesPer int
}

// NewTiers 建立四池，載入既有資料。
func NewTiers(dir string, shortCap, shortTo, longCap, longTo, coreCap, coreTo, maxRunesPer int) *Tiers {
	if shortCap <= 0 {
		shortCap = 200
	}
	if shortTo <= 0 {
		shortTo = 50
	}
	if longCap <= 0 {
		longCap = 100
	}
	if longTo <= 0 {
		longTo = 5
	}
	if coreCap <= 0 {
		coreCap = 20
	}
	if coreTo <= 0 {
		coreTo = 1
	}
	t := &Tiers{
		dir:          dir,
		shortCap:     shortCap,
		shortTo:      shortTo,
		longCap:     longCap,
		longTo:      longTo,
		coreCap:     coreCap,
		coreTo:      coreTo,
		maxRunesPer: maxRunesPer,
	}
	_ = t.Load()
	return t
}

func (t *Tiers) shortPath() string  { return filepath.Join(t.dir, "short_term.jsonl") }
func (t *Tiers) longPath() string   { return filepath.Join(t.dir, "long_term.jsonl") }
func (t *Tiers) corePath() string  { return filepath.Join(t.dir, "core.jsonl") }
func (t *Tiers) currentPath() string { return filepath.Join(t.dir, "current.txt") }

// Load 從磁碟載入短期、長期、核心與當前。
func (t *Tiers) Load() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.shortTerm = loadJSONL(t.shortPath())
	t.longTerm = loadJSONL(t.longPath())
	t.core = loadJSONL(t.corePath())
	t.current = loadCurrent(t.currentPath())
	return nil
}

func loadJSONL(path string) []ArchivalEntry {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}
	defer f.Close()
	var out []ArchivalEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e ArchivalEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

func loadCurrent(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (t *Tiers) saveJSONL(path string, entries []ArchivalEntry) error {
	if err := os.MkdirAll(t.dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tiers) saveCurrent(content string) error {
	if err := os.MkdirAll(t.dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(t.currentPath(), []byte(content), 0644)
}

// AddToShortTerm 當前解構一句進短期。若短期池滿（cap），回傳 needCondense=true 與整池內容供濃縮，並已歸零短期池。
func (t *Tiers) AddToShortTerm(content string) (needCondense bool, batch []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := ArchivalEntry{
		Content:   content,
		Tag:       "event",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	t.shortTerm = append(t.shortTerm, e)
	if len(t.shortTerm) < t.shortCap {
		_ = t.saveJSONL(t.shortPath(), t.shortTerm)
		return false, nil
	}
	// 滿池：取出全部、歸零、回傳
	for _, e := range t.shortTerm {
		batch = append(batch, capRune(e.Content, t.maxRunesPer))
	}
	t.shortTerm = nil
	_ = t.saveJSONL(t.shortPath(), t.shortTerm)
	return true, batch
}

func capRune(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n >= max {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

// AppendLongTerm 濃縮後的句子追加進長期池。若長期池滿，回傳 needCondense=true 與整池內容，並已歸零長期池。
func (t *Tiers) AppendLongTerm(contents []string) (needCondense bool, batch []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range contents {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		t.longTerm = append(t.longTerm, ArchivalEntry{
			Content:   c,
			Tag:       "long",
			Timestamp: time.Now().Format(time.RFC3339),
		})
	}
	if len(t.longTerm) < t.longCap {
		_ = t.saveJSONL(t.longPath(), t.longTerm)
		return false, nil
	}
	for _, e := range t.longTerm {
		batch = append(batch, capRune(e.Content, t.maxRunesPer))
	}
	t.longTerm = nil
	_ = t.saveJSONL(t.longPath(), t.longTerm)
	return true, batch
}

// AppendCore 濃縮後的句子追加進核心池。若核心池滿，回傳 needCondense=true 與整池內容，並已歸零核心池。
func (t *Tiers) AppendCore(contents []string) (needCondense bool, batch []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range contents {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		t.core = append(t.core, ArchivalEntry{
			Content:   c,
			Tag:       "core",
			Timestamp: time.Now().Format(time.RFC3339),
		})
	}
	if len(t.core) < t.coreCap {
		_ = t.saveJSONL(t.corePath(), t.core)
		return false, nil
	}
	for _, e := range t.core {
		batch = append(batch, capRune(e.Content, t.maxRunesPer))
	}
	t.core = nil
	_ = t.saveJSONL(t.corePath(), t.core)
	return true, batch
}

// SetCurrentFact 寫入當前 1 句（本輪解構結果，下一輪答覆時用）。
func (t *Tiers) SetCurrentFact(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current = strings.TrimSpace(s)
	_ = t.saveCurrent(t.current)
}

// GetCurrentFact 讀取當前 1 句。
func (t *Tiers) GetCurrentFact() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current
}

// ShortTermHits 短期池關鍵字檢索，回傳 topK 條。
func (t *Tiers) ShortTermHits(query string, topK int) []string {
	t.mu.Lock()
	entries := make([]ArchivalEntry, len(t.shortTerm))
	copy(entries, t.shortTerm)
	t.mu.Unlock()
	return tiersKeywordSearch(query, entries, topK)
}

// LongTermHits 長期池關鍵字檢索，回傳 topK 條。
func (t *Tiers) LongTermHits(query string, topK int) []string {
	t.mu.Lock()
	entries := make([]ArchivalEntry, len(t.longTerm))
	copy(entries, t.longTerm)
	t.mu.Unlock()
	return tiersKeywordSearch(query, entries, topK)
}

// CoreHits 核心池關鍵字檢索，回傳 topK 條。
func (t *Tiers) CoreHits(query string, topK int) []string {
	t.mu.Lock()
	entries := make([]ArchivalEntry, len(t.core))
	copy(entries, t.core)
	t.mu.Unlock()
	return tiersKeywordSearch(query, entries, topK)
}

func tiersKeywordSearch(query string, entries []ArchivalEntry, topK int) []string {
	if len(entries) == 0 || topK <= 0 {
		return nil
	}
	qw := strings.Fields(strings.ToLower(query))
	type scored struct {
		score int
		text  string
	}
	var list []scored
	for _, e := range entries {
		lower := strings.ToLower(e.Content)
		score := 0
		for _, w := range qw {
			if strings.Contains(lower, w) {
				score++
			}
		}
		list = append(list, scored{score: score, text: e.Content})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].score > list[j].score })
	var out []string
	for i := 0; i < topK && i < len(list); i++ {
		out = append(out, list[i].text)
	}
	return out
}
