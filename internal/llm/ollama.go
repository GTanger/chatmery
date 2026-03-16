package llm

import (
	"time"

	"github.com/tanger/chatmery/internal/ollama"
)

// Ollama 實作 Provider，委派給本機 Ollama API。
type Ollama struct {
	*ollama.Client
}

// NewOllama 建立 Ollama provider。timeout 為 0 時由 ollama 包使用預設（300s）。
func NewOllama(baseURL, model string, timeout time.Duration) *Ollama {
	return &Ollama{Client: ollama.NewClient(baseURL, model, timeout)}
}

// ChatStream 實作 Provider。
func (o *Ollama) ChatStream(messages []Message, onChunk func(text string)) (string, error) {
	msgs := make([]ollama.ChatMessage, len(messages))
	for i := range messages {
		msgs[i] = ollama.ChatMessage{Role: messages[i].Role, Content: messages[i].Content}
	}
	return o.Client.ChatStream(msgs, onChunk)
}

// Generate 實作 Provider。
func (o *Ollama) Generate(prompt string) (string, error) {
	return o.Client.Generate(prompt)
}
