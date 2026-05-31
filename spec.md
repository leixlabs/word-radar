# Word Radar — Chrome Extension Spec

> 在英语阅读时收集不认识的单词及上下文，同步到 Obsidian 进行后续学习与练习。

---

## 1. 项目概述

### 1.1 目标
开发一个 Chrome Extension + 本地 Go Server，实现：
- 网页阅读时**点击/选词**即查词、发音
- 收集单词及其**所在句子上下文**
- 后端代理免费查词 API，缓存结果
- 自动生成 Obsidian 卡片，同步到本地 vault
- 后续扩展：接入 LLM 拆解单词词根词缀，结果缓存到 SQLite

### 1.2 核心用户流程
```
用户阅读网页 → 点击生词 → 弹窗展示音标/释义/例句/TTS发音
                           → 自动收集「单词+上下文+URL+时间」
                           → 后端聚合数据，生成 Obsidian Markdown 文件
                           → 用户在 Obsidian 中复习、练习
```

### 1.3 技术栈
| 组件 | 技术 |
|------|------|
| Extension | Chrome Extension Manifest V3 (content script + background service worker + popup) |
| UI | 原生 DOM + 自定义弹窗（无框架，轻量） |
| TTS | Web Speech API (`speechSynthesis`) |
| 后端 | Go 1.22+ (标准库 + chi router) |
| 缓存/存储 | SQLite3 (`mattn/go-sqlite3` 或 `modernc.org/sqlite`) |
| 文件写入 | Go 标准库，直接写入 Obsidian vault 目录 |
| 部署 | Docker / 本地二进制 |

---

## 2. Chrome Extension

### 2.1 功能模块

#### A. 页面划词/点击触发 (Content Script)
- **触发方式**（可配置，默认「单击」）：
  - `单击` — 点击任意单词，自动取词
  - `双击` — 双击取词
  - `选词+快捷键` — 鼠标划选后按 `Shift` 或自定义键
- **取词逻辑**：
  - 通过 `window.getSelection()` 或 `document.caretPositionFromPoint` 获取点击位置的文本
  - 提取整词（正则：`[a-zA-Z'-]+`）
  - 获取单词所在句子上下文（通过 DOM 遍历或 `sentence-extractor` 逻辑）

#### B. 查词弹窗 (Content Script)
- **弹窗内容**：
  - 单词本身
  - 音标 (IPA)
  - 释义列表（词性 + 中文/英文释义）
  - 例句（优先 API 返回，否则展示页面上下文句子）
  - 🔊 发音按钮（调用 Web Speech API，英音优先）
  - 📌「加入生词本」按钮
- **交互行为**：
  - 鼠标**悬浮在弹窗上时不会自动关闭**
  - 点击弹窗外区域 / 按 ESC 关闭
  - 弹窗可拖拽移动
  - 弹窗位置智能避让（不超出视口边缘）

#### C. 数据收集 (Background Service Worker)
- 每次查词自动附带上下文参数到 `/api/lookup`
- 后端检测到 context 参数后自动保存记录并同步到 Obsidian

#### D. 配置面板 (Popup)
- 触发方式选择（单击/双击/选词+快捷键）
- 后端 Server 地址（默认 `http://localhost:8787`）
- TTS 设置：语速、音调、英音/美音
- 是否自动加入生词本（默认开启）
- Obsidian Vault 路径设置（用于后端生成文件）

### 2.2 扩展结构
```
extension/
├── manifest.json          # MV3
├── content.js             # 页面注入、取词、弹窗渲染
├── background.js          # Service worker: API 通信、数据收集
├── popup.html / popup.js / popup.css
├── icons/
└── _locales/
```

### 2.3 权限需求
```json
"permissions": [
  "activeTab",
  "storage",
  "scripting"
],
"host_permissions": [
  "http://localhost:8787/*",
  "<all_urls>"
]
```

---

## 3. Go 后端 Server

### 3.1 架构设计
```
server/
├── main.go                # 入口，HTTP server 启动
├── cmd/
│   └── server/
├── internal/
│   ├── api/               # HTTP handlers (chi)
│   ├── dict/              # 查词代理模块
│   ├── llm/               # LLM 拆解（v2 扩展）
│   ├── model/             # 数据模型
│   ├── obsidian/          # Markdown 文件生成
│   ├── storage/           # SQLite 存储层
│   └── config/            # 配置管理
├── migrations/            # SQLite schema migrations
├── Dockerfile
└── go.mod
```

