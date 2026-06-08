package wordcard

import (
	"fmt"
	"strings"
)

// buildUserPrompt 构建用户提示词。
// 只传单词和用户查词的上下文场景（不传词典数据），
// LLM 从自身知识出发生成记忆增强内容（词源、拆解、助记等）。
func (s *Service) buildUserPrompt(word string, context, url, title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Analyze: \"%s\"\n", word)

	// 提供用户的查词上下文（哪里看到、什么场景）
	hasContext := false
	if context != "" {
		hasContext = true
		b.WriteString("\nUser's lookup context:\n")
		fmt.Fprintf(&b, "  Text: %s\n", context)
		if url != "" {
			fmt.Fprintf(&b, "  URL: %s\n", url)
		}
		if title != "" {
			fmt.Fprintf(&b, "  Title: %s\n", title)
		}
	}

	if !hasContext {
		b.WriteString("\n(No specific context — the user looked up this word.)\n")
	}

	b.WriteString("\n")
	b.WriteString("Generate the word card JSON. Layer approach: ")
	b.WriteString(s.buildLayerHint())
	return b.String()
}

// buildLayerHint 根据配置中的 fields layer 分组生成层级提示字符串。
func (s *Service) buildLayerHint() string {
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
