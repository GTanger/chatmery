package knowledge

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ChunkRunes 將文字依 rune 數切段，overlap 為相鄰段重疊 rune 數。
func ChunkRunes(text string, chunkSize, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = 400
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = 50
	}
	var chunks []string
	runes := []rune(text)
	step := chunkSize - overlap
	for i := 0; i < len(runes); i += step {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		seg := strings.TrimSpace(string(runes[i:end]))
		if seg != "" {
			chunks = append(chunks, seg)
		}
		if end >= len(runes) {
			break
		}
	}
	return chunks
}

var wikiLinkRE = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)

// ParseWikiLinks 從內容中解析 Obsidian 風格 [[筆記名]] 或 [[筆記名|顯示]]，回傳筆記名（不含顯示）。
func ParseWikiLinks(content string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range wikiLinkRE.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// CapRunes 截斷為最多 max 個 rune，超出加 …。
func CapRunes(s string, max int) string {
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
