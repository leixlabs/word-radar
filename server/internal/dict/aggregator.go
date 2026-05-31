package dict

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"word-radar/server/internal/logger"
	"word-radar/server/internal/model"
	"word-radar/server/internal/storage"
)

// 内部统一格式，用于合并
type wordResultRaw struct {
	Word        string         `json:"word"`
	Phonetic    string         `json:"phonetic"`
	AudioURL    string         `json:"audio_url"`
	YouGlishURL string         `json:"youglish_url"`
	Meanings    []meaningEntry `json:"meanings"`
	Examples    []string       `json:"examples"`
	Sources     []sourceEntry  `json:"sources"`
}

type meaningEntry struct {
	PartOfSpeech string   `json:"partOfSpeech"`
	Definitions  []string `json:"definitions"`
}

type sourceEntry struct {
	Platform string `json:"platform"`
	Model    string `json:"model"`
}

// Aggregator 查词聚合器
type Aggregator struct {
	db  *storage.DB
	ttl time.Duration
	log *slog.Logger
}

// NewAggregator 创建聚合器
func NewAggregator(db *storage.DB, ttl time.Duration) *Aggregator {
	return &Aggregator{db: db, ttl: ttl, log: logger.L()}
}

// Lookup 查词，优先缓存，否则并发请求多个 API
func (a *Aggregator) Lookup(word string) (*model.WordResult, error) {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil, fmt.Errorf("empty word")
	}

	// 1. 分别检查缓存
	var cachedDict *model.WordResult
	for _, source := range []string{"dictionaryapi", "youdao"} {
		cached, ok, err := a.db.GetCache(word, source, "")
		if err != nil {
			a.log.Debug("cache read error", slog.String("word", word), slog.String("source", source), slog.String("error", err.Error()))
			continue
		}
		if ok {
			var raw wordResultRaw
			if err := json.Unmarshal([]byte(cached), &raw); err == nil {
				cachedDict = mergeResult(cachedDict, &raw)
			}
		}
	}
	if cachedDict != nil {
		a.log.Info("dict cache hit", slog.String("word", word))
		return cachedDict, nil
	}

	// 2. 并发请求
	var wg sync.WaitGroup
	var mu sync.Mutex
	var final *wordResultRaw
	var errs []error

	a.log.Info("dict cache miss, fetching apis", slog.String("word", word))

	wg.Add(2)
	go func() {
		defer wg.Done()
		r, err := FetchFromDictionaryAPI(word)
		if err != nil {
			a.log.Warn("dictionaryapi failed", slog.String("word", word), slog.String("error", err.Error()))
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		converted := r.ToWordResult()
		if converted != nil {
			a.cacheResult(word, "dictionaryapi", "", converted)
			mu.Lock()
			final = mergeRaw(final, converted)
			mu.Unlock()
			a.log.Debug("dictionaryapi ok", slog.String("word", word))
		}
	}()

	go func() {
		defer wg.Done()
		r, err := FetchFromYoudao(word)
		if err != nil {
			a.log.Warn("youdao failed", slog.String("word", word), slog.String("error", err.Error()))
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		converted := r.ToWordResult()
		if converted != nil {
			a.cacheResult(word, "youdao", "", converted)
			mu.Lock()
			final = mergeRaw(final, converted)
			mu.Unlock()
			a.log.Debug("youdao ok", slog.String("word", word))
		}
	}()

	wg.Wait()

	if final == nil {
		if len(errs) > 0 {
			return nil, fmt.Errorf("all dict apis failed: %v", errs)
		}
		return nil, fmt.Errorf("no result for %s", word)
	}

	a.log.Info("dict fetched", slog.String("word", word))
	return rawToModel(final), nil
}

func (a *Aggregator) cacheResult(word, source, model string, raw *wordResultRaw) {
	data, err := json.Marshal(raw)
	if err != nil {
		return
	}
	_ = a.db.SetCache(word, source, model, string(data), a.ttl)
}

func mergeRaw(a, b *wordResultRaw) *wordResultRaw {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	// 合并 phonetic：优先 dictionaryapi 的 IPA
	if a.Phonetic == "" {
		a.Phonetic = b.Phonetic
	}
	if a.AudioURL == "" {
		a.AudioURL = b.AudioURL
	}
	// 合并 meanings（简单追加，去重由调用方处理）
	a.Meanings = append(a.Meanings, b.Meanings...)
	// 合并 examples
	a.Examples = append(a.Examples, b.Examples...)
	// 合并 sources
	a.Sources = append(a.Sources, b.Sources...)
	return a
}

func mergeResult(m *model.WordResult, raw *wordResultRaw) *model.WordResult {
	if m == nil {
		return rawToModel(raw)
	}
	if raw == nil {
		return m
	}
	if m.Phonetic == "" {
		m.Phonetic = raw.Phonetic
	}
	if m.AudioURL == "" {
		m.AudioURL = raw.AudioURL
	}
	for _, mm := range raw.Meanings {
		m.Meanings = append(m.Meanings, model.Meaning{
			PartOfSpeech: mm.PartOfSpeech,
			Definitions:  mm.Definitions,
		})
	}
	m.Examples = append(m.Examples, raw.Examples...)
	for _, s := range raw.Sources {
		m.Sources = append(m.Sources, model.Source{Platform: s.Platform, Model: s.Model})
	}
	return m
}

func rawToModel(r *wordResultRaw) *model.WordResult {
	m := &model.WordResult{
		Word:        r.Word,
		Phonetic:    r.Phonetic,
		AudioURL:    r.AudioURL,
		YouGlishURL: fmt.Sprintf("https://youglish.com/pronounce/%s/english", r.Word),
	}
	for _, mm := range r.Meanings {
		m.Meanings = append(m.Meanings, model.Meaning{
			PartOfSpeech: mm.PartOfSpeech,
			Definitions:  mm.Definitions,
		})
	}
	m.Examples = r.Examples
	for _, s := range r.Sources {
		m.Sources = append(m.Sources, model.Source{Platform: s.Platform, Model: s.Model})
	}
	return m
}
