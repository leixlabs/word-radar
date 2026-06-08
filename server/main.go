package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"word-radar/server/internal/api"
	"word-radar/server/internal/config"
	"word-radar/server/internal/dict"
	"word-radar/server/internal/logger"
	"word-radar/server/internal/obsidian"
	"word-radar/server/internal/storage"
	"word-radar/server/internal/wordcard"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to config file (default: config.yaml)")
	flag.Parse()

	log := logger.L()
	log.Info("Word Radar server starting")

	cfg := config.Load(configPath)
	log.Info("config loaded",
		slog.String("port", cfg.Server.Port),
		slog.String("dataDir", cfg.DataDir),
		slog.String("obsidianVault", cfg.Obsidian.VaultPath),
		slog.Bool("llmEnabled", cfg.LLM.Enabled),
		slog.String("llmProvider", cfg.LLM.Provider),
		slog.String("llmModel", cfg.LLM.Model),
	)

	// 初始化数据库
	db, err := storage.New(cfg.DataDir)
	if err != nil {
		log.Error("failed to init db", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()
	log.Info("database initialized")

	// 初始化模块
	aggregator := dict.NewAggregator(db, cfg.Dict.CacheTTL.AsDuration())
	obsGen := obsidian.NewGenerator(&cfg.Obsidian)
	wordcardSvc := wordcard.NewService(db, aggregator, cfg.LLM, cfg.WordCard)
	handler := api.NewHandler(cfg, db, aggregator, obsGen, wordcardSvc)
	router := api.NewRouter(handler)
	log.Info("modules initialized",
		slog.Bool("wordcardAvailable", wordcardSvc.IsAvailable()),
	)

	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	log.Info("server listening", slog.String("addr", addr))

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 7 * time.Minute, // LLM calls may take up to 6 min
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Error("server failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
