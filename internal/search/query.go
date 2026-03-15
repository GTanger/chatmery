package search

import (
	"strings"
)

var prefixes = []string{"上網查詢", "幫我查", "查一下", "搜尋", "查詢", "找一下"}

// BuildQuery 從使用者句子萃出簡短搜尋關鍵字，提高 API 相關性。
func BuildQuery(userText string) string {
	t := strings.TrimSpace(userText)
	for _, p := range prefixes {
		if idx := strings.Index(t, p); idx >= 0 {
			t = strings.TrimSpace(t[idx+len(p):])
			break
		}
	}
	t = strings.ReplaceAll(t, "的", " ")
	t = strings.ReplaceAll(t, "與", " ")
	t = strings.ReplaceAll(t, "和", " ")
	words := strings.Fields(t)
	if len(words) > 8 {
		words = words[:8]
	}
	q := strings.Join(words, " ")
	if len(q) > 60 {
		q = q[:60]
	}
	if q == "" {
		if len(userText) > 60 {
			return userText[:60]
		}
		return userText
	}
	return q
}
