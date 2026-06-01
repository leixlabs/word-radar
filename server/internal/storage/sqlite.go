package storage

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"word-radar/server/internal/model"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB 包装 sql.DB
type DB struct {
	conn *sql.DB
}

// New 创建并初始化数据库
func New(dataDir string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s/word-radar.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", dataDir)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	db := &DB{conn: conn}
	if err := db.runMigrations(); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return db, nil
}

func (db *DB) runMigrations() error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("iofs source: %w", err)
	}

	driver, err := sqlite.WithInstance(db.conn, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("sqlite driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}

	// don't call m.Close() — that closes the underlying sql.DB
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Close 关闭连接
func (db *DB) Close() error {
	return db.conn.Close()
}

// UpsertWord 保存/更新单词聚合记录，同时记录查询历史
func (db *DB) UpsertWord(r *model.WordRecord) error {
	now := time.Now().UTC()

	// 1. 插入或更新单词聚合表（保留 synced_at 原值）
	_, err := db.conn.Exec(`
		INSERT INTO words (word, phonetic, meanings, examples, sources, last_context, last_url, last_title, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(word) DO UPDATE SET
			phonetic = excluded.phonetic,
			meanings = excluded.meanings,
			examples = excluded.examples,
			sources = excluded.sources,
			last_context = excluded.last_context,
			last_url = excluded.last_url,
			last_title = excluded.last_title
	`, r.Word, r.Phonetic, r.Meanings, r.Examples, r.Sources, r.Context, r.URL, r.Title, now)
	if err != nil {
		return fmt.Errorf("upsert word: %w", err)
	}

	// 2. 记录查询历史
	_, err = db.conn.Exec(`
		INSERT INTO word_lookups (word, context, url, title, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, r.Word, r.Context, r.URL, r.Title, now)
	if err != nil {
		return fmt.Errorf("insert lookup history: %w", err)
	}

	return nil
}

// ListWordRecords 查询所有单词聚合记录
func (db *DB) ListWordRecords(limit, offset int) ([]model.WordRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := db.conn.Query(`
		SELECT word, phonetic, meanings, examples, sources, last_context, last_url, last_title, created_at, synced_at
		FROM words ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []model.WordRecord
	for rows.Next() {
		var r model.WordRecord
		if err := rows.Scan(&r.Word, &r.Phonetic, &r.Meanings, &r.Examples, &r.Sources,
			&r.Context, &r.URL, &r.Title, &r.CreatedAt, &r.SyncedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// ListUnsyncedRecords 获取未同步到 Obsidian 的单词（首次出现）
func (db *DB) ListUnsyncedRecords() ([]model.WordRecord, error) {
	rows, err := db.conn.Query(`
		SELECT word, phonetic, meanings, examples, sources, last_context, last_url, last_title, created_at, synced_at
		FROM words WHERE synced_at IS NULL ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []model.WordRecord
	for rows.Next() {
		var r model.WordRecord
		if err := rows.Scan(&r.Word, &r.Phonetic, &r.Meanings, &r.Examples, &r.Sources,
			&r.Context, &r.URL, &r.Title, &r.CreatedAt, &r.SyncedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// MarkSynced 标记单词已同步
func (db *DB) MarkSynced(words []string) error {
	if len(words) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, w := range words {
		_, err := db.conn.Exec(`UPDATE words SET synced_at = ? WHERE word = ?`, now, w)
		if err != nil {
			return err
		}
	}
	return nil
}

// wordLookupTimeLayout matches SQLite datetime format from modernc.org/sqlite
const wordLookupTimeLayout = "2006-01-02 15:04:05.999999 -0700 MST"

// ListWordLookupStats 按时间范围统计单词查询次数（word_lookups 单表聚合）
// word 为空时返回所有单词的统计，否则只返回指定单词
// 单 SQL 查询，使用条件聚合获取：时间范围内次数、总次数、最早和最晚查询时间
func (db *DB) ListWordLookupStats(start, end time.Time, word string) ([]model.WordLookupStats, error) {
	var (
		rows *sql.Rows
		err  error
	)

	sqlQuery := `
		SELECT
			l.word,
			SUM(CASE WHEN l.created_at >= ? AND l.created_at <= ? THEN 1 ELSE 0 END),
			COUNT(*),
			MIN(l.created_at),
			MAX(CASE WHEN l.created_at >= ? AND l.created_at <= ? THEN l.created_at ELSE NULL END)
		FROM word_lookups l
		WHERE l.word IN (SELECT DISTINCT word FROM word_lookups WHERE created_at >= ? AND created_at <= ?)`

	if word != "" {
		sqlQuery += ` AND l.word = ?`
	}
	sqlQuery += ` GROUP BY l.word ORDER BY 2 DESC`

	if word != "" {
		rows, err = db.conn.Query(sqlQuery, start, end, start, end, start, end, word)
	} else {
		rows, err = db.conn.Query(sqlQuery, start, end, start, end, start, end)
	}
	if err != nil {
		return nil, fmt.Errorf("query word lookup stats: %w", err)
	}
	defer rows.Close()

	var stats []model.WordLookupStats
	for rows.Next() {
		var s model.WordLookupStats
		var firstLookupStr, lastLookupStr string
		if err := rows.Scan(&s.Word, &s.RangeCount, &s.TotalCount, &firstLookupStr, &lastLookupStr); err != nil {
			return nil, fmt.Errorf("scan word lookup stats: %w", err)
		}
		s.FirstLookupAt, _ = time.Parse(wordLookupTimeLayout, firstLookupStr)
		s.LastLookupAt, _ = time.Parse(wordLookupTimeLayout, lastLookupStr)
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// GetCache 获取查词缓存
func (db *DB) GetCache(word, source, modelName string) (string, bool, error) {
	var result string
	var expiresAt *time.Time
	err := db.conn.QueryRow(
		`SELECT result, expires_at FROM lookup_cache WHERE word = ? AND source = ? AND model IS ?`,
		word, source, modelName,
	).Scan(&result, &expiresAt)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return "", false, nil
	}
	return result, true, nil
}

// SetCache 设置查词缓存
func (db *DB) SetCache(word, source, modelName, result string, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl).UTC()
	_, err := db.conn.Exec(
		`INSERT INTO lookup_cache (word, source, model, result, expires_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(word, source, model) DO UPDATE SET
			 result = excluded.result,
			 created_at = excluded.created_at,
			 expires_at = excluded.expires_at`,
		word, source, modelName, result, expiresAt,
	)
	return err
}

// GetLLMAnalysis 获取 LLM 分析缓存，按 prompt_version 精确匹配
func (db *DB) GetLLMAnalysis(word, provider, modelName, promptVersion string) (*model.LLMAnalysis, bool, error) {
	var a model.LLMAnalysis
	var expiresAt *time.Time
	err := db.conn.QueryRow(
		`SELECT id, word, provider, model, prompt_version, schema_version, result, raw_response, tokens_used, created_at, expires_at
		 FROM word_analysis WHERE word = ? AND provider = ? AND model = ? AND prompt_version = ?`,
		word, provider, modelName, promptVersion,
	).Scan(&a.ID, &a.Word, &a.Provider, &a.Model, &a.PromptVersion, &a.SchemaVersion, &a.Result,
		&a.RawResponse, &a.TokensUsed, &a.CreatedAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return nil, false, nil
	}
	return &a, true, nil
}

// SaveLLMAnalysis 保存 LLM 分析
func (db *DB) SaveLLMAnalysis(a *model.LLMAnalysis) error {
	_, err := db.conn.Exec(
		`INSERT INTO word_analysis (word, provider, model, prompt_version, schema_version, result, raw_response, tokens_used, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(word, provider, model, prompt_version) DO UPDATE SET
			 prompt_version = excluded.prompt_version,
			 schema_version = excluded.schema_version,
			 result = excluded.result,
			 raw_response = excluded.raw_response,
			 tokens_used = excluded.tokens_used,
			 created_at = excluded.created_at,
			 expires_at = excluded.expires_at`,
		a.Word, a.Provider, a.Model, a.PromptVersion, a.SchemaVersion, a.Result, a.RawResponse, a.TokensUsed,
		a.ExpiresAt,
	)
	return err
}
