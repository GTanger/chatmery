package document

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// MaxRunes 單一附檔注入 context 的上限（避免爆 context）
const MaxRunes = 28000

var textExts = map[string]bool{
	".txt": true, ".md": true, ".markdown": true,
}

// ExtractText 從本機檔案擷取文字：.pdf 用 PDF 庫，.txt/.md 當 UTF-8 讀；超過 MaxRunes 會截斷。
func ExtractText(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".pdf" {
		return extractPDF(path)
	}
	if textExts[ext] || ext == "" {
		return extractPlain(path)
	}
	return "", nil
}

func extractPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	raw, _ := io.ReadAll(b)
	return capRunes(strings.TrimSpace(string(raw)), MaxRunes), nil
}

func extractPlain(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// 簡單當 UTF-8；若有 BOM 可略過
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		raw = raw[3:]
	}
	return capRunes(strings.TrimSpace(string(raw)), MaxRunes), nil
}

func capRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n >= max {
			return s[:i] + "\n…（已截斷）"
		}
		n++
	}
	return s
}
