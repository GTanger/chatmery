package document

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	docxtext "github.com/Art-Man/GetDocxText"
	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// MaxRunes 單一附檔注入 context 的上限（避免爆 context）
const MaxRunes = 28000

var textExts = map[string]bool{
	".txt": true, ".md": true, ".markdown": true,
}

// 支援的附檔/讀檔副檔名（用於錯誤訊息）
var SupportedExts = []string{".pdf", ".txt", ".md", ".docx", ".xlsx", ".odt", ".ods"}

// ExtractText 從本機檔案擷取文字；超過 MaxRunes 會截斷。
// 支援：.pdf, .txt, .md, .docx, .xlsx, .odt, .ods
func ExtractText(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pdf":
		return extractPDF(path)
	case ".docx":
		return extractDocx(path)
	case ".xlsx":
		return extractXlsx(path)
	case ".odt":
		return extractOdt(path)
	case ".ods":
		return extractOds(path)
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

func extractDocx(path string) (string, error) {
	xmlContent, err := docxtext.GetXmlContent(path)
	if err != nil {
		return "", err
	}
	paragraphs, err := docxtext.GetTextByParagraph(xmlContent)
	if err != nil {
		return "", err
	}
	return capRunes(strings.TrimSpace(strings.Join(paragraphs, "\n")), MaxRunes), nil
}

func extractXlsx(path string) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var buf strings.Builder
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil {
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString("--- ")
		buf.WriteString(name)
		buf.WriteString(" ---\n")
		for _, row := range rows {
			buf.WriteString(strings.Join(row, "\t"))
			buf.WriteString("\n")
		}
	}
	return capRunes(strings.TrimSpace(buf.String()), MaxRunes), nil
}

// extractOdt 從 OpenDocument 文字檔 (.odt) 擷取文字（ZIP 內 content.xml，取 text:p / text:h 等）。
func extractOdt(path string) (string, error) {
	body, err := readZipFile(path, "content.xml")
	if err != nil {
		return "", err
	}
	return capRunes(strings.TrimSpace(extractOdfText(body)), MaxRunes), nil
}

// extractOds 從 OpenDocument 試算表 (.ods) 擷取文字（ZIP 內 content.xml，取表格儲存格文字）。
func extractOds(path string) (string, error) {
	body, err := readZipFile(path, "content.xml")
	if err != nil {
		return "", err
	}
	return capRunes(strings.TrimSpace(extractOdfText(body)), MaxRunes), nil
}

func readZipFile(zipPath, name string) ([]byte, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, os.ErrNotExist
}

// extractOdfText 從 ODF content.xml 擷取文字（共用 ODT/ODS：text:p, text:h, table:table-cell 等）。
func extractOdfText(xmlBody []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(xmlBody))
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) { return input, nil }
	var out strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// OpenDocument 文字在 text:p, text:h；試算表在 table:table-cell 內的 text:p
		local := start.Name.Local
		if local != "p" && local != "h" && local != "table-cell" {
			continue
		}
		// 讀取此元素內所有字元資料
		inner, _ := readElementText(dec)
		if inner != "" {
			out.WriteString(inner)
			out.WriteString("\n")
		}
	}
	return out.String()
}

// readElementText 從當前 StartElement 讀到對應 EndElement，收集所有 CharData。
func readElementText(dec *xml.Decoder) (string, error) {
	var out strings.Builder
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			return out.String(), err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				return out.String(), nil
			}
		case xml.CharData:
			out.Write(t)
		}
	}
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
