# Word Radar

点击单词查词、发音，收集生词同步到 Obsidian 的 Chrome Extension + Go 后端。

## 快速开始

### 1. 启动后端

```bash
cd server
go mod tidy
go build -o word-radar-server main.go
./word-radar-server
```

或 Docker：
```bash
docker-compose up -d
```

后端默认监听 `http://localhost:8787`。

### 配置

Docker 部署使用根目录的 [`confg.yaml`](confg.yaml) 作为配置源，会自动挂载到容器内。
修改 `confg.yaml` 后重启容器即可生效：

```bash
docker-compose restart
```

如需通过环境变量临时覆盖（如传入 API Key），取消注释 `docker-compose.yml` 中的 `environment` 块即可。

### 2. 加载 Extension

1. 打开 Chrome，进入 `chrome://extensions/`
2. 开启「开发者模式」
3. 点击「加载已解压的扩展程序」，选择 `extension/` 目录

### 3. 配置

点击扩展图标，设置：
- 触发方式：单击 / 双击
- 后端地址（默认 `http://localhost:8787`）
- TTS 语音偏好

### 4. 使用

在任意网页点击单词即可查词、发音。单词自动保存到 Obsidian。

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SERVER_PORT` | 后端端口 | `8787` |
| `OBSIDIAN_VAULT_PATH` | Obsidian Vault 路径 | `/Users/lei/dev/ob` |
| `OBSIDIAN_WORDS_DIR` | 单词文件存放目录 | `英语学习/words` |
| `DATA_DIR` | SQLite 数据目录 | `./data` |
| `DICT_CACHE_TTL` | 查词缓存 TTL | `168h` |

## 技术栈

- **Extension**: Chrome Extension Manifest V3 + Web Speech API
- **Backend**: Go + Chi Router + SQLite (modernc.org/sqlite)
- **Dict APIs**: dictionaryapi.dev + 有道 suggest
- **Obsidian**: Markdown Daily Note 格式

## 项目结构

```
word-radar/
├── extension/          # Chrome Extension
├── server/             # Go Backend
├── docker-compose.yml
└── spec.md             # 设计文档
```
