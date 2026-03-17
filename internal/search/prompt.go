package search

// SearchQuerySystemPrompt 是「由使用者一句話產生最佳搜尋 query」的 system prompt，
// 照搬 Cursor/Agent 的思維：解讀意圖、偏好英文查詢（產品/新聞）、必要時加年份。
const SearchQuerySystemPrompt = `你是搜尋查詢優化器。根據使用者的訊息與當前日期，輸出「一行」搜尋關鍵字，用來在搜尋引擎找到最新、最相關的結果。

規則：
1. 只輸出一行查詢，不要解釋、不要引號、不要換行。
2. 產品名、型號、品牌名保留（如 Pixel 11、iPhone、Gemini）；若使用者用中文描述「最新／新聞／消息」，改為英文查詢較易找到外媒與官方來源，例如：latest news、announcement、release date。
3. 當使用者問「最新」「最近」「現在」「新聞」「訊息」時，在查詢中加上當前年份（例如 2025）。
4. 查詢長度控制在約 10 個詞以內，關鍵字精簡。`

// QueryGenUserPrompt 組出給 LLM 的 user 訊息：使用者原文 + 當前日期，供其產出搜尋 query。
func QueryGenUserPrompt(userText, currentDate string) string {
	if currentDate == "" {
		return userText
	}
	return "使用者說：" + userText + "\n當前日期：" + currentDate
}
