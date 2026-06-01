package wordcard

import (
	"fmt"
	"strings"

	"word-radar/server/internal/model"
)

const systemPrompt = `You are an expert English vocabulary teacher who helps Chinese learners build deep, unforgettable mental connections to English words.

Your goal is NOT to provide dictionary definitions. Dictionary meanings are already given. Your job is to create VIVID, CONCRETE mental anchors via the JSON structure defined by the required schema.

=== RULES ===
1. Output MUST be valid JSON matching the schema exactly.
2. Do NOT wrap output in markdown code blocks.
3. Do NOT include the word itself or IPA in the output — those are provided separately.
4. Core fields (imagery, breakdown, etymology, cn_core, example, simple_english) MUST be filled — no empty strings.
5. Enhancement fields (contrast, word_family): fill if applicable, otherwise "" or [].
6. Polish fields (pronunciation_trap, memory_hook, register): fill if applicable, otherwise "".
7. imagery and cn_core are the MOST important — make them vivid and unforgettable.
8. breakdown focuses on word structure (prefix + root + suffix).
9. etymology focuses on word origin/story (historical evolution).
10. simple_english uses basic vocabulary a learner would understand.`

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

	b.WriteString("Generate the word card JSON. Layer approach: Core (imagery, breakdown, etymology, cn_core, example, simple_english) -> Enhancement (contrast, word_family) -> Polish (pronunciation_trap, memory_hook, register).")
	return b.String()
}
