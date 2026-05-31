-- 单词聚合表（去重主表，一个单词一条记录）
CREATE TABLE IF NOT EXISTS words (
    word TEXT PRIMARY KEY,
    phonetic TEXT,
    meanings TEXT,
    examples TEXT,
    sources TEXT,
    last_context TEXT,
    last_url TEXT,
    last_title TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    synced_at DATETIME
);

-- 查询历史表（记录每次查词的上下文）
CREATE TABLE IF NOT EXISTS word_lookups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    word TEXT NOT NULL,
    context TEXT,
    url TEXT,
    title TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_word_lookups_word ON word_lookups(word);
CREATE INDEX IF NOT EXISTS idx_word_lookups_created ON word_lookups(created_at);

-- 查词 API 缓存（按平台区分）
CREATE TABLE IF NOT EXISTS lookup_cache (
    word TEXT NOT NULL,
    source TEXT NOT NULL,
    model TEXT,
    result TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    PRIMARY KEY (word, source, model)
);

CREATE INDEX IF NOT EXISTS idx_lookup_cache_expires ON lookup_cache(expires_at);

-- LLM 单词卡缓存（按 prompt_version 隔离不同 prompt 的缓存）
CREATE TABLE IF NOT EXISTS word_analysis (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    word TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_version TEXT NOT NULL DEFAULT '',
    schema_version TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL,
    raw_response TEXT,
    tokens_used INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    UNIQUE(word, provider, model, prompt_version)
);

CREATE INDEX IF NOT EXISTS idx_word_analysis_word ON word_analysis(word);
