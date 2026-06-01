package wordcard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"word-radar/server/internal/config"
	"word-radar/server/internal/dict"
	"word-radar/server/internal/llm"
	"word-radar/server/internal/logger"
	"word-radar/server/internal/model"
	"word-radar/server/internal/storage"
)

const promptVersion = "v4-wordcard"

// Service 单词卡生成服务
// 架构上解耦：词典 API 提供基础数据，LLM 提供记忆增强数据，Service 负责组装
// dict APIs → IPA, meanings, examples (base layer)
// LLM → imagery, breakdown, etymology, cn_core, contrast, word_family, etc. (enhancement layer)
type Service struct {
	db         *storage.DB
	aggregator *dict.Aggregator
	client     *llm.Client
	cfg        config.LLMConfig
	log        *slog.Logger
}

// NewService 创建单词卡服务
func NewService(db *storage.DB, aggregator *dict.Aggregator, cfg config.LLMConfig) *Service {
	var client *llm.Client
	if cfg.Enabled && cfg.APIURL != "" && cfg.APIKey != "" {
		client = llm.NewClient(cfg.APIURL, cfg.APIKey, cfg.Model, cfg.Temperature)
	}
	return &Service{
		db:         db,
		aggregator: aggregator,
		client:     client,
		cfg:        cfg,
		log:        logger.L(),
	}
}

// IsAvailable LLM 是否可用
func (s *Service) IsAvailable() bool {
	return s.client != nil
}

// Generate 生成单词卡
func (s *Service) Generate(word string) (*WordCard, error) {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil, fmt.Errorf("empty word")
	}

	// 1. 查词典
	dictResult, dictErr := s.aggregator.Lookup(word)
	if dictErr != nil {
		s.log.Warn("dict lookup failed, falling back to llm only",
			slog.String("word", word),
			slog.String("error", dictErr.Error()),
		)
		dictResult = &model.WordResult{Word: word}
	}

	// 2. 读 LLM 缓存
	var llmPart *LLMResult
	if s.IsAvailable() {
		cached, ok, err := s.db.GetLLMAnalysis(word, s.cfg.Provider, s.cfg.Model, promptVersion)
		if err != nil {
			s.log.Debug("wordcard llm cache read error",
				slog.String("word", word),
				slog.String("error", err.Error()),
			)
		} else if ok && cached.SchemaVersion == schemaVersion && cached.Result != "" {
			var cachedResult LLMResult
			if err := json.Unmarshal([]byte(cached.Result), &cachedResult); err == nil {
				s.log.Info("wordcard llm cache hit", slog.String("word", word))
				llmPart = &cachedResult
			}
		}
	}

	// 3. 无缓存则调用 LLM
	if llmPart == nil && s.IsAvailable() {
		s.log.Info("wordcard llm cache miss, calling llm", slog.String("word", word))

		start := time.Now()
		schema := BuildJSONSchema()
		raw, tokens, err := s.client.ChatCompletionWithSchema(systemPrompt, buildUserPrompt(word, dictResult), schema)
		elapsed := time.Since(start)

		if err != nil {
			s.log.Error("llm call failed",
				slog.String("word", word),
				slog.String("error", err.Error()),
			)
			return s.buildFallbackCard(word, dictResult), nil
		}

		s.log.Info("llm responded",
			slog.String("word", word),
			slog.Int("tokens", tokens),
			slog.Duration("elapsed", elapsed),
		)

		var result LLMResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			s.log.Error("parse llm response failed",
				slog.String("word", word),
				slog.String("raw", raw),
				slog.String("error", err.Error()),
			)
			return s.buildFallbackCard(word, dictResult), nil
		}
		llmPart = &result

		// 写入缓存
		resultJSON, _ := json.Marshal(result)
		expiresAt := time.Now().Add(30 * 24 * time.Hour).UTC()
		analysis := &model.LLMAnalysis{
			Word:          word,
			Provider:      s.cfg.Provider,
			Model:         s.cfg.Model,
			PromptVersion: promptVersion,
			SchemaVersion: schemaVersion,
			Result:        string(resultJSON),
			RawResponse:   raw,
			TokensUsed:    tokens,
			ExpiresAt:     &expiresAt,
		}
		if err := s.db.SaveLLMAnalysis(analysis); err != nil {
			s.log.Warn("save wordcard llm cache failed",
				slog.String("word", word),
				slog.String("error", err.Error()),
			)
		}
	}

	// 4. 组装最终卡片（含 aspects）
	card := s.buildCard(word, dictResult, llmPart)
	return card, nil
}

