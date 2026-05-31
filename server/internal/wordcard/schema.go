package wordcard

import (
	"word-radar/server/internal/llm"
	"word-radar/server/internal/model"
)

// schemaVersion 当前 LLMResult 的 schema 版本。
// 修改 LLMResult 结构体或 BuildJSONSchema 时递增此值，
// 以使旧缓存失效（避免 schema 不匹配）。
const schemaVersion = "v1"

// WordCard 单词卡完整模型 — 3 层属性结构
//
// Layer 1 — Core（必填，决定"能否记住"）:
//   word, ipa, scene, etymology, cn_core, example
//
// Layer 2 — Enhancement（区分，防止混淆）:
//   contrast, word_family
//
// Layer 3 — Polish（可选，加深记忆）:
//   pronunciation_trap, memory_hook, register
type WordCard struct {
	// Core — 必填（来自 dict + LLM）
	Word      string `json:"word"`
	IPA       string `json:"ipa"`
	Scene     string `json:"scene"`
	Etymology string `json:"etymology"`
	CNCore    string `json:"cn_core"`
	Example   string `json:"example"`

	// Enhancement — 按需
	Contrast   string   `json:"contrast,omitempty"`
	WordFamily []string `json:"word_family,omitempty"`

	// Polish — 可选
	PronunciationTrap string `json:"pronunciation_trap,omitempty"`
	MemoryHook        string `json:"memory_hook,omitempty"`
	Register          string `json:"register,omitempty"`

	// Meta
	Sources []model.Source `json:"sources"`
}

// LLMResult LLM 生成的单词卡片段（与词典数据解耦）
// 仅包含 LLM 产出的记忆增强字段。word/ipa/词典释义由上层从 dict API 注入。
//
// Core: scene, etymology, cn_core, example — 必须填满
// Enhancement: contrast, word_family — 按需
// Polish: pronunciation_trap, memory_hook, register — 可选
type LLMResult struct {
	// Core — 必须非空
	Scene     string `json:"scene"`
	Etymology string `json:"etymology"`
	CNCore    string `json:"cn_core"`
	Example   string `json:"example"`

	// Enhancement — 没有则 "" 或 []
	Contrast   string   `json:"contrast"`
	WordFamily []string `json:"word_family"`

	// Polish — 没有则 ""
	PronunciationTrap string `json:"pronunciation_trap"`
	MemoryHook        string `json:"memory_hook"`
	Register          string `json:"register"`
}

// BuildJSONSchema 构建 OpenAI JSON Schema 定义
// 通过 LLM API 的 response_format: json_schema 强制约束输出结构。
// 所有字段都是 required（即便可选的返回空值），保证 JSON 结构一致性。
func BuildJSONSchema() llm.JSONSchema {
	return llm.JSONSchema{
		Name:   "word_card",
		Strict: true,
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				// === Layer 1: Core (must be non-empty) ===
				"scene": map[string]interface{}{
					"type":        "string",
					"description": "核心动态画面。用中文描述这个单词唤起什么动作/场景，必须有画面感。如 '双手合握抓住全部碎片'",
				},
				"etymology": map[string]interface{}{
					"type":        "string",
					"description": "词根拆解骨架。格式: '前缀 (意思) + 词根 (意思)'。如 'com- (一起) + prehendere (抓住)'",
				},
				"cn_core": map[string]interface{}{
					"type":        "string",
					"description": "灵魂翻译。不是词典释义，是基于 scene 的场景翻译。4-6字，简洁有力。如 '合掌抓住全部'",
				},
				"example": map[string]interface{}{
					"type":        "string",
					"description": "一句自然的上下文例句，带英文引号。让学习者一看就懂用法。如 '\"I finally comprehend the structure.\"'",
				},
				// === Layer 2: Enhancement (fill if applicable) ===
				"contrast": map[string]interface{}{
					"type":        "string",
					"description": "与易混淆词对比，磨锐含义边界。格式: 'X (特征A) ≠ Y (特征B)'。如 'comprehend (主动抓) ≠ understand (被动覆盖)'。没有则空字符串",
				},
				"word_family": map[string]interface{}{
					"type":        "array",
					"description": "同根词列表，帮助扩展词汇。如 ['apprehend', 'comprehensive', 'prehensile']。没有则空数组",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				// === Layer 3: Polish (optional) ===
				"pronunciation_trap": map[string]interface{}{
					"type":        "string",
					"description": "中文使用者常见发音错误。只有容易读错的词才填。如 '重音在 -HEND, 不是 com-'。没有则空字符串",
				},
				"memory_hook": map[string]interface{}{
					"type":        "string",
					"description": "记忆钩子，绑定已有知识（影视、编程、游戏、小说等）。如 '韩立合掌抓丹方精髓'。没有好联想则空字符串",
				},
				"register": map[string]interface{}{
					"type":        "string",
					"description": "使用场合（正式/口语/书面/文学/俚语/古语）。只有明显偏某种语域才填。如 '偏书面/正式'。普通词空字符串",
				},
			},
			"required": []string{
				"scene", "etymology", "cn_core", "example",
				"contrast", "word_family", "pronunciation_trap", "memory_hook", "register",
			},
			"additionalProperties": false,
		},
	}
}
