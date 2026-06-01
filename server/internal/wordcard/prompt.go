package wordcard

import (
	"fmt"
	"strings"

	"word-radar/server/internal/model"
)

// buildUserPrompt 构建用户提示词。
// 词典数据（IPA, meanings, examples）注入作为上下文。
// 末尾层级提示由配置中的 fields layer 分组动态生成。
func (s *Service) buildUserPrompt(word string, dictResult *model.WordResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Analyze: \"%s\"\n\n", word)

	if dictResult != nil {
		if dictResult.Phonetic != "" {
			fmt.Fprintf(&b, "IPA: %s\n", dictResult.Phonetic)
		}
		if len(dictResult.Meanings) > 0 {
			b.WriteString("Dictionary meanings:\n")
			for _, m := range dictResult.Meanings {
				if m.PartOfSpeech != "" {
					fmt.Fprintf(&b, "  [%s] ", m.PartOfSpeech)
				}
				for i, d := range m.Definitions {
					if i > 0 {
						b.WriteString("; ")
					}
					b.WriteString(d)
				}
				b.WriteString("\n")
			}
		}
		if len(dictResult.Examples) > 0 {
			b.WriteString("Dictionary examples:\n")
			for _, ex := range dictResult.Examples {
				fmt.Fprintf(&b, "  - %s\n", ex)
			}
		}
		b.WriteString("\n")
	}

	// 动态生成层级提示
	b.WriteString("Generate the word card JSON. Layer approach: ")
	b.WriteString(s.buildLayerHint())
	return b.String()
}

// buildLayerHint 根据配置中的 fields layer 分组生成层级提示字符串。
// 如: "Core (imagery, breakdown, etymology, cn_core, example, simple_english) -> Enhancement (contrast, word_family) -> Polish (pronunciation_trap, memory_hook, register)"
func (s *Service) buildLayerHint() string {
	// 收集每个 layer 的字段 key
	layers := make(map[string][]string)
	layerOrder := []string{"core", "enhancement", "polish"}

	for _, f := range s.wcCfg.Fields {
		layers[f.Layer] = append(layers[f.Layer], f.Key)
	}

	var parts []string
	for _, layerName := range layerOrder {
		keys, ok := layers[layerName]
		if !ok || len(keys) == 0 {
			continue
		}
		capitalized := strings.ToUpper(layerName[:1]) + layerName[1:]
		parts = append(parts, fmt.Sprintf("%s (%s)", capitalized, strings.Join(keys, ", ")))
	}

	// 兜底：layerOrder 中未列出的 layer
	for layerName, keys := range layers {
		found := false
		for _, lo := range layerOrder {
			if lo == layerName {
				found = true
				break
			}
		}
		if found {
			continue
		}
		capitalized := strings.ToUpper(layerName[:1]) + layerName[1:]
		parts = append(parts, fmt.Sprintf("%s (%s)", capitalized, strings.Join(keys, ", ")))
	}

	return strings.Join(parts, " -> ")
}
