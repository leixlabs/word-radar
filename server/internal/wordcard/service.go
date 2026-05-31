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

const promptVersion = "v3-wordcard"

// Service 单词卡生成服务
// 架构上解耦：词典 API 提供基础数据，LLM 提供记忆增强数据，Service 负责组装
// dict APIs → IPA, meanings, examples (base layer)
// LLM → scene, etymology, cn_core, contrast, word_family, memory_hook, etc. (enhancement layer)
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
// 1. 查词典获取基础数据（IPA、例句、释义）— dict APIs 负责
// 2. 查 LLM 缓存（word_analysis 表，与 etymology 共享存储表但 prompt_version 不同）
// 3. 如无缓存，调用 LLM 生成记忆增强数据（使用 JSON Schema 约束，保证输出结构化）
// 4. 组装为完整 WordCard
func (s *Service) Generate(word string) (*WordCard, error) {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil, fmt.Errorf("empty word")
	}

	// 1. 查词典（允许失败，降级为仅 LLM 生成）
	dictResult, dictErr := s.aggregator.Lookup(word)
	if dictErr != nil {
		s.log.Warn("dict lookup failed, falling back to llm only",
			slog.String("word", word),
			slog.String("error", dictErr.Error()),
		)
		dictResult = &model.WordResult{Word: word}
	}

	// 2. 尝试读 LLM 缓存（复用 word_analysis 表）
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
			// LLM 失败但不阻断，尝试用纯词典数据生成降级卡片
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

		// 写入缓存（word_analysis 表，与 etymology 共用但 prompt_version 不同）
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

	// 4. 组装最终卡片
	card := s.buildCard(word, dictResult, llmPart)
	return card, nil
}

// buildCard 组装完整单词卡
// dict 数据注入 IPA、基本释义
// LLM 数据注入场景、词根、对比等记忆增强字段
func (s *Service) buildCard(word string, dictResult *model.WordResult, llmPart *LLMResult) *WordCard {
	card := &WordCard{Word: word}

	// 词典数据注入 — 基础层
	if dictResult != nil {
		card.IPA = dictResult.Phonetic
		if len(dictResult.Examples) > 0 {
			card.Example = dictResult.Examples[0]
		}
		for _, src := range dictResult.Sources {
			card.Sources = append(card.Sources, src)
		}
	}

	// LLM 数据注入 — 记忆增强层
	if llmPart != nil {
		card.Scene = llmPart.Scene
		card.Etymology = llmPart.Etymology
		card.CNCore = llmPart.CNCore
		if llmPart.Example != "" {
			card.Example = llmPart.Example // LLM 例句优先
		}
		if llmPart.Contrast != "" {
			card.Contrast = llmPart.Contrast
		}
		if len(llmPart.WordFamily) > 0 {
			card.WordFamily = llmPart.WordFamily
		}
		if llmPart.PronunciationTrap != "" {
			card.PronunciationTrap = llmPart.PronunciationTrap
		}
		if llmPart.MemoryHook != "" {
			card.MemoryHook = llmPart.MemoryHook
		}
		if llmPart.Register != "" {
			card.Register = llmPart.Register
		}
		card.Sources = append(card.Sources, model.Source{
			Platform: s.cfg.Provider,
			Model:    s.cfg.Model,
		})
	}

	return card
}

// buildFallbackCard 纯词典降级卡片（无 LLM 时）
func (s *Service) buildFallbackCard(word string, dictResult *model.WordResult) *WordCard {
	card := &WordCard{Word: word}
	if dictResult != nil {
		card.IPA = dictResult.Phonetic
		if len(dictResult.Examples) > 0 {
			card.Example = dictResult.Examples[0]
		}
		for _, src := range dictResult.Sources {
			card.Sources = append(card.Sources, src)
		}
	}
	return card
}
