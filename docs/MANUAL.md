# Sourcelex Go 使用文档

## 目录

- [1. 项目简介](#1-项目简介)
- [2. 环境准备与安装](#2-环境准备与安装)
- [3. 配置说明](#3-配置说明)
- [4. 快速开始](#4-快速开始)
- [5. CLI 命令详解](#5-cli-命令详解)
- [6. API 接口参考](#6-api-接口参考)
  - [6.1 健康检查与工作空间](#61-健康检查与工作空间)
  - [6.2 语义搜索](#62-语义搜索)
  - [6.3 混合搜索](#63-混合搜索)
  - [6.4 上下文感知搜索](#64-上下文感知搜索)
  - [6.5 实体查询](#65-实体查询)
  - [6.6 调用图（JSON 格式）](#66-调用图json-格式)
  - [6.7 紧凑调用链（AI 优化格式）](#67-紧凑调用链ai-优化格式)
  - [6.8 图分析](#68-图分析)
  - [6.9 RAG 上下文组装](#69-rag-上下文组装)
  - [6.10 Git 历史分析](#610-git-历史分析)
- [7. MCP 协议集成（AI 助手对接）](#7-mcp-协议集成ai-助手对接)
- [8. AI Agent 对话功能](#8-ai-agent-对话功能)
- [9. 高级特性](#9-高级特性)
- [10. 故障排查](#10-故障排查)

---

## 1. 项目简介

Sourcelex 是一个代码知识库系统，它能将任意代码仓库转化为可搜索、可分析的知识库，并通过 MCP 协议与 AI 助手无缝对接。

**核心能力：**

| 能力 | 说明 |
|------|------|
| 语义搜索 | 用自然语言查找代码，基于 HuggingFace 向量嵌入 |
| 调用图分析 | 追踪函数之间的调用/被调用关系，支持多级深度遍历 |
| 多语言支持 | Python、Go、Java、JavaScript、TypeScript、C、C++ |
| MCP 协议 | 原生对接 Cursor、Claude Desktop 等 AI 助手 |
| AI Agent | 内置 LLM 对话，支持 OpenAI / Anthropic / DeepSeek |
| RAG 管线 | 多源上下文组装，为 LLM 提供精准代码上下文 |
| 增量索引 | 基于文件哈希+修改时间，只处理变更文件 |
| Git 历史 | 提交历史、文件 Blame、实体变更追踪 |

**数据流水线：**

```
Git 仓库 → 文件扫描(增量检测) → Tree-sitter AST 解析 → 实体提取(函数/类/方法)
         → 调用关系提取(符号表) → 代码分块 → HuggingFace 向量嵌入
         → chromem-go 向量库 + SQLite 图数据库
```

---

## 2. 环境准备与安装

### 2.1 系统要求

| 项目 | 要求 |
|------|------|
| 操作系统 | Windows 10+、macOS 10.15+、Linux |
| Go 版本 | 1.21 或更高 |
| GCC | Tree-sitter CGO 编译需要 |
| 内存 | 建议 4GB+ |
| HuggingFace Token | 生成代码向量嵌入（必需） |

### 2.2 安装 Go

```bash
# macOS
brew install go

# Linux (Debian/Ubuntu)
sudo apt install golang-go

# Windows
# 从 https://go.dev/dl/ 下载安装包

# 验证
go version
```

### 2.3 安装 GCC

**macOS：**
```bash
xcode-select --install
```

**Linux：**
```bash
sudo apt install build-essential    # Debian/Ubuntu
sudo yum install gcc                # CentOS/RHEL
```

**Windows：**
```powershell
# 1. 下载并安装 MSYS2: https://www.msys2.org/
# 2. 在 MSYS2 终端中安装 MinGW-w64
pacman -S mingw-w64-ucrt-x86_64-gcc

# 3. 添加到系统 PATH
# 将 C:\msys64\ucrt64\bin 添加到系统环境变量

# 4. 验证
gcc --version
```

### 2.4 获取 HuggingFace API Token

1. 访问 [huggingface.co/settings/tokens](https://huggingface.co/settings/tokens)
2. 创建一个 Access Token（Read 权限即可）
3. 记录 Token，后续配置使用

### 2.5 构建

```bash
git clone https://github.com/Lion-Leporidae/sourcelex.git
cd sourcelex
go mod tidy

# Linux / macOS
CGO_ENABLED=1 go build -o sourcelex ./cmd/sourcelex

# Windows PowerShell
$env:Path = "C:\msys64\ucrt64\bin;" + $env:Path
$env:CGO_ENABLED = "1"
go build -o sourcelex.exe ./cmd/sourcelex

# 验证
./sourcelex --help
```

---

## 3. 配置说明

### 3.1 配置文件位置

按优先级从高到低加载：

1. `--config` 命令行参数指定的路径
2. `./configs/config.yaml`
3. `./config.yaml`
4. `~/.sourcelex/config.yaml`

### 3.2 最小配置

只需一个 HuggingFace Token 即可运行：

```yaml
vector_store:
  huggingface:
    api_token: "hf_你的token"
```

### 3.3 完整配置

```yaml
# 路径配置
paths:
  data_dir: "./data"       # 数据存储目录（向量库、图数据库）
  temp_dir: "./temp"       # 临时文件目录（克隆的仓库）

# Git 配置
git:
  clone_depth: 1           # 克隆深度（1 = 浅克隆，节省空间）
  credentials:             # 私有仓库凭证（可选）
    # github.com: "ghp_xxxx"

# 向量存储配置
vector_store:
  type: "chromem"          # chromem（本地）或 qdrant（远程）
  embedder_type: "huggingface"
  chunk_size: 512          # 代码分块大小
  chunk_overlap: 50        # 分块重叠字符数

  # HuggingFace 嵌入模型配置（必需）
  huggingface:
    api_token: "hf_xxxxx"
    model_id: "sentence-transformers/all-MiniLM-L6-v2"
    dimension: 384         # 向量维度，需与模型匹配

  # Qdrant 配置（如使用远程向量库）
  qdrant:
    host: "localhost"
    port: 6334
    collection_name: "code_vectors"

# 图存储配置
graph_store:
  type: "sqlite"           # sqlite（持久化）或 memory（内存）

# MCP 服务器配置
mcp:
  host: "0.0.0.0"          # 监听地址（0.0.0.0 = 所有接口）
  port: 8000               # 监听端口

# AI Agent 配置（可选，启用对话问答功能）
agent:
  provider: ""             # openai / anthropic / 留空禁用

  # OpenAI / 兼容 API 配置
  openai:
    api_key: ""
    model: "gpt-4o"
    base_url: ""           # 自定义端点，留空默认 OpenAI
                           # DeepSeek: https://api.deepseek.com/v1
                           # Ollama: http://localhost:11434/v1

  # Anthropic Claude 配置
  anthropic:
    api_key: ""
    model: "claude-sonnet-4-20250514"

# 日志配置
logging:
  level: "info"            # debug / info / warn / error
  format: "text"           # json / text
```

### 3.4 环境变量覆盖

所有配置项都可以用 `SOURCELEX_` 前缀的环境变量覆盖，将 `.` 替换为 `_` 并大写：

```bash
export SOURCELEX_VECTOR_STORE_HUGGINGFACE_API_TOKEN=hf_xxxxx
export SOURCELEX_AGENT_OPENAI_API_KEY=sk-xxxxx
export SOURCELEX_MCP_PORT=9000
export SOURCELEX_LOGGING_LEVEL=debug
```

---

## 4. 快速开始

### 三步上手

```bash
# 第一步：索引仓库
./sourcelex store --repo https://github.com/gin-gonic/gin.git --branch master

# 第二步：启动服务
./sourcelex serve --port 8000

# 第三步：查询代码
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "HTTP路由处理", "top_k": 5}'
```

### 索引本地项目

```bash
./sourcelex store --path /path/to/your/project
./sourcelex store --path .   # 索引当前目录
```

### 完整使用流程

```bash
# 1. 创建配置文件
cat > config.yaml << 'EOF'
vector_store:
  huggingface:
    api_token: "hf_你的token"
EOF

# 2. 索引仓库
./sourcelex store --repo https://github.com/Lion-Leporidae/sourcelex.git --branch main

# 3. 启动服务
./sourcelex serve --port 8000 &

# 4. 验证服务
curl http://localhost:8000/health
# → {"status":"ok","service":"sourcelex-mcp"}

# 5. 查看索引统计
curl http://localhost:8000/api/v1/workspace
# → {"success":true,"data":{"vector_count":256,"node_count":128,"edge_count":342}}

# 6. 语义搜索
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "代码分析", "top_k": 5}'

# 7. 查看调用链
curl "http://localhost:8000/api/v1/callchain/Analyze?depth=2"

# 8. 停止服务
kill %1
```

---

## 5. CLI 命令详解

### 5.1 `store` — 索引仓库

分析代码仓库并构建知识库。

```bash
# 索引远程仓库
./sourcelex store --repo https://github.com/user/repo.git --branch main

# 索引本地仓库
./sourcelex store --path /path/to/repo

# 强制重建（清除全部旧数据）
./sourcelex store --path . --force
```

| 参数 | 缩写 | 说明 | 默认值 |
|------|------|------|--------|
| `--repo` | | Git 仓库 URL（HTTPS/SSH） | — |
| `--path` | | 本地仓库路径 | — |
| `--branch` | | 分支名 | `main` |
| `--force` | `-f` | 强制重建，删除现有数据 | `false` |

**索引过程中会输出：**
- 文件扫描统计（新增/修改/跳过）
- 实体提取统计（函数、类、方法数量）
- 调用关系数量
- 向量嵌入进度
- 系统资源占用（内存、CPU、Goroutine）

**增量更新：** 不带 `--force` 时，只处理自上次索引后变更的文件（通过文件哈希+修改时间检测）。

### 5.2 `serve` — 启动 MCP 服务

启动 HTTP 服务器，提供 REST API 和 MCP 协议端点。

```bash
./sourcelex serve                          # 默认 0.0.0.0:8000
./sourcelex serve --port 9000              # 指定端口
./sourcelex serve --host 127.0.0.1         # 仅本地访问
```

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--host` | 监听地址 | `0.0.0.0` |
| `--port` | 监听端口 | `8000` |

**启动流程：**
1. 加载 HuggingFace 嵌入模型
2. 加载向量库（`./data/vectors/`）
3. 加载图数据库（`./data/graph.db`）
4. 加载 Git 仓库（如有 `metadata.json`）
5. 初始化 AI Agent（如已配置 LLM）
6. 启动 Gin HTTP 服务器
7. 注册 Web UI 路由

支持 **Ctrl+C 优雅关闭**（等待最多 5 秒完成进行中的请求）。

### 5.3 `version` — 查看版本

```bash
./sourcelex version
```

---

## 6. API 接口参考

**基础 URL：** `http://localhost:8000`

**通用响应格式：**

```json
{
  "success": true,
  "data": { ... },
  "error": ""
}
```

### 6.1 健康检查与工作空间

#### 健康检查

```
GET /health
```

```json
{"status": "ok", "service": "sourcelex-mcp"}
```

#### 工作空间统计

```
GET /api/v1/workspace
```

```json
{
  "success": true,
  "data": {
    "vector_count": 256,
    "node_count": 128,
    "edge_count": 342
  }
}
```

---

### 6.2 语义搜索

```
POST /api/v1/search/semantic
```

用自然语言查找代码实体。

**请求体：**

```json
{
  "query": "处理用户登录验证的函数",
  "top_k": 5
}
```

| 字段 | 类型 | 必需 | 说明 | 默认值 |
|------|------|------|------|--------|
| `query` | string | 是 | 自然语言查询 | — |
| `top_k` | int | 否 | 返回结果数量 | 10 |
| `filter` | object | 否 | 元数据过滤条件 | — |

**响应：**

```json
{
  "success": true,
  "data": [
    {
      "entity_id": "auth.validate_user",
      "name": "validate_user",
      "type": "function",
      "file_path": "src/auth/login.py",
      "start_line": 45,
      "end_line": 62,
      "score": 0.89,
      "metadata": { "name": "validate_user", "type": "function", "file_path": "..." }
    }
  ]
}
```

**curl 示例：**

```bash
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "数据库连接池管理", "top_k": 10}'
```

---

### 6.3 混合搜索

```
POST /api/v1/search/hybrid
```

向量语义搜索 + 关键词重排序，提升搜索准确性。

**请求体：**

```json
{
  "query": "authentication login handler",
  "top_k": 10,
  "filters": {}
}
```

响应格式同语义搜索。

---

### 6.4 上下文感知搜索

```
POST /api/v1/search/context
```

在语义搜索基础上自动展开调用关系上下文。

**请求体：**

```json
{
  "query": "错误处理",
  "top_k": 5
}
```

响应格式同语义搜索。

---

### 6.5 实体查询

```
GET /api/v1/entity/:id
```

获取单个代码实体的详细信息。

| 参数 | 位置 | 说明 |
|------|------|------|
| `id` | URL 路径 | 实体 QualifiedName（如 `Analyze` 或 `Server.Start`） |

**响应：**

```json
{
  "success": true,
  "data": {
    "id": "Server.Start",
    "name": "Start",
    "type": "method",
    "file_path": "internal/mcp/server.go",
    "start_line": 156,
    "end_line": 168,
    "signature": "func (s *Server) Start() error"
  }
}
```

**实体 ID 命名规则：**
- 顶层函数：`FunctionName`（如 `Analyze`、`main`）
- 类/结构体方法：`ClassName.MethodName`（如 `Server.Start`、`KnowledgeStore.Search`）

---

### 6.6 调用图（JSON 格式）

#### 调用关系全图

```
GET /api/v1/callmap/:id?depth=2
```

同时获取调用者和被调用者。

| 参数 | 位置 | 说明 | 默认值 |
|------|------|------|--------|
| `id` | URL 路径 | 实体 ID | — |
| `depth` | Query | 遍历深度 | 1 |

**响应：**

```json
{
  "success": true,
  "data": {
    "entity_id": "Server.Start",
    "callers": [
      {
        "id": "main",
        "name": "main",
        "type": "function",
        "file_path": "cmd/sourcelex/main.go",
        "start_line": 10,
        "end_line": 25,
        "signature": "func main()"
      }
    ],
    "callees": [
      {
        "id": "Server.setupRoutes",
        "name": "setupRoutes",
        "type": "method",
        "file_path": "internal/mcp/server.go",
        "start_line": 107,
        "end_line": 153
      }
    ]
  }
}
```

#### 查询调用者

```
GET /api/v1/callers/:id?depth=1
```

谁调用了这个函数？

#### 查询被调用者

```
GET /api/v1/callees/:id?depth=1
```

这个函数调用了谁？

两者响应格式为实体数组：

```json
{
  "success": true,
  "data": [
    { "id": "...", "name": "...", "type": "...", "file_path": "...", "start_line": 0, "end_line": 0 }
  ]
}
```

---

### 6.7 紧凑调用链（AI 优化格式）

专为 AI 助手设计，比 JSON 格式节省约 95% 的 Token。

#### 紧凑调用链

```
GET /api/v1/callchain/:id?depth=2
```

| 参数 | 位置 | 说明 | 默认值 |
|------|------|------|--------|
| `id` | URL 路径 | 实体 ID | — |
| `depth` | Query | 遍历深度 | 1 |

**响应：**

```json
{
  "success": true,
  "data": {
    "entity_id": "Analyze",
    "depth": 2,
    "text": "Analyze [file: internal/analyzer/analyzer.go:30-80]\n  ├─ calls: Parse, ExtractEntities, ExtractRelations\n  │  ├─ Parse [parser/parser.go:15-40]\n  │  │  └─ calls: NewParser, ParseFile\n  │  └─ ExtractEntities [entity/extractor.go:20-60]\n  │     └─ calls: extractGo, extractPython\n  └─ called_by: ExecuteStore"
  }
}
```

#### 全局调用图摘要

```
GET /api/v1/graph/summary?file=pattern
```

按文件分组的邻接表格式，一次请求了解全部调用关系。100 个函数约 1000 tokens（JSON 需要 10000+）。

| 参数 | 位置 | 说明 |
|------|------|------|
| `file` | Query | 文件路径过滤（可选） |

**响应：**

```json
{
  "success": true,
  "data": {
    "text": "internal/analyzer/analyzer.go:\n  Analyze: [Parse, ExtractEntities, ExtractRelations]\n  ExtractEntities: [extractGo, extractPython, extractJava]\ninternal/mcp/server.go:\n  Start: [setupRoutes, ListenAndServe]\n  setupRoutes: [handleHealth, handleSemanticSearch, ...]",
    "node_count": 128,
    "edge_count": 342
  }
}
```

---

### 6.8 图分析

#### 完整功能图谱

```
GET /api/v1/graph/function?type=function&file=xxx
```

获取所有节点和边，适合可视化。

| 参数 | 位置 | 说明 |
|------|------|------|
| `type` | Query | 按���体类型过滤：`function`、`class`、`method`（可选） |
| `file` | Query | 按文件路径过滤（可选） |

**响应：**

```json
{
  "success": true,
  "data": {
    "nodes": [
      { "id": "Analyze", "name": "Analyze", "type": "function", "file_path": "...", "start_line": 30, "end_line": 80 }
    ],
    "edges": [
      { "source": "Analyze", "target": "Parse", "type": "calls" }
    ],
    "stats": { "node_count": 42, "edge_count": 58 }
  }
}
```

#### 局部子图

```
GET /api/v1/graph/subgraph/:id?depth=2
```

获取某个节点周围的局部网络。

**响应：**

```json
{
  "success": true,
  "data": {
    "center_id": "Analyze",
    "depth": 2,
    "nodes": [ ... ],
    "edges": [ ... ]
  }
}
```

#### 路径查找

```
GET /api/v1/graph/path?from=main&to=Parse
```

查找两个函数之间的调用路径（BFS 最短路径）。

| 参数 | 位置 | 必需 | 说明 |
|------|------|------|------|
| `from` | Query | 是 | 起始实体 ID |
| `to` | Query | 是 | 目标实体 ID |

**响应：**

```json
{
  "success": true,
  "data": {
    "source": "main",
    "target": "Parse",
    "path": ["main", "Analyze", "Parse"],
    "edges": [
      { "source": "main", "target": "Analyze", "type": "calls" },
      { "source": "Analyze", "target": "Parse", "type": "calls" }
    ]
  }
}
```

#### 循环依赖检测

```
GET /api/v1/graph/cycles
```

检测调用图中的循环依赖。

```json
{
  "success": true,
  "data": {
    "cycles": [
      ["funcA", "funcB", "funcC", "funcA"]
    ],
    "cycle_count": 1
  }
}
```

#### 拓扑排序

```
GET /api/v1/graph/topo-sort
```

对调用图进行拓扑排序（用于分析依赖顺序）。

```json
{
  "success": true,
  "data": {
    "sorted": ["init", "setup", "process", "cleanup", "main"],
    "node_count": 5
  }
}
```

---

### 6.9 RAG 上下文组装

```
POST /api/v1/rag/context
```

多源检索 + 上下文组装，专为 LLM 设计。将向量检索、调用图、文件上下文组合成结构化文本。

**请求体：**

```json
{
  "query": "用户认证是怎么实现的",
  "top_k": 5,
  "min_score": 0.3,
  "include_call_graph": true,
  "call_graph_depth": 2,
  "include_file_context": true,
  "enable_reranking": true,
  "max_context_length": 16000
}
```

| 字段 | 类型 | 必需 | 说明 | 默认值 |
|------|------|------|------|--------|
| `query` | string | 是 | 查询问题 | — |
| `top_k` | int | 否 | 向量检索数量 | 10 |
| `min_score` | float | 否 | 最低相似度阈值 | 0.3 |
| `include_call_graph` | bool | 否 | 包含调用关系上下文 | false |
| `call_graph_depth` | int | 否 | 调用图遍历深度 | 1 |
| `include_file_context` | bool | 否 | 包含同文件其他实体 | false |
| `enable_reranking` | bool | 否 | 启用代码感知重排 | false |
| `filters` | object | 否 | 元数据过滤 | — |
| `max_context_length` | int | 否 | 最大上下文长度（字符） | 16000 |

**响应：**

```json
{
  "success": true,
  "data": {
    "context": "[Function] authenticate\nFile: auth/user.go (lines 45-78)\n\n```go\nfunc authenticate(user string, password string) (string, error) {\n    ...\n}\n```\n\n**Call Graph:**\n- Called by: main, http_handler\n- Calls: verifyPassword, createSession\n",
    "sources": [
      {
        "entity_id": "authenticate",
        "name": "authenticate",
        "type": "function",
        "file_path": "auth/user.go",
        "start_line": 45,
        "end_line": 78,
        "score": 0.95,
        "reason": "向量语义匹配"
      }
    ],
    "stats": {
      "vector_results": 5,
      "graph_results": 3,
      "file_results": 2,
      "total_sources": 10,
      "context_length": 3500,
      "reranked_results": 5
    }
  }
}
```

**RAG 管线处理流程：**

```
查询 → ① 向量检索 → ② 代码重排序(可选) → ③ 调用图展开(可选)
     → ④ 文件上下文(可选) → ⑤ 组装结构化文本 → ⑥ 长度裁剪
```

1. **向量检索** — 找到语义最相似的代码实体
2. **代码重排序** — 基于代码特征重排，提升相关性（检索 3x top_k，保留 top_k）
3. **调用图展开** — 添加 caller/callee 关系摘要（紧凑文本，非完整代码）
4. **文件上下文** — 添加同文件内相关实体
5. **组装** — 带来源标注的结构化文本
6. **长度控制** — 裁剪到 `max_context_length`，优先保留高分结果

**curl 示例：**

```bash
# 最简请求（仅语义检索）
curl -X POST http://localhost:8000/api/v1/rag/context \
  -H "Content-Type: application/json" \
  -d '{"query": "错误处理逻辑", "top_k": 5}'

# 完整请求（启用所有特性）
curl -X POST http://localhost:8000/api/v1/rag/context \
  -H "Content-Type: application/json" \
  -d '{
    "query": "数据库查询如何实现",
    "top_k": 5,
    "include_call_graph": true,
    "call_graph_depth": 2,
    "include_file_context": true,
    "enable_reranking": true,
    "max_context_length": 8000
  }'
```

---

### 6.10 Git 历史分析

> 需要先通过 `store` 命令索引过仓库，Git 历史功能才可用。

#### 提交历史搜索

```
GET /api/v1/history/commits?limit=20&author=xxx&keyword=xxx&file=xxx&since=2024-01-01&until=2024-12-31
```

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `limit` | 最大返回数 | 20 |
| `author` | 按作者过滤 | — |
| `keyword` | 按提交信息搜索 | — |
| `file` | 按文件路径过滤 | — |
| `since` | 起始日期（`YYYY-MM-DD` 或 RFC3339） | — |
| `until` | 截止日期 | — |

**响应：**

```json
{
  "success": true,
  "data": {
    "commits": [
      {
        "hash": "abc123def456...",
        "short_hash": "abc123d",
        "author": "John Doe",
        "email": "john@example.com",
        "message": "feat: add authentication",
        "timestamp": "2024-01-15T10:30:00Z",
        "files": [
          { "path": "src/auth.go", "status": "modified", "additions": 15, "deletions": 3 }
        ]
      }
    ],
    "count": 1
  }
}
```

#### 提交详情

```
GET /api/v1/history/commit/:hash
```

```json
{
  "success": true,
  "data": {
    "commit": { "hash": "...", "author": "...", "message": "...", "files": [...] },
    "stats": { "files_changed": 3, "total_additions": 45, "total_deletions": 12 }
  }
}
```

#### 文件变更历史

```
GET /api/v1/history/file?path=internal/mcp/server.go&limit=20
```

```json
{
  "success": true,
  "data": {
    "path": "internal/mcp/server.go",
    "entries": [
      {
        "commit": { "hash": "...", "author": "...", "message": "...", "timestamp": "..." },
        "change": { "path": "internal/mcp/server.go", "status": "modified", "additions": 10, "deletions": 2 }
      }
    ]
  }
}
```

#### 文件 Blame（逐行溯源）

```
GET /api/v1/history/blame?path=internal/mcp/server.go
```

```json
{
  "success": true,
  "data": {
    "path": "internal/mcp/server.go",
    "lines": [
      {
        "line_number": 1,
        "hash": "abc123d",
        "author": "Jane Smith",
        "timestamp": "2024-01-20T14:00:00Z",
        "content": "package mcp"
      }
    ]
  }
}
```

#### 实体变更历史

```
GET /api/v1/history/entity?id=Server.Start&limit=10
```

结合图存储（实体位置）和 Git 历史，追踪某个函数/方法的变更记录。

```json
{
  "success": true,
  "data": {
    "entity": { "id": "Server.Start", "name": "Start", "type": "method", "file_path": "...", "start_line": 156, "end_line": 168 },
    "commits": [ ... ],
    "count": 5
  }
}
```

---

## 7. MCP 协议集成（AI 助手对接）

Sourcelex 原生支持 [MCP（Model Context Protocol）](https://modelcontextprotocol.io/)，可直接对接 AI 编程助手。

### MCP 端点

| 端点 | 说明 |
|------|------|
| `GET /mcp/sse` | SSE 实时流（AI 助手连接入口） |
| `POST /mcp/request` | MCP 协议请求处理 |

### 在 Cursor 中使用

在 Cursor 的 Settings → MCP 中添加：

```json
{
  "mcpServers": {
    "repomind": {
      "url": "http://localhost:8000/mcp/sse"
    }
  }
}
```

配置后，Cursor 可以直接调用 Sourcelex 的语义搜索、调用图分析等能力来理解你的代码库。

### 在 Claude Desktop 中使用

在 Claude Desktop 配置文件（`claude_desktop_config.json`）中添加：

```json
{
  "mcpServers": {
    "repomind": {
      "url": "http://localhost:8000/mcp/sse"
    }
  }
}
```

### 自定义集成

通过 REST API 直接集成到任何系统：

```python
import requests

# 语义搜索
resp = requests.post("http://localhost:8000/api/v1/search/semantic",
    json={"query": "数据库连接池", "top_k": 5})
results = resp.json()["data"]
for r in results:
    print(f"{r['name']} - {r['file_path']}:{r['start_line']} (score: {r['score']:.2f})")

# 获取 RAG 上下文（供 LLM 使用）
resp = requests.post("http://localhost:8000/api/v1/rag/context",
    json={
        "query": "如何处理错误",
        "top_k": 5,
        "include_call_graph": True,
        "include_file_context": True
    })
context = resp.json()["data"]["context"]
# 将 context 作为上下文传给 LLM
```

---

## 8. AI Agent 对话功能

启用 Agent 后，Sourcelex 可以直接用自然语言回答关于代码的问题。

### 配置 Agent

在 `config.yaml` 中选择一个 LLM 提供方：

```yaml
# 方式一：OpenAI
agent:
  provider: "openai"
  openai:
    api_key: "sk-xxxxx"
    model: "gpt-4o"

# 方式二：Anthropic Claude
agent:
  provider: "anthropic"
  anthropic:
    api_key: "sk-ant-xxxxx"
    model: "claude-sonnet-4-20250514"

# 方式三：DeepSeek（OpenAI 兼容 API）
agent:
  provider: "openai"
  openai:
    api_key: "sk-xxxxx"
    model: "deepseek-chat"
    base_url: "https://api.deepseek.com/v1"

# 方式四：Ollama 本地模型
agent:
  provider: "openai"
  openai:
    api_key: "ollama"
    model: "llama3"
    base_url: "http://localhost:11434/v1"
```

### Agent 可用工具

Agent 内置了以下工具，会根据问题自动选择调用：

| 工具名 | 说明 |
|--------|------|
| `semantic_search` | 用自然语言搜索代码 |
| `get_entity` | 获取函数/类的详细信息 |
| `get_callers` | 查找谁调用了某个函数 |
| `get_callees` | 查找某个函数调用了谁 |
| `get_subgraph` | 获取函数的局部调用网络 |
| `find_path` | 查找两个函数之间的调用路径 |
| `get_workspace_stats` | 获取知识库统计信息 |
| `detect_cycles` | 检测循环依赖 |

### 使用 Web UI

启动服务后，访问 `http://localhost:8000` 即可使用 Web 对话界面。

**可以问的问题示例：**

- "用户认证的流程是怎样的？"
- "哪些函数调用了数据库查询？"
- "main 函数到 handleRequest 之间的调用路径是什么？"
- "这个项目有循环依赖吗？"
- "搜索所有跟文件解析相关的代码"

---

## 9. 高级特性

### 9.1 增量索引

Sourcelex 使用文件哈希 + 修改时间（mtime）进行变更检测：

- **首次索引**：全量分析所有代码文件
- **后续索引**：只重新处理修改过的文件，未变更文件完全跳过
- **强制重建**：`--force` 清除全部数据后重新索引

增量索引大幅减少 HuggingFace API 调用次数和处理时间。

### 9.2 支持的语言

| 语言 | 文件扩展名 | 提取的实体类型 |
|------|-----------|---------------|
| Python | `.py` | function、class、method |
| Go | `.go` | function、struct |
| Java | `.java` | class、method |
| JavaScript | `.js` | function |
| TypeScript | `.ts` | function、class |
| C | `.c` | function |
| C++ | `.cpp`、`.cc`、`.cxx` | class、method |

### 9.3 资源监控

索引过程中自动监控并输出系统资源：

- 系统内存（总量/已用/可用）
- 进程内存（RSS / VMS）
- CPU 占用率
- Go 运行时内存 / Goroutine 数量

### 9.4 存储后端

| 组件 | 默认 | 可选 | 说明 |
|------|------|------|------|
| 向量库 | chromem-go（本地） | Qdrant（远程） | 无需外部依赖 |
| 图数据库 | SQLite（本地） | 内存模式 | ACID 事务 |
| 嵌入模型 | HuggingFace API | — | 默认 all-MiniLM-L6-v2 |

### 9.5 数据目录结构

```
./data/
├── vectors/          # chromem-go 向量数据库
├── graph.db          # SQLite 调用图
└── metadata.json     # 仓库元信息（URL、分支、索引时间）

./temp/repos/
└── xxx.git/          # 克隆的仓库
```

### 9.6 CORS 跨域

服务器默认启用全跨域支持：

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
```

---

## 10. 故障排查

### CGO 编译错误

**错误：** `gcc: command not found` 或 `CGO_ENABLED is not set`

**解决：**
```bash
# 确保 GCC 已安装
gcc --version

# 显式启用 CGO
CGO_ENABLED=1 go build -o sourcelex ./cmd/sourcelex

# Windows PowerShell
$env:Path = "C:\msys64\ucrt64\bin;" + $env:Path
$env:CGO_ENABLED = "1"
```

### HuggingFace API 错误

**错误：** `API 错误 (401): Invalid token`

**解决：**
1. 检查 `config.yaml` 中的 `api_token` 是否正确
2. 在 [huggingface.co/settings/tokens](https://huggingface.co/settings/tokens) 验证 Token 有效性
3. 确认 Token 有 Read 权限

### 端口占用

**错误：** `bind: address already in use`

**解决：**
```bash
# 换端口
./sourcelex serve --port 9000

# 或查找占用进程
lsof -i :8000           # macOS / Linux
netstat -ano | findstr :8000  # Windows
```

### 实体提取为空

**可能原因：**
- 文件扩展名不在支持列表中
- 代码文件为空或语法错误严重

**排查：** 开启 debug 日志查看详情：
```yaml
logging:
  level: "debug"
```

### SQLite 锁错误

**错误：** `database is locked`

**可能原因：** 有未正确关闭的数据库连接（如上次程序异常退出）。

**解决：** 确保没有其他 Sourcelex 进程在运行，然后重新启动。

### 服务无响应

1. 检查服务是否启动：`curl http://localhost:8000/health`
2. 检查终端日志输出
3. 确认防火墙/代理设置
4. 确认 `./data/` 目录中有索引数据

---

## 附录：接口速查表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/api/v1/workspace` | 知识库统计 |
| POST | `/api/v1/search/semantic` | 语义搜索 |
| POST | `/api/v1/search/hybrid` | 混合搜索 |
| POST | `/api/v1/search/context` | 上下文感知搜索 |
| GET | `/api/v1/entity/:id` | 实体详情 |
| POST | `/api/v1/rag/context` | RAG 上下文组装 |
| GET | `/api/v1/callmap/:id` | 调用关系（双向） |
| GET | `/api/v1/callers/:id` | 调用者 |
| GET | `/api/v1/callees/:id` | 被调用者 |
| GET | `/api/v1/callchain/:id` | 紧凑调用链（AI 优化） |
| GET | `/api/v1/graph/summary` | 全局调用图摘要（AI 优化） |
| GET | `/api/v1/graph/function` | 完整功能图谱 |
| GET | `/api/v1/graph/subgraph/:id` | 局部子图 |
| GET | `/api/v1/graph/path` | 路径查找 |
| GET | `/api/v1/graph/cycles` | 循环依赖检测 |
| GET | `/api/v1/graph/topo-sort` | 拓扑排序 |
| GET | `/api/v1/history/commits` | 提交历史 |
| GET | `/api/v1/history/commit/:hash` | 提交详情 |
| GET | `/api/v1/history/file` | 文件变更历史 |
| GET | `/api/v1/history/blame` | 文件 Blame |
| GET | `/api/v1/history/entity` | 实体变更历史 |
| GET | `/mcp/sse` | MCP SSE 流 |
| POST | `/mcp/request` | MCP 协议请求 |

## 附录：环境变量速查

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SOURCELEX_VECTOR_STORE_HUGGINGFACE_API_TOKEN` | HuggingFace API Token | — |
| `SOURCELEX_AGENT_OPENAI_API_KEY` | OpenAI API Key | — |
| `SOURCELEX_AGENT_ANTHROPIC_API_KEY` | Anthropic API Key | — |
| `SOURCELEX_MCP_PORT` | 服务端口 | 8000 |
| `SOURCELEX_MCP_HOST` | 服务监听地址 | 0.0.0.0 |
| `SOURCELEX_LOGGING_LEVEL` | 日志级别 | info |
| `SOURCELEX_PATHS_DATA_DIR` | 数据目录 | ./data |
| `SOURCELEX_PATHS_TEMP_DIR` | 临时目录 | ./temp |
| `CGO_ENABLED` | CGO 开关（构建时需要） | 1 |
