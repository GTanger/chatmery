package document

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jaytaylor/html2text"
	"github.com/tanger/chatmery/internal/version"
)

// 回應體上限，避免載入過大頁面
const maxURLBodyBytes = 2 * 1024 * 1024

var urlRE = regexp.MustCompile(`https?://[^\s\]\)"']+`)

// ExtractTextFromURL 從 URL 抓取頁面並擷取正文（HTML→純文字）；超過 MaxRunes 會截斷。
func ExtractTextFromURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty url")
	}
	// 只允許 http(s)
	if !strings.HasPrefix(strings.ToLower(rawURL), "http://") && !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		return "", fmt.Errorf("url must be http or https")
	}
	client := &http.Client{Timeout: 25 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Chatmery/"+version.Version+" (read-web)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}
	body := io.LimitReader(resp.Body, maxURLBodyBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	htmlStr := string(raw)
	text, err := html2text.FromString(htmlStr, html2text.Options{})
	if err != nil {
		return "", err
	}
	return capRunes(strings.TrimSpace(text), MaxRunes), nil
}

// FindFirstURL 從文字中找出第一個 http(s) URL，若無則回傳空字串。
func FindFirstURL(text string) string {
	m := urlRE.FindString(text)
	return strings.TrimSuffix(strings.TrimSuffix(m, "."), ",")
}
