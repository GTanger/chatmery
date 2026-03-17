package knowledge

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Chunk 單一知識庫片段。
type Chunk struct {
	Id        string   `json:"id"`
	Source    string   `json:"source"`
	Content   string   `json:"content"`
	CreatedAt string   `json:"created_at"`
	Links     []string `json:"links,omitempty"` // Obsidian [[筆記名]] 解析結果
}

// Store 知識庫儲存與檢索；與 memory/archival 分離。
type Store struct {
	mu          sync.Mutex
	dir         string
	chunks      []Chunk
	chunkRunes  int
	overlap     int
	expandLinks bool
	expandMax   int
}

// NewStore 建立知識庫；dir 為 knowledge 目錄，chunkRunes/overlap 為切段參數，expandLinks/expandMax 為蒲公英擴散選項。
func NewStore(dir string, chunkRunes, overlap int, expandLinks bool, expandMax int) *Store {
	if chunkRunes <= 0 {
		chunkRunes = 400
	}
	if overlap < 0 {
		overlap = 50
	}
	if expandMax <= 0 {
		expandMax = 5
	}
	s := &Store{
		dir:         dir,
		chunkRunes:  chunkRunes,
		overlap:     overlap,
		expandLinks: expandLinks,
		expandMax:   expandMax,
	}
	_ = s.Load()
	return s
}

func (s *Store) chunksPath() string { return filepath.Join(s.dir, "chunks.jsonl") }

// Load 從 chunks.jsonl 載入。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.chunksPath())
	if err != nil {
		if os.IsNotExist(err) {
			s.chunks = nil
			return nil
		}
		return err
	}
	defer f.Close()
	var chunks []Chunk
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c Chunk
		if json.Unmarshal(sc.Bytes(), &c) == nil {
			chunks = append(chunks, c)
		}
	}
	s.chunks = chunks
	return sc.Err()
}

// Save 將 chunks 寫回 chunks.jsonl（覆寫）。
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(s.chunksPath())
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, c := range s.chunks {
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	return nil
}

// Ingest 將整段文字依 source 標識切段並寫入；回傳寫入的 chunk 數。
func (s *Store) Ingest(text, source string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" || source == "" {
		return 0, nil
	}
	s.mu.Lock()
	// 先刪除同 source 的舊 chunk
	var kept []Chunk
	for _, c := range s.chunks {
		if c.Source != source {
			kept = append(kept, c)
		}
	}
	s.chunks = kept
	segments := ChunkRunes(text, s.chunkRunes, s.overlap)
	now := time.Now().Format(time.RFC3339)
	for i, seg := range segments {
		links := ParseWikiLinks(seg)
		id := source + "_" + strconv.Itoa(i)
		id = strings.ReplaceAll(id, " ", "-")
		s.chunks = append(s.chunks, Chunk{
			Id:        id,
			Source:    source,
			Content:   seg,
			CreatedAt: now,
			Links:     links,
		})
	}
	s.mu.Unlock()
	return len(segments), s.Save()
}

// Retrieve 依 query 關鍵字檢索 topK 條，每條截斷為 snippetMax rune，並可選沿連結擴散。
// 回傳字串格式為 "- [來源] 內容"，供直接注入 prompt。
func (s *Store) Retrieve(query string, topK, snippetMax int) []string {
	s.mu.Lock()
	chunks := make([]Chunk, len(s.chunks))
	copy(chunks, s.chunks)
	expandLinks := s.expandLinks
	expandMax := s.expandMax
	s.mu.Unlock()
	if len(chunks) == 0 {
		return nil
	}
	qw := strings.Fields(strings.ToLower(query))
	type scored struct {
		score int
		i     int
	}
	var list []scored
	for i, c := range chunks {
		lower := strings.ToLower(c.Content)
		score := 0
		for _, w := range qw {
			if strings.Contains(lower, w) {
				score++
			}
		}
		list = append(list, scored{score: score, i: i})
	}
	sort.Slice(list, func(a, b int) bool { return list[a].score > list[b].score })
	// 取 topK 的 index
	seen := make(map[int]bool)
	var indices []int
	for _, sc := range list {
		if len(indices) >= topK {
			break
		}
		if sc.score > 0 || len(list) <= topK {
			if !seen[sc.i] {
				seen[sc.i] = true
				indices = append(indices, sc.i)
			}
		}
	}
	// 選配：沿連結擴散
	if expandLinks && expandMax > 0 {
		added := 0
		for _, idx := range indices {
			if added >= expandMax {
				break
			}
			c := chunks[idx]
			// 雙向：與 c 有連結關係的 chunk 也加入
			for j, d := range chunks {
				if seen[j] {
					continue
				}
				linked := false
				for _, L := range c.Links {
					if d.Source == L || strings.Contains(d.Content, "[["+L+"]]") {
						linked = true
						break
					}
				}
				for _, L := range d.Links {
					if c.Source == L || c.Id == L || strings.Contains(c.Content, "[["+L+"]]") {
						linked = true
						break
					}
				}
				if linked {
					seen[j] = true
					indices = append(indices, j)
					added++
					if added >= expandMax {
						break
					}
				}
			}
		}
	}
	var out []string
	for _, idx := range indices {
		c := chunks[idx]
		content := CapRunes(c.Content, snippetMax)
		out = append(out, "- ["+c.Source+"] "+content)
	}
	return out
}

// ListSources 回傳所有不重複的 source。
func (s *Store) ListSources() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool)
	var list []string
	for _, c := range s.chunks {
		if c.Source != "" && !seen[c.Source] {
			seen[c.Source] = true
			list = append(list, c.Source)
		}
	}
	sort.Strings(list)
	return list
}

// DeleteBySource 刪除指定 source 的所有 chunk。
func (s *Store) DeleteBySource(source string) error {
	s.mu.Lock()
	var kept []Chunk
	for _, c := range s.chunks {
		if c.Source != source {
			kept = append(kept, c)
		}
	}
	s.chunks = kept
	s.mu.Unlock()
	return s.Save()
}
