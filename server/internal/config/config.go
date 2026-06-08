package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Obsidian ObsidianConfig `yaml:"obsidian"`
	Dict     DictConfig     `yaml:"dict"`
	LLM      LLMConfig      `yaml:"llm"`
	WordCard WordCardConfig `yaml:"wordcard"`
	DataDir  string         `yaml:"dataDir"`
}

type ServerConfig struct {
	Port string `yaml:"port"`
}

type ObsidianConfig struct {
	VaultPath       string `yaml:"vaultPath"`
	WordsDir        string `yaml:"wordsDir"`
	DailyNoteFormat string `yaml:"dailyNoteFormat"`
}

type DictConfig struct {
	CacheTTL durationOrZero `yaml:"cacheTTL"`
}

// durationOrZero extends time.Duration to accept "0" as a valid YAML / env value.
// "0" without a unit suffix is treated as 0 (meaning "never expire").
type durationOrZero time.Duration

// UnmarshalYAML handles both standard duration strings ("168h", "0s") and bare "0".
func (d *durationOrZero) UnmarshalYAML(value *yaml.Node) error {
	// 1. Try string value
	var s string
	if err := value.Decode(&s); err == nil {
		if s == "0" {
			*d = 0
			return nil
		}
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid cacheTTL %q: %w", s, err)
		}
		*d = durationOrZero(parsed)
		return nil
	}
	// 2. Try bare integer (YAML: cacheTTL: 0)
	var i int64
	if err := value.Decode(&i); err == nil {
		*d = durationOrZero(i)
		return nil
	}
	return fmt.Errorf("cannot decode cacheTTL: must be a duration string (e.g. 168h, 0s) or integer 0")
}

func (d durationOrZero) AsDuration() time.Duration {
	return time.Duration(d)
}

type LLMConfig struct {
	Enabled     bool    `yaml:"enabled"`
	Provider    string  `yaml:"provider"`
	APIURL      string  `yaml:"apiUrl"`
	APIKey      string  `yaml:"apiKey"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
}

// WordCardConfig 单词卡生成配置。
// 修改 fields 即可增减/调整单词卡字段，无需改代码。
// fields 数组的顺序决定前端 Aspect 输出顺序。
type WordCardConfig struct {
	PromptVersion string          `yaml:"promptVersion"` // LLM 缓存版本 key
	SchemaVersion string          `yaml:"schemaVersion"` // Schema 版本，变更时旧缓存失效
	SystemPrompt  string          `yaml:"systemPrompt"`  // LLM system prompt
	Fields        []WordCardField `yaml:"fields"`        // 字段定义（顺序 = 输出顺序）
}

// WordCardField 单个单词卡字段定义
type WordCardField struct {
	Key         string `yaml:"key"`         // 稳定标识符，如 "imagery", "breakdown"
	Label       string `yaml:"label"`       // 中文展示标签
	Icon        string `yaml:"icon"`        // emoji 图标
	Layer       string `yaml:"layer"`       // "core" | "enhancement" | "polish"
	Type        string `yaml:"type"`        // "string" | "array"
	Required    bool   `yaml:"required"`    // JSON Schema required
	Description string `yaml:"description"` // 字段描述（送入 LLM JSON Schema）
}

// Load 加载配置：先读配置文件（如果存在），再用环境变量覆盖
func Load(configPath string) *Config {
	cfg := defaultConfig()

	// 尝试读取配置文件
	path := configPath
	if path == "" {
		path = "config.yaml"
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(data, cfg)
	}

	// 环境变量覆盖
	if v := os.Getenv("SERVER_PORT"); v != "" {
		cfg.Server.Port = v
	}
	if v := os.Getenv("OBSIDIAN_VAULT_PATH"); v != "" {
		cfg.Obsidian.VaultPath = v
	}
	if v := os.Getenv("OBSIDIAN_WORDS_DIR"); v != "" {
		cfg.Obsidian.WordsDir = v
	}
	if v := os.Getenv("OBSIDIAN_DAILY_FORMAT"); v != "" {
		cfg.Obsidian.DailyNoteFormat = v
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("DICT_CACHE_TTL"); v != "" {
		// Handle bare "0" (without unit suffix)
		if v == "0" {
			cfg.Dict.CacheTTL = 0
		} else if d, err := time.ParseDuration(v); err == nil {
			cfg.Dict.CacheTTL = durationOrZero(d)
		}
	}
	if v := os.Getenv("LLM_ENABLED"); v != "" {
		cfg.LLM.Enabled, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("LLM_API_URL"); v != "" {
		cfg.LLM.APIURL = v
	}
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("LLM_TEMPERATURE"); v != "" {
		if t, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.LLM.Temperature = t
		}
	}

	return cfg
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port: "8787",
		},
		Obsidian: ObsidianConfig{
			VaultPath:       "/Users/lei/dev/ob",
			WordsDir:        "英语学习/words",
			DailyNoteFormat: "Word Radar - {date}.md",
		},
		Dict: DictConfig{
			CacheTTL: durationOrZero(168 * time.Hour),
		},
		LLM: LLMConfig{
			Enabled:     false,
			Provider:    "openai",
			APIURL:      "http://localhost:4000",
			APIKey:      "",
			Model:       "gpt-4o-mini",
			Temperature: 1.0,
		},
		DataDir: "./data",
	}
}
