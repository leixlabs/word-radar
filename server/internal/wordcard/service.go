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

// Service 单词卡生成服务
// 架构上解耦：词典 API 提供基础数据，LLM 提供记忆增强数据，Service 负责组装
// dict APIs → IPA, meanings, examples (base layer)
// LLM → imagery, breakdown, etymology, cn_core, contrast, word_family, etc. (enhancement layer)
//
// 字段定义（key/label/icon/layer/type/description）完全由配置驱动。
// 新增/删除/修改字段只需改 config.yaml 中的 wordcard.fields，无需改代码。
type Service struct {
	db         *storage.DB
	aggregator *dict.Aggregator
	client     *llm.Client
	cfg        config.LLMConfig
	wcCfg      config.WordCardConfig
	log        *slog.Logger
}

// NewService 创建单词卡服务
func NewService(db *storage.DB, aggregator *dict.Aggregator, llmCfg config.LLMConfig, wcCfg config.WordCardConfig) *Service {
	var client *llm.Client
	if llmCfg.Enabled && llmCfg.APIURL != "" && llmCfg.APIKey != "" {
		client = llm.NewClient(llmCfg.APIURL, llmCfg.APIKey, llmCfg.Model, llmCfg.Temperature)
	}
	return &Service{
		db:         db,
		aggregator: aggregator,
		client:     client,
		cfg:        llmCfg,
		wcCfg:      wcCfg,
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
	var llmPart LLMResult
	if s.IsAvailable() {
		cached, ok, err := s.db.GetLLMAnalysis(word, s.cfg.Provider, s.cfg.Model, s.wcCfg.PromptVersion)
		if err != nil {
			s.log.Debug("wordcard llm cache read error",
				slog.String("word", word),
				slog.String("error", err.Error()),
			)
		} else if ok && cached.SchemaVersion == s.wcCfg.SchemaVersion && cached.Result != "" {
			var cachedResult LLMResult
			if err := json.Unmarshal([]byte(cached.Result), &cachedResult); err == nil {
				s.log.Info("wordcard llm cache hit", slog.String("word", word))
				llmPart = cachedResult
			}
		}
	}

	// 3. 无缓存则调用 LLM
	if llmPart == nil && s.IsAvailable() {
		s.log.Info("wordcard llm cache miss, calling llm", slog.String("word", word))

		// 获取用户查词上下文（不传词典数据给 LLM）
		ctx, ctxURL, ctxTitle, _ := s.db.GetWordContext(word)

		start := time.Now()
		schema := BuildJSONSchema(s.wcCfg)
		raw, tokens, err := s.client.ChatCompletionWithSchema(s.wcCfg.SystemPrompt, s.buildUserPrompt(word, ctx, ctxURL, ctxTitle), schema)
		elapsed := time.Since(start)

		if err != nil {
			s.log.Error("llm call failed",
				slog.String("word", word),
				slog.String("error", err.Error()),
			)
			card := s.buildFallbackCard(word, dictResult)
			card.Warning = "AI 增强服务异常，仅显示词典释义。详情见服务端日志。"
			return card, nil
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
			card := s.buildFallbackCard(word, dictResult)
			card.Warning = "AI 增强数据解析异常，仅显示词典释义。"
			return card, nil
		}
		llmPart = result

		// 写入缓存
		resultJSON, _ := json.Marshal(result)
		expiresAt := time.Now().Add(30 * 24 * time.Hour).UTC()
		analysis := &model.LLMAnalysis{
			Word:          word,
			Provider:      s.cfg.Provider,
			Model:         s.cfg.Model,
			PromptVersion: s.wcCfg.PromptVersion,
			SchemaVersion: s.wcCfg.SchemaVersion,
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

// buildCard 组装完整单词卡
// dict 数据注入 IPA
// LLM 数据按配置 fields 顺序映射为通用 aspects 数组
func (s *Service) buildCard(word string, dictResult *model.WordResult, llmPart LLMResult) *WordCard {
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

		// 按配置 fields 顺序迭代，构建有序 aspects
		for _, f := range s.wcCfg.Fields {
			val, vals, hasVal := extractAspectValue(llmPart, f.Key, f.Type)
			if !hasVal {
				continue
			}
			card.Aspects = append(card.Aspects, Aspect{
				Key:    f.Key,
				Label:  f.Label,
				Icon:   f.Icon,
				Value:  val,
				Values: vals,
				Layer:  f.Layer,
			})
		}
	}

	// 词典例句：只有 LLM 没有提供 example aspect 时，使用词典例句作为降级
	if len(card.Aspects) == 0 || !hasAspectKey(card.Aspects, "example") {
		if dictResult != nil && len(dictResult.Examples) > 0 {
			if field := findField(s.wcCfg.Fields, "example"); field != nil {
				card.Aspects = append(card.Aspects, Aspect{
					Key:   "example",
					Label: field.Label,
					Icon:  field.Icon,
					Value: fmt.Sprintf("\"%s\"", dictResult.Examples[0]),
					Layer: field.Layer,
				})
			}
		}
	}

	return card
}

// BuildFallbackCard 构建纯词典降级卡片（无 LLM 时），导出供 handler 使用
func (s *Service) BuildFallbackCard(dictResult *model.WordResult) *WordCard {
	return s.buildFallbackCard("", dictResult)
}

// buildFallbackCard 纯词典降级卡片（无 LLM 时）— 内部使用
func (s *Service) buildFallbackCard(word string, dictResult *model.WordResult) *WordCard {
	if dictResult != nil && word == "" {
		word = dictResult.Word
	}
	card := &WordCard{Word: word}
	if dictResult != nil {
		card.IPA = dictResult.Phonetic
		if len(dictResult.Examples) > 0 {
			if field := findField(s.wcCfg.Fields, "example"); field != nil {
				card.Aspects = append(card.Aspects, Aspect{
					Key:   "example",
					Label: field.Label,
					Icon:  field.Icon,
					Value: fmt.Sprintf("\"%s\"", dictResult.Examples[0]),
					Layer: field.Layer,
				})
			}
		}
		for _, src := range dictResult.Sources {
			card.Sources = append(card.Sources, src)
		}
	}
	return card
}
