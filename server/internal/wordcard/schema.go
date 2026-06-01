package wordcard

import (
	"word-radar/server/internal/config"
	"word-radar/server/internal/llm"
	"word-radar/server/internal/model"
)

// ==== Generic Aspects Model ====
//
// 前端不需要知道有哪些 aspect。后端定义 key/label/icon/layer，
// 前端只负责迭代 aspects 并渲染。新增 aspect 只需改配置。
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

// LLMResult LLM 返回的单词卡数据。
// 使用 map 而非固定 struct，字段完全由配置驱动。
// 新增/删除/修改字段只需改 config.yaml，无需改代码。
type LLMResult map[string]interface{}

// BuildJSONSchema 根据 WordCardConfig 构建 OpenAI JSON Schema。
// 字段定义（type, description, required）完全由配置驱动。
func BuildJSONSchema(cfg config.WordCardConfig) llm.JSONSchema {
	properties := make(map[string]interface{})
	required := make([]string, 0)

	for _, f := range cfg.Fields {
		prop := map[string]interface{}{
			"type":        f.Type,
			"description": f.Description,
		}
		if f.Type == "array" {
			prop["items"] = map[string]interface{}{"type": "string"}
		}
		properties[f.Key] = prop
		if f.Required {
			required = append(required, f.Key)
		}
	}

	return llm.JSONSchema{
		Name:   "word_card",
		Strict: true,
		Schema: map[string]interface{}{
			"type":                 "object",
			"properties":           properties,
			"required":             required,
			"additionalProperties": false,
		},
	}
}

// extractAspectValue 从 LLMResult map 中提取指定 key 的值。
// 支持 string 类型和 []interface{} (array) 类型。
func extractAspectValue(llmPart LLMResult, key string, fieldType string) (val string, vals []string, hasVal bool) {
	raw, ok := llmPart[key]
	if !ok {
		return "", nil, false
	}

	switch fieldType {
	case "string":
		if s, ok := raw.(string); ok && s != "" {
			return s, nil, true
		}
	case "array":
		arr, ok := raw.([]interface{})
		if !ok || len(arr) == 0 {
			return "", nil, false
		}
		strs := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				strs = append(strs, s)
			}
		}
		if len(strs) > 0 {
			return "", strs, true
		}
	}
	return "", nil, false
}

// findField 在字段列表中查找指定 key 的字段定义。
// 返回 nil 表示未找到。
func findField(fields []config.WordCardField, key string) *config.WordCardField {
	for i := range fields {
		if fields[i].Key == key {
			return &fields[i]
		}
	}
	return nil
}

// hasAspectKey 检查 aspects 数组中是否已存在指定 key
func hasAspectKey(aspects []Aspect, key string) bool {
	for _, a := range aspects {
		if a.Key == key {
			return true
		}
	}
	return false
}
