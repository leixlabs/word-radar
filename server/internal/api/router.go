package api

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// NewRouter 创建路由
func NewRouter(handler *Handler) *chi.Mux {
	r := chi.NewRouter()

	// 中间件
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second)) // LLM 调用可能需要更长时间

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// 根路径 — API 文档
	r.Get("/", handler.APIDocs)

	// 健康检查
	r.Get("/health", handler.HealthCheck)

	// API 路由
	r.Route("/api", func(r chi.Router) {
		r.Get("/lookup", handler.Lookup)
		r.Get("/wordcard", handler.WordCard)

		r.Get("/words", handler.ListWordRecords)
		r.Post("/words/sync", handler.SyncToObsidian)
		r.Get("/words/lookups", handler.WordLookupStats)
	})

	return r
}
