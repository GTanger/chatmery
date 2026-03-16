package llm

// Message 單則對話訊息（與 Ollama / OpenAI 等格式相容）。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Provider 聊天與單次生成介面；可由 Ollama、OpenAI 相容 API 等實作。
type Provider interface {
	// ChatStream 發送對話並以 callback 接收串流片段；回傳完整回覆與錯誤。
	ChatStream(messages []Message, onChunk func(text string)) (full string, err error)
	// Generate 單次生成（用於 deconstruct / refine），非串流。
	Generate(prompt string) (string, error)
}