### 3.2 核心模块

#### A. 查词代理 (`internal/dict/`)
聚合多个免费 API，返回统一格式：

```go
// 聚合后的查词结果
type WordResult struct {
    Word        string       `json:"word"`
    Phonetic    string       `json:"phonetic"`      // IPA 音标
    Meanings    []Meaning    `json:"meanings"`      // 释义列表
    Examples    []string     `json:"examples"`      // 例句
    AudioURL    string       `json:"audio_url"`     // 发音音频链接（dictionaryapi 提供）
    Sources     []Source     `json:"sources"`       // 本次聚合使用了哪些平台
}

type Source struct {
    Platform string `json:"platform"`  // dictionaryapi / youdao
    Model    string `json:"model"`     // API 平台无模型时为 ""
}

// LLM 单词拆解（独立数据结构）
type LLMAnalysis struct {
    Word         string   `json:"word"`
    Provider     string   `json:"provider"`      // openai / anthropic / ollama
    Model        string   `json:"model"`         // gpt-4o-mini / claude-3.5-sonnet
    Etymology    string   `json:"etymology"`     // 词源
    Root         string   `json:"root"`          // 词根
    Prefix       string   `json:"prefix"`        // 前缀
    Suffix       string   `json:"suffix"`        // 后缀
    Cognates     []string `json:"cognates"`      // 同根词
    MemoryAid    string   `json:"memory_aid"`    // 记忆法
    Nuances      string   `json:"nuances"`       // 用法辨析
}
```

**API 策略**：
1. 同时并发请求：
   - `https://api.dictionaryapi.dev/api/v2/entries/en/{word}`
   - `https://dict.youdao.com/suggest?num=1&doctype=json&q={word}`
2. 先返回成功者优先，但尽量合并两者结果
3. 结果缓存到 SQLite（TTL 7 天）

#### B. 查词记录存储 (`internal/storage/`)
SQLite schema：
```sql
CREATE TABLE words (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    word TEXT NOT NULL,
    context TEXT,
    url TEXT,
    title TEXT,
    phonetic TEXT,
    meanings TEXT,        -- JSON（聚合后的释义）
    examples TEXT,        -- JSON
    sources TEXT,         -- JSON: [{"platform":"dictionaryapi"},{"platform":"youdao"}]
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    synced_at DATETIME,   -- 同步到 Obsidian 的时间
    UNIQUE(word, url, created_at)
);

-- 查词 API 缓存（按平台区分）
CREATE TABLE lookup_cache (
    word TEXT NOT NULL,
    source TEXT NOT NULL,   -- 平台: dictionaryapi / youdao / 后续扩展
    model TEXT,             -- API 无模型时 NULL
    result TEXT NOT NULL,   -- JSON: 原始 API 响应
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    PRIMARY KEY (word, source, model)
);

-- LLM 单词拆解（独立表，与查词结果解耦）
CREATE TABLE word_analysis (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    word TEXT NOT NULL,
    provider TEXT NOT NULL,       -- openai / anthropic / ollama / etc.
    model TEXT NOT NULL,          -- gpt-4o-mini / claude-3.5-sonnet / llama3.1
    prompt_version TEXT,          -- 提示词版本，便于追踪迭代
    result TEXT NOT NULL,         -- JSON: LLM 返回的拆解结果
    raw_response TEXT,            -- 原始 LLM 响应（调试用）
    tokens_used INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    UNIQUE(word, provider, model)
);
```

#### C. Obsidian 卡片生成 (`internal/obsidian/`)
- 监听 `/api/words/sync` 或定时任务
- 按日期生成/追加 Markdown 文件：
  - 路径：`{vault}/英语学习/words/Word Radar - {YYYY-MM-DD}.md`
- 文件格式（Daily Note 风格）：

```markdown
---
date: 2026-05-31
total_words: 3
source: Word Radar
---

# Word Radar - 2026-05-31

## unrivaled
- **音标**: /ʌnˈraɪvəld/
- **释义**: (adj.) 无与伦比的，无敌的
- **上下文**: The company has an unrivaled reputation in the industry.
- **来源**: [Article Title](https://example.com/article)
- **时间**: 10:30

---

## produced
- **音标**: /prəˈduːst/
- **释义**: (v.) 生产，制造；产生
- **上下文**: The factory produced over 10,000 units last month.
- **来源**: [Another Article](https://example.com/another)
- **时间**: 11:15

---

<!-- LLM 拆解 v2 扩展 -->
## produced - LLM 拆解
**词根**: pro- (向前) + duct (引导)  
...（后续接入 LLM）
```

