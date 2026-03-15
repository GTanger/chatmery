package memory

import (
	"os"
)

// Backstory 提供 SOUL（身份）與 MEMORY（背景摘要）的讀取，對齊設計文件的 blocks。
type Backstory struct {
	SoulPath   string
	MemoryPath string
}

// NewBackstory 依 workspace 路徑建立。
func NewBackstory(soulPath, memoryPath string) *Backstory {
	return &Backstory{SoulPath: soulPath, MemoryPath: memoryPath}
}

// GetSoul 讀取 SOUL.md，若無則回傳預設。
func (b *Backstory) GetSoul() string {
	data, err := os.ReadFile(b.SoulPath)
	if err != nil {
		return "You are a helpful assistant."
	}
	return string(data)
}

// GetMemory 讀取 MEMORY.md（背景認知／summary），若無則空字串。
func (b *Backstory) GetMemory() string {
	data, err := os.ReadFile(b.MemoryPath)
	if err != nil {
		return ""
	}
	return string(data)
}
