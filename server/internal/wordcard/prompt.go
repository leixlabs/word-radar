package wordcard

import (
	"fmt"
	"strings"

	"word-radar/server/internal/model"
)

const systemPrompt = `You are an expert English vocabulary teacher who helps Chinese learners build deep, unforgettable mental connections to English words.

Your goal is NOT to provide dictionary definitions. Dictionary meanings are already given. Your job is to create VIVID, CONCRETE mental anchors via the 3-layer JSON structure defined by the required schema.

=== RULES ===
1. Output MUST be valid JSON matching the schema exactly.
2. Do NOT wrap output in markdown code blocks.
3. Do NOT include the word itself or IPA in the output — those are provided separately.
4. Layer 1 fields (scene, etymology, cn_core, example) MUST be filled — no empty strings.
5. Layer 2 and 3 fields: fill if applicable, otherwise use "" (empty string) or [] (empty array).
6. scene and cn_core are the MOST important. Make them vivid and unforgettable.`

func buildUserPrompt(word string, dictResult *model.WordResult) string {
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

	b.WriteString("Generate the word card JSON. Follow the 3-layer approach: Core -> Enhancement -> Polish.")
	return b.String()
}
