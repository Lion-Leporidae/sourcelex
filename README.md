# 🧠 RepoMind Go

> 代码知识库系统 - Go 语言实现

RepoMind 是一个智能代码分析和检索系统，提供代码语义搜索、函数调用链分析和 MCP 协议服务。可与 AI 助手（Cursor、Claude）无缝集成。

## ✨ 功能特性

- **🔍 语义搜索** - 基于向量嵌入的代码语义检索，用自然语言找代码
- **🕸️ 调用关系分析** - 函数调用链上下追溯，理解代码依赖
- **🌳 多语言 AST 解析** - 支持 Python、Go、Java、JavaScript/TypeScript
- **📡 MCP 协议** - 与 AI 助手（Cursor、Claude）无缝集成
- **⚡ 增量更新** - 基于文件哈希的智能增量索引

## 🛠️ 技术栈

| 模块 | 技术 |
|------|------|
| CLI | [Cobra](https://github.com/spf13/cobra) |
| 配置 | [Viper](https://github.com/spf13/viper) |
| 日志 | [Zap](https://github.com/uber-go/zap) |
| Git | [go-git](https://github.com/go-git/go-git) |
| AST | [go-tree-sitter](https://github.com/smacker/go-tree-sitter) |
| 向量存储 | [Qdrant](https://github.com/qdrant/go-client) |
| 嵌入 | HuggingFace Inference API |
| HTTP | [Gin](https://github.com/gin-gonic/gin) |

---

## 📦 安装

### 前置要求

- Go 1.21+
- GCC (用于 Tree-sitter CGO)
  - **Windows**: [MSYS2](https://www.msys2.org/) + MinGW-w64
  - **macOS**: Xcode Command Line Tools
  - **Linux**: `build-essential`

### 构建步骤

```bash
# 1. 克隆仓库
git clone https://github.com/your-username/repomind-go.git
cd repomind-go

# 2. 安装依赖
go mod tidy

# 3. 构建
# Windows (PowerShell)
$env:Path = "C:\msys64\ucrt64\bin;" + $env:Path
go build -o repomind.exe ./cmd/repomind

# Linux/macOS
go build -o repomind ./cmd/repomind

# 4. 验证安装
./repomind --help
```

---

## 🚀 快速开始

### 1. 配置 HuggingFace API

1. 访问 [huggingface.co/settings/tokens](https://huggingface.co/settings/tokens) 获取 API Token
2. 创建配置文件 `configs/config.yaml`:

```yaml
vector_store:
  huggingface:
    api_token: "hf_xxxxxxxxxx"  # 你的 Token
    model_id: "sentence-transformers/all-MiniLM-L6-v2"
    dimension: 384
```

### 2. 索引代码仓库

```bash
# 索引本地仓库
./repomind store --path /path/to/your/repo

# 索引远程仓库
./repomind store --repo https://github.com/user/repo --branch main
```

### 3. 启动 MCP 服务

```bash
./repomind serve --port 8000
```

### 4. 测试 API

```bash
# 健康检查
curl http://localhost:8000/health

# 语义搜索
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "计算两数之和", "top_k": 5}'
```

---

## 📁 项目结构

```
repomind-go/
├── cmd/repomind/          # 程序入口
├── internal/
│   ├── analyzer/          # 代码分析
│   │   ├── parser/        # Tree-sitter AST 解析
│   │   └── entity/        # 实体提取
│   ├── cmd/               # CLI 命令
│   ├── config/            # 配置管理
│   ├── git/               # Git 仓库管理
│   ├── logger/            # 日志系统
│   ├── mcp/               # MCP 服务
│   └── store/             # 存储层
│       ├── vector/        # 向量存储
│       └── graph/         # 图存储
├── configs/               # 配置文件
└── go.mod
```

---

## 🔌 API 参考

### 健康检查

```http
GET /health
```

响应:
```json
{"status": "ok", "service": "repomind-mcp"}
```

### 语义搜索

```http
POST /api/v1/search/semantic
Content-Type: application/json

{
  "query": "处理用户登录的函数",
  "top_k": 10
}
```

响应:
```json
{
  "success": true,
  "data": [
    {
      "entity_id": "auth.login",
      "name": "login",
      "type": "function",
      "file_path": "src/auth.py",
      "score": 0.92
    }
  ]
}
```

### 获取实体信息

```http
GET /api/v1/entity/{entity_id}
```

### 获取调用关系

```http
GET /api/v1/callmap/{entity_id}?depth=2
```

响应:
```json
{
  "success": true,
  "data": {
    "entity_id": "main.process",
    "callers": [...],
    "callees": [...]
  }
}
```

### SSE 连接

```http
GET /mcp/sse
```

---

## ⚙️ 配置说明

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `paths.data_dir` | 数据存储目录 | `./data` |
| `vector_store.type` | 向量数据库类型 | `qdrant` |
| `vector_store.huggingface.api_token` | HuggingFace API Token | 必填 |
| `vector_store.huggingface.model_id` | 嵌入模型 | `all-MiniLM-L6-v2` |
| `graph_store.type` | 图存储类型 | `memory` |
| `mcp.host` | 服务监听地址 | `0.0.0.0` |
| `mcp.port` | 服务监听端口 | `8000` |
| `logging.level` | 日志级别 | `info` |

---

## 📝 CLI 命令

```bash
# 查看帮助
./repomind --help

# 索引仓库
./repomind store --path .          # 本地仓库
./repomind store --repo <url>       # 远程仓库

# 启动服务
./repomind serve                   # 默认端口 8000
./repomind serve --port 9000       # 指定端口

# 查看版本
./repomind version
```

---

## 🔧 开发状态

- [x] Phase 1: 基础设施 (CLI, 配置, 日志)
- [x] Phase 2: 代码分析 (Git, AST, 实体提取)
- [x] Phase 3: 存储层 (向量存储, 图存储)
- [x] Phase 4: MCP 服务 (HTTP, SSE, API)

---

## 📄 License

MIT License