// aspectDef 定义后端已知的 aspect 元数据
// 新增 aspect: 在此 map 中添加一行，前端无需修改
type aspectDef struct {
	Label string
	Icon  string
	Layer string
}

var aspectMeta = map[string]aspectDef{
	// Core — 必填，决定"能否记住"
	"imagery":        {Label: "形象联想", Icon: "🎨", Layer: "core"},
	"breakdown":      {Label: "单词拆解", Icon: "🧩", Layer: "core"},
	"etymology":      {Label: "词源", Icon: "📜", Layer: "core"},
	"cn_core":        {Label: "核心释义", Icon: "💡", Layer: "core"},
	"example":        {Label: "例句", Icon: "📖", Layer: "core"},
	"simple_english": {Label: "简单解释", Icon: "📝", Layer: "core"},

	// Enhancement — 区分，防止混淆
	"contrast":    {Label: "易混对比", Icon: "⚡", Layer: "enhancement"},
	"word_family": {Label: "同根词", Icon: "🌳", Layer: "enhancement"},

	// Polish — 可选，加深记忆
	"pronunciation_trap": {Label: "发音提示", Icon: "🎯", Layer: "polish"},
	"memory_hook":        {Label: "记忆钩子", Icon: "🔗", Layer: "polish"},
	"register":           {Label: "使用语域", Icon: "🏷️", Layer: "polish"},
}

// aspectOrder 控制 aspect 输出顺序。aspectMeta 中但未列出的放在末尾。
var aspectOrder = []string{
	"imagery", "breakdown", "etymology", "cn_core", "example", "simple_english",
	"contrast", "word_family",
	"pronunciation_trap", "memory_hook", "register",
}

// buildCard 组装完整单词卡
// dict 数据注入 IPA
// LLM 数据映射为通用 aspects 数组
func (s *Service) buildCard(word string, dictResult *model.WordResult, llmPart *LLMResult) *WordCard {
	card := &WordCard{Word: word}

	if dictResult != nil {
		card.IPA = dictResult.Phonetic
		for _, src := range dictResult.Sources {
			card.Sources = append(card.Sources, src)
		}
	}

	if llmPart != nil {
		card.Sources = append(card.Sources, model.Source{
			Platform: s.cfg.Provider,
			Model:    s.cfg.Model,
		})

		// 按 order 迭代，构建有序 aspects
		seen := make(map[string]bool)
		for _, key := range aspectOrder {
			val, vals, hasVal := s.extractAspectValue(llmPart, key)
			if !hasVal {
				continue
			}
			meta := aspectMeta[key]
			card.Aspects = append(card.Aspects, Aspect{
				Key:    key,
				Label:  meta.Label,
				Icon:   meta.Icon,
				Value:  val,
				Values: vals,
				Layer:  meta.Layer,
			})
			seen[key] = true
		}

		// 兜底：aspectMeta 中有但 aspectOrder 遗漏的
		for key, meta := range aspectMeta {
			if seen[key] {
				continue
			}
			val, vals, hasVal := s.extractAspectValue(llmPart, key)
			if !hasVal {
				continue
			}
			card.Aspects = append(card.Aspects, Aspect{
				Key:    key,
				Label:  meta.Label,
				Icon:   meta.Icon,
				Value:  val,
				Values: vals,
				Layer:  meta.Layer,
			})
		}
	}

	// 词典例句：只有 LLM 没有提供 example aspect 时，使用词典例句作为降级
	if len(card.Aspects) == 0 || !s.hasAspectKey(card.Aspects, "example") {
		if dictResult != nil && len(dictResult.Examples) > 0 {
			meta := aspectMeta["example"]
			card.Aspects = append(card.Aspects, Aspect{
				Key:   "example",
				Label: meta.Label,
				Icon:  meta.Icon,
				Value: fmt.Sprintf("\"%s\"", dictResult.Examples[0]),
				Layer: meta.Layer,
			})
		}
	}

	return card
}

