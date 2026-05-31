package obsidian

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"word-radar/server/internal/config"
	"word-radar/server/internal/model"
)

// Generator Obsidian 文件生成器
type Generator struct {
	cfg *config.ObsidianConfig
}

// NewGenerator 创建生成器
func NewGenerator(cfg *config.ObsidianConfig) *Generator {
	return &Generator{cfg: cfg}
}

// GenerateDailyNote 生成/追加当日 Markdown 文件
func (g *Generator) GenerateDailyNote(records []model.WordRecord) (string, error) {
	if len(records) == 0 {
		return "", nil
	}

	today := time.Now().Format("2006-01-02")
	filename := strings.ReplaceAll(g.cfg.DailyNoteFormat, "{date}", today)
	fullPath := filepath.Join(g.cfg.VaultPath, g.cfg.WordsDir, filename)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	// 读取当日文件已有内容（如果存在）
	var existingContent string
	if data, err := os.ReadFile(fullPath); err == nil {
		existingContent = string(data)
	}

	// 提取当日文件中已存在的单词，避免追加重复
	existingWords := extractWordsFromMarkdown(existingContent)

	var newRecords []model.WordRecord
	for _, r := range records {
		if !existingWords[r.Word] {
			newRecords = append(newRecords, r)
		}
	}
	if len(newRecords) == 0 {
		return "", nil
	}

	// 构建 Markdown 内容
	var sb strings.Builder
	if existingContent == "" {
		// 新文件：写 frontmatter
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("date: %s\n", today))
		sb.WriteString(fmt.Sprintf("total_words: %d\n", len(newRecords)))
		sb.WriteString("source: Word Radar\n")
		sb.WriteString("---\n\n")
		sb.WriteString(fmt.Sprintf("# Word Radar - %s\n\n", today))
	} else {
		sb.WriteString("\n\n")
	}

	for _, r := range newRecords {
		sb.WriteString(fmt.Sprintf("## %s\n", r.Word))
		if r.Phonetic != "" {
			sb.WriteString(fmt.Sprintf("- **音标**: %s\n", r.Phonetic))
		}

		// 解析 meanings JSON
		if r.Meanings != "" {
			var meanings []model.Meaning
			if err := json.Unmarshal([]byte(r.Meanings), &meanings); err == nil {
				for _, m := range meanings {
					pos := m.PartOfSpeech
					if pos == "" {
						pos = "释义"
					}
					for _, def := range m.Definitions {
						sb.WriteString(fmt.Sprintf("- **%s**: %s\n", pos, def))
					}
				}
			}
		}

		// 例句
		if r.Examples != "" {
			var examples []string
			if err := json.Unmarshal([]byte(r.Examples), &examples); err == nil && len(examples) > 0 {
				sb.WriteString("- **例句**: ")
				sb.WriteString(examples[0])
				sb.WriteString("\n")
			}
		}

		// 上下文
		if r.Context != "" {
			sb.WriteString(fmt.Sprintf("- **上下文**: %s\n", r.Context))
		}

		// 来源
		if r.URL != "" {
			title := r.Title
			if title == "" {
				title = r.URL
			}
			sb.WriteString(fmt.Sprintf("- **来源**: [%s](%s)\n", title, r.URL))
		}

		sb.WriteString(fmt.Sprintf("- **时间**: %s\n", r.CreatedAt.Format("15:04")))
		sb.WriteString("\n---\n\n")
	}

	// 写入文件
	if existingContent == "" {
		if err := os.WriteFile(fullPath, []byte(sb.String()), 0644); err != nil {
			return "", fmt.Errorf("write file: %w", err)
		}
	} else {
		f, err := os.OpenFile(fullPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return "", fmt.Errorf("open file: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString(sb.String()); err != nil {
			return "", fmt.Errorf("append file: %w", err)
		}
	}

	return fullPath, nil
}

// extractWordsFromMarkdown 从 Markdown 内容中提取已有的单词（## word 格式）
func extractWordsFromMarkdown(content string) map[string]bool {
	words := make(map[string]bool)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			word := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			if word != "" {
				words[word] = true
			}
		}
	}
	return words
}