#### D. LLM 单词拆解（v2 扩展，预留）
- 当查词时，异步请求 LLM API（OpenAI/Claude/本地 Ollama）
- 提示词模板：拆解词根词缀、联想记忆、同根词
- 结果独立存入 `word_analysis` 表，与 `lookup_cache` 完全解耦
- 支持同一单词多个 LLM 的对比（不同 provider/model）
- 生成 Obsidian 卡片时可选并入

### 3.3 API 设计

| Method | Path | 描述 |
|--------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/api/lookup?q={word}&context={ctx}` | 查词并返回 WordResult；提供 context 时自动保存记录 |
| GET | `/api/words` | 查词历史列表（分页） |
| POST | `/api/words/sync` | 手动触发同步到 Obsidian |
| GET | `/api/words/stats` | 统计信息（今日单词数等） |

### 3.4 配置
环境变量或 config.yaml：
```yaml
server:
  port: 8787

obsidian:
  vault_path: "/Users/lei/dev/ob"
  words_dir: "英语学习/words"
  daily_note_format: "Word Radar - {date}.md"

dict:
  cache_ttl: "168h"  # 7天

llm:  # v2 扩展
  enabled: false
  provider: "openai"  # openai / claude / ollama
  api_key: ""
  model: "gpt-4o-mini"
```

---

## 4. 数据流

### 4.1 查词流程
```
┌─────────────┐     点击单词      ┌──────────────────┐
│   网页页面   │ ───────────────→ │  Content Script  │
└─────────────┘                  └──────────────────┘
                                         │
                                         ▼
                              ┌──────────────────┐
                              │  提取单词+上下文  │
                              └──────────────────┘
                                         │
                    ┌────────────────────┼────────────────────┐
                    ▼                    ▼                    ▼
           ┌─────────────┐      ┌─────────────┐      ┌─────────────┐
           │  调用 TTS    │      │ 展示弹窗 UI  │      │ POST /api/  │
           │ speechSynthe-│      │ (音标/释义)  │      │ words       │
           │ sis          │      └─────────────┘      └──────┬──────┘
           └─────────────┘                                   │
                                                              ▼
                                                   ┌──────────────────┐
                                                   │   Go Backend     │
                                                   └──────────────────┘
                                                            │
                    ┌───────────────────────────────────────┼───────────┐
                    ▼                                       ▼           ▼
           ┌─────────────┐                         ┌─────────────┐ ┌──────────┐
           │  SQLite 存储 │                         │ 查词 API 代理 │ │ Obsidian │
           │  words 表   │                         │ (缓存优先)   │ │ 文件生成  │
           └─────────────┘                         └─────────────┘ └──────────┘
```

### 4.2 同步到 Obsidian 流程
```
方式一：实时追加（默认）
  - 每次 POST /api/words 后，后端立即追加到当日 Markdown 文件

方式二：手动同步
  - 用户点击 Extension popup 中的「Sync to Obsidian」
  - 或调用 POST /api/words/sync
  - 后端批量生成/更新文件