// extractAspectValue 从 LLMResult 中提取指定 key 的值
func (s *Service) extractAspectValue(llmPart *LLMResult, key string) (val string, vals []string, hasVal bool) {
	switch key {
	case "imagery":
		if llmPart.Imagery != "" {
			return llmPart.Imagery, nil, true
		}
	case "breakdown":
		if llmPart.Breakdown != "" {
			return llmPart.Breakdown, nil, true
		}
	case "etymology":
		if llmPart.Etymology != "" {
			return llmPart.Etymology, nil, true
		}
	case "cn_core":
		if llmPart.CNCore != "" {
			return llmPart.CNCore, nil, true
		}
	case "example":
		if llmPart.Example != "" {
			return llmPart.Example, nil, true
		}
	case "simple_english":
		if llmPart.SimpleEnglish != "" {
			return llmPart.SimpleEnglish, nil, true
		}
	case "contrast":
		if llmPart.Contrast != "" {
			return llmPart.Contrast, nil, true
		}
	case "word_family":
		if len(llmPart.WordFamily) > 0 {
			return "", llmPart.WordFamily, true
		}
	case "pronunciation_trap":
		if llmPart.PronunciationTrap != "" {
			return llmPart.PronunciationTrap, nil, true
		}
	case "memory_hook":
		if llmPart.MemoryHook != "" {
			return llmPart.MemoryHook, nil, true
		}
	case "register":
		if llmPart.Register != "" {
			return llmPart.Register, nil, true
		}
	}
	return "", nil, false
}

// hasAspectKey 检查 aspects 数组中是否已存在指定 key
func (s *Service) hasAspectKey(aspects []Aspect, key string) bool {
	for _, a := range aspects {
		if a.Key == key {
			return true
		}
	}
	return false
}

// BuildFallbackCard 构建纯词典降级卡片（无 LLM 时），导出供 handler 使用
func BuildFallbackCard(dictResult *model.WordResult) *WordCard {
	word := ""
	if dictResult != nil {
		word = dictResult.Word
	}
	card := &WordCard{Word: word}
	if dictResult != nil {
		card.IPA = dictResult.Phonetic
		if len(dictResult.Examples) > 0 {
			meta := aspectMeta["example"]
			card.Aspects = append(card.Aspects, Aspect{
				Key:   "example",
				Label: meta.Label,
				Icon:  meta.Icon,
				Value: fmt.Sprintf("\"%s\"", dictResult.Examples[0]),
				Layer: meta.Layer,
			})
		}
		for _, src := range dictResult.Sources {
			card.Sources = append(card.Sources, src)
		}
	}
	return card
}

// buildFallbackCard 纯词典降级卡片（无 LLM 时）— 内部使用
func (s *Service) buildFallbackCard(word string, dictResult *model.WordResult) *WordCard {
	card := &WordCard{Word: word}
	if dictResult != nil {
		card.IPA = dictResult.Phonetic
		if len(dictResult.Examples) > 0 {
			meta := aspectMeta["example"]
			card.Aspects = append(card.Aspects, Aspect{
				Key:   "example",
				Label: meta.Label,
				Icon:  meta.Icon,
				Value: fmt.Sprintf("\"%s\"", dictResult.Examples[0]),
				Layer: meta.Layer,
			})
		}
		for _, src := range dictResult.Sources {
			card.Sources = append(card.Sources, src)
		}
	}
	return card
}
