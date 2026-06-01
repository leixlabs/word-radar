package wordcard

import (
	"word-radar/server/internal/llm"
	"word-radar/server/internal/model"
)

// schemaVersion 当前 LLMResult 的 schema 版本。
// 修改 LLMResult 结构体或 BuildJSONSchema 时递增此值，
// 以使旧缓存失效（避免 schema 不匹配）。
const schemaVersion = "v2"

// ==== Generic Aspects Model ====
//
// 前端不需要知道有哪些 aspect。后端定义 key/label/icon/layer，
// 前端只负责迭代 aspects 并渲染。新增 aspect 只需改后端。
//
// Layer 分组语义:
//   core        — 必填，决定"能否记住"
//   enhancement — 区分，防止混淆
//   polish      — 可选，加深记忆

// Aspect 通用单词卡属性
type Aspect struct {
	Key    string   `json:"key"`              // 稳定标识符，如 "imagery", "breakdown"
	Label  string   `json:"label"`            // 中文展示标签
	Icon   string   `json:"icon"`             // emoji 图标
	Value  string   `json:"value,omitempty"`  // 单值字段
	Values []string `json:"values,omitempty"` // 多值字段（如 word_family）
	Layer  string   `json:"layer"`            // "core" | "enhancement" | "polish"
}

// WordCard 单词卡完整模型
// 前端通过 aspects 数组迭代渲染，不再依赖具体字段名。
type WordCard struct {
	Word    string         `json:"word"`
	IPA     string         `json:"ipa"`
	Aspects []Aspect       `json:"aspects"`
	Sources []model.Source `json:"sources"`
}

// LLMResult LLM 生成的单词卡片段（与词典数据解耦）
//
// imagery       — 场景/形象联想（取代旧 scene）
// breakdown     — 词根词缀拆解骨架
// etymology     — 词源故事（单词的起源/演变）
// cn_core       — 灵魂翻译
// example       — 上下文例句
// simple_english — 简单英语解释
// contrast      — 易混淆词对比
// word_family   — 同根词
// pronunciation_trap — 发音陷阱
// memory_hook   — 记忆钩子
// register      — 语域
type LLMResult struct {
	Imagery       string   `json:"imagery"`
	Breakdown     string   `json:"breakdown"`
	Etymology     string   `json:"etymology"`
	CNCore        string   `json:"cn_core"`
	Example       string   `json:"example"`
	SimpleEnglish string   `json:"simple_english"`
	Contrast      string   `json:"contrast"`
	WordFamily    []string `json:"word_family"`

	PronunciationTrap string `json:"pronunciation_trap"`
	MemoryHook        string `json:"memory_hook"`
	Register          string `json:"register"`
}

// BuildJSONSchema 构建 OpenAI JSON Schema 定义
func BuildJSONSchema() llm.JSONSchema {
	return llm.JSONSchema{
		Name:   "word_card",
		Strict: true,
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				// === Core ===
				"imagery": map[string]interface{}{
					"type":        "string",
					"description": "核心形象联想。用中文描述这个单词唤起的动态画面/场景，必须有画面感。如 '双手合握抓住全部碎片'",
				},
				"breakdown": map[string]interface{}{
					"type":        "string",
					"description": "词根词缀拆解骨架。格式: '前缀 (意思) + 词根 (意思) [+ 后缀 (意思)]'。如 'com- (一起) + prehendere (抓住)'",
				},
				"etymology": map[string]interface{}{
					"type":        "string",
					"description": "词源故事。一句话说明这个单词的历史来源/演变过程。如 '来自拉丁语 comprehendere, 意为抓住、包含, 后演变为理解'",
				},
				"cn_core": map[string]interface{}{
					"type":        "string",
					"description": "灵魂翻译。不是词典释义，是基于 imagery 的场景翻译。4-6字，简洁有力。如 '合掌抓住全部'",
				},
				"example": map[string]interface{}{
					"type":        "string",
					"description": "一句自然的上下文例句，带英文引号。如 '\"I finally comprehend the structure.\"'",
				},
				"simple_english": map[string]interface{}{
					"type":        "string",
					"description": "用简单英语解释这个词（英语学习者友好，用基础词汇）。如 'to understand something completely, to grasp the meaning'",
				},
				// === Enhancement ===
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
				// === Polish ===
				"pronunciation_trap": map[string]interface{}{
					"type":        "string",
					"description": "中文使用者常见发音错误。如 '重音在 -HEND, 不是 com-'。没有则空字符串",
				},
				"memory_hook": map[string]interface{}{
					"type":        "string",
					"description": "记忆钩子，绑定已有知识（影视、编程、游戏、小说等）。如 '韩立合掌抓丹方精髓'。没有好联想则空字符串",
				},
				"register": map[string]interface{}{
					"type":        "string",
					"description": "使用场合（正式/口语/书面/文学/俚语/古语）。如 '偏书面/正式'。普通词空字符串",
				},
			},
			"required": []string{
				"imagery", "breakdown", "etymology", "cn_core", "example", "simple_english",
				"contrast", "word_family", "pronunciation_trap", "memory_hook", "register",
			},
			"additionalProperties": false,
		},
	}
}
