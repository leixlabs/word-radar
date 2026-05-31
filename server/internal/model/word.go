package model

import "time"

// Meaning 表示一个释义
type Meaning struct {
	PartOfSpeech string   `json:"partOfSpeech"`
	Definitions  []string `json:"definitions"`
}

// Source 表示数据来源
type Source struct {
	Platform string `json:"platform"`
	Model    string `json:"model"`
}

// WordResult 聚合后的查词结果
type WordResult struct {
	Word        string    `json:"word"`
	Phonetic    string    `json:"phonetic"`
	Meanings    []Meaning `json:"meanings"`
	Examples    []string  `json:"examples"`
	AudioURL    string    `json:"audio_url"`
	YouGlishURL string    `json:"youglish_url"`
	Sources     []Source  `json:"sources"`
}

// WordRecord 单词聚合记录（words 表，一个单词一条）
type WordRecord struct {
	Word      string     `json:"word"`
	Phonetic  string     `json:"phonetic"`
	Meanings  string     `json:"meanings"` // JSON
	Examples  string     `json:"examples"` // JSON
	Sources   string     `json:"sources"`  // JSON
	Context   string     `json:"context"`
	URL       string     `json:"url"`
	Title     string     `json:"title"`
	CreatedAt time.Time  `json:"created_at"`
	SyncedAt  *time.Time `json:"synced_at"`
}

// LookupHistory 单次查询历史（word_lookups 表）
type LookupHistory struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	Context   string    `json:"context"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// LLMAnalysis LLM 单词拆解
type LLMAnalysis struct {
	ID            int64      `json:"id"`
	Word          string     `json:"word"`
	Provider      string     `json:"provider"`
	Model         string     `json:"model"`
	PromptVersion string     `json:"prompt_version"`
	SchemaVersion string     `json:"schema_version"` // schema 版本，独立于 prompt 版本
	Result        string     `json:"result"`         // JSON
	RawResponse   string     `json:"raw_response"`
	TokensUsed    int        `json:"tokens_used"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at"`
}
