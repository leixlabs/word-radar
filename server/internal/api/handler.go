package api

import (
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"word-radar/server/internal/config"
	"word-radar/server/internal/dict"
	"word-radar/server/internal/logger"
	"word-radar/server/internal/model"
	"word-radar/server/internal/obsidian"
	"word-radar/server/internal/storage"
	"word-radar/server/internal/wordcard"
)

//go:embed templates/*
var templateFS embed.FS

var apiDocsTmpl = template.Must(template.ParseFS(templateFS, "templates/api_docs.html"))

// Handler API 处理器
type Handler struct {
	cfg        *config.Config
	db         *storage.DB
	aggregator *dict.Aggregator
	obsidian   *obsidian.Generator
	wordcard   *wordcard.Service
	syncMu     sync.Mutex
	log        *slog.Logger
}

// NewHandler 创建处理器
func NewHandler(cfg *config.Config, db *storage.DB, aggregator *dict.Aggregator, obsGen *obsidian.Generator, wc *wordcard.Service) *Handler {
	return &Handler{
		cfg:        cfg,
		db:         db,
		aggregator: aggregator,
		obsidian:   obsGen,
		wordcard:   wc,
		log:        logger.L(),
	}
}

// APIDocs returns an HTML API documentation page at the root path.
func (h *Handler) APIDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := struct {
		Port   string
		Limit  string
		Offset string
	}{
		Port:   h.cfg.Server.Port,
		Limit:  "20",
		Offset: "0",
	}

	if err := apiDocsTmpl.Execute(w, data); err != nil {
		h.log.Error("failed to render api docs template", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// HealthCheck 健康检查
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LookupResponse 查词响应
type LookupResponse struct {
	Word        string          `json:"word"`
	Phonetic    string          `json:"phonetic"`
	Meanings    []model.Meaning `json:"meanings"`
	Examples    []string        `json:"examples"`
	AudioURL    string          `json:"audio_url"`
	YouGlishURL string          `json:"youglish_url"`
	Sources     []model.Source  `json:"sources"`
}

// Lookup 查词接口
// 只返回字典查询结果（音标、释义、例句），快速响应
// 如果提供 context 参数，则同时保存查词记录
func (h *Handler) Lookup(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	word := r.URL.Query().Get("q")
	if word == "" {
		h.log.Warn("lookup missing q")
		respondError(w, http.StatusBadRequest, "missing q parameter")
		return
	}

	result, err := h.aggregator.Lookup(word)
	if err != nil {
		h.log.Warn("lookup failed", slog.String("word", word), slog.String("error", err.Error()))
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// 如果有上下文，自动保存记录（只在 lookup 时保存，避免重复）
	contextStr := r.URL.Query().Get("context")
	if contextStr != "" {
		h.saveRecord(result, contextStr)
	}

	elapsed := time.Since(start)
	h.log.Info("lookup success",
		slog.String("word", word),
		slog.String("clientIP", r.RemoteAddr),
		slog.Duration("elapsed", elapsed),
	)

	respondJSON(w, http.StatusOK, LookupResponse{
		Word:        result.Word,
		Phonetic:    result.Phonetic,
		Meanings:    result.Meanings,
		Examples:    result.Examples,
		AudioURL:    result.AudioURL,
		YouGlishURL: result.YouGlishURL,
		Sources:     result.Sources,
	})
}

// WordCard 单词卡生成接口
// 聚合 dict API（基础数据）+ LLM（记忆增强数据），返回完整 3 层单词卡。
// 优先读 LLM 缓存，TTL 30 天。
func (h *Handler) WordCard(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	word := r.URL.Query().Get("q")
	if word == "" {
		h.log.Warn("wordcard missing q")
		respondError(w, http.StatusBadRequest, "missing q parameter")
		return
	}

	if h.wordcard == nil || !h.wordcard.IsAvailable() {
		// 降级：用纯词典数据返回基础卡片
		h.log.Warn("wordcard llm unavailable, returning dict-only card", slog.String("word", word))
		dictResult, err := h.aggregator.Lookup(word)
		if err != nil {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		card := h.wordcard.BuildFallbackCard(dictResult)
		card.Warning = "AI 增强服务未配置（或 API Key 缺失），仅显示词典释义。"
		respondJSON(w, http.StatusOK, card)
		return
	}

	card, err := h.wordcard.Generate(word)
	if err != nil {
		h.log.Error("wordcard generation failed", slog.String("word", word), slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	elapsed := time.Since(start)
	h.log.Info("wordcard success",
		slog.String("word", word),
		slog.String("clientIP", r.RemoteAddr),
		slog.Duration("elapsed", elapsed),
	)

	respondJSON(w, http.StatusOK, card)
}

// ListWordRecords 查词历史列表
func (h *Handler) ListWordRecords(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	records, err := h.db.ListWordRecords(limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, records)
}

// SyncToObsidian 手动触发同步到 Obsidian
func (h *Handler) SyncToObsidian(w http.ResponseWriter, r *http.Request) {
	path, err := h.syncToObsidian()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{
		"path":    path,
		"message": "synced to obsidian",
	})
}

func (h *Handler) syncToObsidian() (string, error) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()

	records, err := h.db.ListUnsyncedRecords()
	if err != nil {
		return "", err
	}

	if len(records) == 0 {
		return "", nil
	}

	h.log.Info("syncing to obsidian", slog.Int("count", len(records)))

	path, err := h.obsidian.GenerateDailyNote(records)
	if err != nil {
		return "", err
	}

	var words []string
	for _, r := range records {
		words = append(words, r.Word)
	}
	if err := h.db.MarkSynced(words); err != nil {
		return path, err
	}

	h.log.Info("synced to obsidian", slog.String("path", path), slog.Int("words", len(words)))
	return path, nil
}

// WordLookupStats 按时间范围统计单词查询次数
// GET /api/words/lookups?start=2026-01-01T00:00:00Z&end=2026-06-01T23:59:59Z&word=example
// word 参数可选，不传则返回时间范围内所有单词的查询统计
func (h *Handler) WordLookupStats(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	if startStr == "" || endStr == "" {
		respondError(w, http.StatusBadRequest, "missing start or end parameter")
		return
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid start format, use RFC3339 (e.g. 2026-01-01T00:00:00Z)")
		return
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid end format, use RFC3339 (e.g. 2026-06-01T23:59:59Z)")
		return
	}

	word := r.URL.Query().Get("word")

	stats, err := h.db.ListWordLookupStats(start, end, word)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, stats)
}

// ====== 辅助方法 ======

func (h *Handler) saveRecord(result *model.WordResult, context string) {
	meaningsJSON, _ := json.Marshal(result.Meanings)
	examplesJSON, _ := json.Marshal(result.Examples)
	sourcesJSON, _ := json.Marshal(result.Sources)

	record := &model.WordRecord{
		Word:      result.Word,
		Context:   context,
		Phonetic:  result.Phonetic,
		Meanings:  string(meaningsJSON),
		Examples:  string(examplesJSON),
		Sources:   string(sourcesJSON),
		CreatedAt: time.Now().UTC(),
	}

	if err := h.db.UpsertWord(record); err != nil {
		h.log.Error("save record failed", slog.String("word", result.Word), slog.String("error", err.Error()))
		return
	}
	h.log.Debug("record saved", slog.String("word", result.Word))
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