```

---

## 5. 界面设计

### 5.1 查词弹窗（Content Script 内嵌）
```
┌────────────────────────────────────────┐
│  unparalleled     [🔊]  [📌]  [✕]     │
│  /ʌnˈpærəleld/                         │
├────────────────────────────────────────┤
│  adj. 无比的，无双的                    │
│  v.  使...无与伦比                    │
├────────────────────────────────────────┤
│  📖 例句                               │
│  "The beauty of the sunset was         │
│   unparalleled."                       │
├────────────────────────────────────────┤
│  📄 上下文                             │
│  "The view from the mountain offered    │
│   an unparalleled panorama."           │
│  —— From: Example Article              │
└────────────────────────────────────────┘
```

### 5.2 Popup 配置面板
```
┌─────────────────────────────┐
│      ⚙️ Word Radar          │
├─────────────────────────────┤
│ 触发方式: [单击 ▼]          │
│ 后端地址: http://localhost:8787
│ TTS: [英音 ●] [美音 ○]      │
│ 自动保存: [✓]               │
│                             │
│ [🔄 同步到 Obsidian]        │
│                             │
│ 今日已收集: 12 词           │
└─────────────────────────────┘
```

---

## 6. 开发阶段

### Phase 1: MVP（核心闭环）
- [ ] Chrome Extension：取词、弹窗、TTS
- [ ] Go Server：查词代理（dictionaryapi + youdao）、REST API
- [ ] SQLite 存储查词记录
- [ ] 生成 Obsidian Daily Note 格式 Markdown

### Phase 2: 体验优化
- [ ] 弹窗位置智能、悬浮不关闭、拖拽
- [ ] 配置面板（popup）
- [ ] 查词缓存机制
- [ ] 批量同步/手动同步

### Phase 3: LLM 扩展
- [ ] 接入 LLM API 拆解单词
- [ ] SQLite 缓存 LLM 结果
- [ ] Obsidian 卡片中加入 LLM 分析区块
- [ ] Anki/Quiz 模式（可选）

---

## 7. 关键决策记录 (ADR)

### ADR-1: 后端写本地文件而非 Extension 直接写
- **决策**: Go backend 负责写入 Obsidian vault
- **理由**: Extension 写本地文件需要 `chrome.fileSystem` API（已废弃/限制多），且需要用户交互选择目录。后端跑在本地/Docker，直接文件写入更可靠，也便于后续定时任务、批量处理。

### ADR-2: 每日一个 Markdown 文件
- **决策**: 按 `Word Radar - YYYY-MM-DD.md` 聚合，而非每词一个文件
- **理由**: 减少文件数量，便于浏览当日学习内容；与 Obsidian Daily Note 工作流一致。

### ADR-3: 并发请求多个免费 API
- **决策**: 同时请求 dictionaryapi.dev 和 youdao，合并结果
- **理由**: dictionaryapi 返回详细释义和音标但无中文；youdao 有中文释义和基本音标。合并两者互补。

### ADR-4: 使用 modernc.org/sqlite（纯 Go）
- **决策**: 优先使用 `modernc.org/sqlite` 而非 CGO 的 mattn/go-sqlite3
- **理由**: 纯 Go 实现，交叉编译简单，Docker 镜像更小，无需 CGO 环境。

---

## 8. 目录结构（完整项目）

```
word-radar/
├── spec.md                    # 本文件
├── README.md
├── docker-compose.yml
│
├── extension/                 # Chrome Extension
│   ├── manifest.json
│   ├── content.js
│   ├── content.css
│   ├── background.js
│   ├── popup.html
│   ├── popup.js
│   ├── popup.css
│   ├── icons/
│   │   ├── icon16.png
│   │   ├── icon48.png
│   │   └── icon128.png
│   └── _locales/
│       └── en/
│           └── messages.json
│
└── server/                    # Go Backend
    ├── Dockerfile
    ├── go.mod
    ├── go.sum
    ├── main.go
    ├── config.yaml
    ├── internal/
    │   ├── api/
    │   │   ├── handler.go
    │   │   └── router.go
    │   ├── dict/
    │   │   ├── dictionaryapi.go
    │   │   ├── youdao.go
    │   │   ├── aggregator.go
    │   │   └── cache.go
    │   ├── model/
    │   │   └── word.go
    │   ├── storage/
    │   │   ├── sqlite.go
    │   │   └── word_repo.go
    │   ├── obsidian/
    │   │   └── generator.go
    │   ├── llm/
    │   │   └── analyzer.go        # v2
    │   └── config/
    │       └── config.go
    └── migrations/
        └── 001_init.sql
```

---

## 9. 风险与应对

| 风险 | 应对 |
|------|------|
| 免费 API 限流/不可用 | 本地 SQLite 缓存 + 降级提示 |
| dictionaryapi.dev 对中文支持差 | youdao API 补充中文释义 |
| 弹窗与某些网站 CSS 冲突 | Shadow DOM 隔离样式 |
| Extension 与 CSP 严格网站冲突 | 通过 backend 代理所有请求 |
| Go 后端文件权限问题 | Docker 运行，挂载 volume，文档说明权限配置 |

---

## 10. 后续扩展（Backlog）

- [ ] 单词熟练度评级（生疏/认识/掌握）
- [ ] 根据艾宾浩斯曲线生成复习提醒
- [ ] 导出 Anki 卡片
- [ ] 单词本 Web UI（独立页面）
- [ ] 支持 PDF 阅读器取词
- [ ] 多语言支持（日语、法语等）
