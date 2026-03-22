<div align="center">

# Sourcelex

**将任意代码仓库转化为 AI 可搜索、可查询的知识库。**

语义搜索、调用图分析、MCP 协议 —— 一个二进制文件，开箱即用。

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-Protocol-8A2BE2)](https://modelcontextprotocol.io/)

[快速开始](#-快速开始) · [详细文档](docs/MANUAL.md) · [API 参考](#-api-参考) · [MCP 集成](#-mcp-集成)

---

[English](README.md) | **中文**

</div>

---

## 为什么选择 Sourcelex？

AI 编程助手很强大，但在大型代码库中容易丢失上下文。Sourcelex 将你的仓库索引为向量 + 图谱知识库，让 AI 工具可以实时查询。

```
你: "这个项目的用户认证是怎么实现的？"

Sourcelex → 向量搜索 → 找到 authenticate()、verifyPassword()、createSession()
         → 图谱遍历 → 发现调用链: main → handler → authenticate → verifyPassword
         → RAG 组装 → 返回结构化上下文：代码、调用关系、文件位置
         → AI 助手 → 给出准确、有据可查的回答
```

**核心优势：**

- **零外部依赖** —— SQLite + chromem-go，无需 Docker，无需数据库服务
- **单文件部署** —— `go build` 即可，一个二进制搞定一切
- **MCP 原生支持** —— 与 Cursor、Claude Desktop 及任何 MCP 客户端无缝对接
- **调用图感知** —— 不只是文本搜索，理解函数间的调用关系
- **增量索引** —— 再次索引只处理变更文件，大幅节省时间

---

## 功能特性

| | 功能 | 说明 |
|---|------|------|
| **搜索** | 语义、混合、上下文感知 | 用自然语言找代码，向量相似度 + 关键词重排序 |
| **调用图** | 调用者、被调用者、路径、循环 | 双向调用链遍历，支持自定义深度 |
| **多语言** | 7 种语言 | Python、Go、Java、JavaScript、TypeScript、C、C++，基于 Tree-sitter |
| **RAG 管线** | 为 LLM 组装上下文 | 向量检索 → 图谱展开 → 文件上下文 → 重排序 → 长度控制 |
| **MCP 协议** | AI 助手集成 | 原生 SSE/HTTP 支持 Cursor、Claude Desktop 及自定义客户端 |
| **AI Agent** | 内置对话问答 | OpenAI、Anthropic、DeepSeek、Ollama —— 直接向代码提问 |
| **Git 历史** | 提交记录、Blame、实体追踪 | 谁改了这个函数？什么时候？同时还改了什么？ |
| **图分析** | 算法工具集 | 循环检测、拓扑排序、BFS 路径查找、子图提取 |

---

## 快速开始

### 前置条件

- **Go 1.21+**
- **GCC** —— Tree-sitter CGO 编译需要（[安装指南](#gcc-安装)）
- **HuggingFace API Token** —— 免费申请：[huggingface.co/settings/tokens](https://huggingface.co/settings/tokens)

### 安装

```bash
git clone https://github.com/Lion-Leporidae/sourcelex.git
cd sourcelex
go build -o sourcelex ./cmd/sourcelex
```

### 配置

```bash
cat > config.yaml << 'EOF'
vector_store:
  huggingface:
    api_token: "hf_你的token"
EOF
```

### 运行

```bash
# 1. 索引仓库
./sourcelex store --repo https://github.com/gin-gonic/gin.git --branch master

# 2. 启动服务
./sourcelex serve --port 8000

# 3. 搜索代码
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "HTTP路由处理", "top_k": 5}'
```

就这样。你的代码库现在可以被 AI 搜索了。

---

## 工作原理

```
                        ┌─────────────┐
                        │  Git 仓库   │
                        └──────┬──────┘
                               │
                    ┌──────────▼──────────┐
                    │    文件扫描器        │  基于哈希+修改时间的增量检测
                    │   (增量模式)         │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │  Tree-sitter AST    │  Python, Go, Java, JS/TS, C, C++
                    │  语法解析器          │
                    └──────────┬──────────┘
                               │
                ┌──────────────┼──────────────┐
                │              │              │
        ┌───────▼──────┐ ┌────▼─────┐ ┌──────▼───────┐
        │  代码实体     │ │ 调用关系  │ │  代码分块    │
        │  函数/类/方法 │ │ 调用者 →  │ │  用于嵌入    │
        │              │ │ 被调用者  │ │              │
        └───────┬──────┘ └────┬─────┘ └──────┬───────┘
                │              │              │
                │              │     ┌────────▼────────┐
                │              │     │  HuggingFace    │
                │              │     │  嵌入向量 API    │
                │              │     └────────┬────────┘
                │              │              │
         ┌──────▼──────┐ ┌────▼─────┐ ┌──────▼───────┐
         │   SQLite    │ │  SQLite  │ │  chromem-go  │
         │   (节点)    │ │  (边)    │ │   (向量)     │
         └──────┬──────┘ └────┬─────┘ └──────┬───────┘
                │              │              │
                └──────────────┼──────────────┘
                               │
                    ┌──────────▼──────────┐
                    │   MCP 服务 (Gin)    │  23 个 API 端点
                    │   REST + SSE        │  8 大类别
                    └─────────────────────┘
```

---

## MCP 集成

几秒钟内将 Sourcelex 连接到你的 AI 编程助手。

### Cursor

在 MCP 设置中添加：

```json
{
  "mcpServers": {
    "repomind": {
      "url": "http://localhost:8000/mcp/sse"
    }
  }
}
```

### Claude Desktop

在 `claude_desktop_config.json` 中添加：

```json
{
  "mcpServers": {
    "repomind": {
      "url": "http://localhost:8000/mcp/sse"
    }
  }
}
```

连接后，你的 AI 助手即可通过 MCP 协议进行语义搜索、调用链追踪、Git 历史查询等操作。

---

## API 参考

所有接口统一返回 `{"success": bool, "data": ..., "error": "..."}`。

### 搜索

| 方法 | 端点 | 说明 |
|------|------|------|
| POST | `/api/v1/search/semantic` | 语义向量搜索 |
| POST | `/api/v1/search/hybrid` | 向量 + 关键词重排序 |
| POST | `/api/v1/search/context` | 搜索并展开调用图上下文 |

```bash
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "数据库连接", "top_k": 5}'
```

### 调用图

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | `/api/v1/callmap/:id?depth=N` | 调用者 + 被调用者（双向） |
| GET | `/api/v1/callers/:id?depth=N` | 谁调用了这个函数？ |
| GET | `/api/v1/callees/:id?depth=N` | 这个函数调用了谁？ |
| GET | `/api/v1/callchain/:id?depth=N` | **紧凑文本格式**（节省 95% Token） |
| GET | `/api/v1/graph/summary?file=X` | **全图邻接表摘要** |

### 图分析

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | `/api/v1/graph/function?type=X&file=X` | 完整功能图谱（节点 + 边） |
| GET | `/api/v1/graph/subgraph/:id?depth=N` | 局部子图 |
| GET | `/api/v1/graph/path?from=X&to=Y` | 两个函数间的最短调用路径 |
| GET | `/api/v1/graph/cycles` | 循环依赖检测 |
| GET | `/api/v1/graph/topo-sort` | 拓扑排序 |

### RAG（为 LLM 组装上下文）

| 方法 | 端点 | 说明 |
|------|------|------|
| POST | `/api/v1/rag/context` | 多源检索 + 上下文组装 |

```bash
curl -X POST http://localhost:8000/api/v1/rag/context \
  -H "Content-Type: application/json" \
  -d '{
    "query": "认证是怎么实现的",
    "top_k": 5,
    "include_call_graph": true,
    "call_graph_depth": 2,
    "include_file_context": true,
    "enable_reranking": true,
    "max_context_length": 16000
  }'
```

RAG 管线流程：**向量检索 → 重排序 → 调用图展开 → 文件上下文 → 组装 → 长度控制**。

### Git 历史

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | `/api/v1/history/commits?limit=N&author=X` | 提交历史（支持多条件过滤） |
| GET | `/api/v1/history/commit/:hash` | 提交详情与统计 |
| GET | `/api/v1/history/file?path=X` | 文件变更历史 |
| GET | `/api/v1/history/blame?path=X` | 逐行 Blame 溯源 |
| GET | `/api/v1/history/entity?id=X` | 实体变更历史 |

### 其他

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/api/v1/workspace` | 索引统计信息 |
| GET | `/api/v1/entity/:id` | 实体详情 |
| GET | `/mcp/sse` | MCP SSE 流 |
| POST | `/mcp/request` | MCP 协议请求 |

> 完整的 API 文档（含请求/响应结构）：[docs/MANUAL.md](docs/MANUAL.md)

---

## 配置说明

使用 YAML 配置文件，支持环境变量覆盖。

**配置文件加载优先级：**
1. `--config` 命令行参数
2. `./configs/config.yaml`
3. `./config.yaml`
4. `~/.repomind/config.yaml`

### 最小配置

```yaml
vector_store:
  huggingface:
    api_token: "hf_xxxxx"
```

### 完整配置

```yaml
paths:
  data_dir: "./data"
  temp_dir: "./temp"

git:
  clone_depth: 1
  credentials:
    # github.com: "ghp_xxxx"

vector_store:
  type: "chromem"                    # chromem（本地）| qdrant（远程）
  embedder_type: "huggingface"
  chunk_size: 512
  chunk_overlap: 50
  huggingface:
    api_token: "hf_xxxxx"
    model_id: "sentence-transformers/all-MiniLM-L6-v2"
    dimension: 384
  qdrant:                            # 仅当 type: "qdrant" 时需要
    host: "localhost"
    port: 6334
    collection_name: "code_vectors"

graph_store:
  type: "sqlite"                     # sqlite | memory

mcp:
  host: "0.0.0.0"
  port: 8000

agent:
  provider: ""                       # openai | anthropic | ""（留空禁用）
  openai:
    api_key: ""
    model: "gpt-4o"
    base_url: ""                     # 自定义端点，用于 DeepSeek、Ollama 等
  anthropic:
    api_key: ""
    model: "claude-sonnet-4-20250514"

logging:
  level: "info"
  format: "text"
```

### 环境变量

使用 `SOURCELEX_` 前缀覆盖任意配置项：

```bash
export SOURCELEX_VECTOR_STORE_HUGGINGFACE_API_TOKEN=hf_xxxxx
export SOURCELEX_AGENT_OPENAI_API_KEY=sk-xxxxx
export SOURCELEX_MCP_PORT=9000
```

---

## AI Agent 对话

启用内置 AI 助手，用自然语言向代码库提问。

```yaml
# OpenAI
agent:
  provider: "openai"
  openai: { api_key: "sk-xxx", model: "gpt-4o" }

# Anthropic
agent:
  provider: "anthropic"
  anthropic: { api_key: "sk-ant-xxx", model: "claude-sonnet-4-20250514" }

# DeepSeek / Ollama / 任何 OpenAI 兼容 API
agent:
  provider: "openai"
  openai: { api_key: "sk-xxx", model: "deepseek-chat", base_url: "https://api.deepseek.com/v1" }
```

Agent 会自动调用以下工具来回答问题：

| 工具 | 说明 |
|------|------|
| `semantic_search` | 用自然语言搜索代码 |
| `get_entity` | 获取函数/类详情 |
| `get_callers` / `get_callees` | 追踪调用关系 |
| `get_subgraph` | 局部调用网络 |
| `find_path` | 两个函数间的调用路径 |
| `detect_cycles` | 检测循环依赖 |
| `get_workspace_stats` | 知识库统计 |

启动服务后访问 `http://localhost:8000` 即可使用 Web 对话界面。

---

## CLI 命令

```bash
sourcelex store   --path <目录>                     # 索引本地仓库
sourcelex store   --repo <URL> --branch <分支>      # 索引远程仓库
sourcelex store   --path . --force                   # 强制全量重建
sourcelex serve   --host 0.0.0.0 --port 8000        # 启动 MCP 服务
sourcelex version                                    # 查看版本信息
```

---

## 项目结构

```
sourcelex/
├── cmd/sourcelex/              # 程序入口
├── internal/
│   ├── agent/                 # AI 对话 Agent
│   │   └── llm/               #   OpenAI / Anthropic 适配
│   ├── analyzer/              # 代码分析引擎
│   │   ├── parser/            #   Tree-sitter AST 解析
│   │   ├── entity/            #   实体提取（函数/类/方法）
│   │   ├── relation/          #   调用关系提取
│   │   └── chunker/           #   代码分块（用于嵌入）
│   ├── cmd/                   # CLI 命令（Cobra）
│   ├── config/                # 配置管理（Viper）
│   ├── git/                   # Git 操作（go-git）
│   ├── logger/                # 结构化日志（Zap）
│   ├── mcp/                   # MCP 服务（Gin）—— 23 个端点
│   ├── monitor/               # 资源监控
│   ├── store/                 # 存储门面
│   │   ├── vector/            #   chromem-go / Qdrant
│   │   └── graph/             #   SQLite / 内存
│   └── web/                   # Web 界面
├── configs/                   # 配置模板
├── docs/                      # 文档
└── go.mod
```

---

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.25 |
| CLI | [Cobra](https://github.com/spf13/cobra) |
| 配置 | [Viper](https://github.com/spf13/viper) |
| 日志 | [Zap](https://go.uber.org/zap) |
| HTTP | [Gin](https://github.com/gin-gonic/gin) |
| Git | [go-git](https://github.com/go-git/go-git) |
| AST 解析 | [go-tree-sitter](https://github.com/smacker/go-tree-sitter) |
| 向量存储 | [chromem-go](https://github.com/philippgille/chromem-go)（默认）/ [Qdrant](https://qdrant.tech/) |
| 图存储 | SQLite（默认）/ 内存模式 |
| 嵌入模型 | [HuggingFace Inference API](https://huggingface.co/inference-api) |

---

## 支持的语言

| 语言 | 文件扩展名 | 提取的实体类型 |
|------|-----------|---------------|
| Python | `.py` | function、class、method |
| Go | `.go` | function、struct |
| Java | `.java` | class、method |
| JavaScript | `.js` | function |
| TypeScript | `.ts` | function、class |
| C | `.c` | function |
| C++ | `.cpp` `.cc` `.cxx` | class、method |

---

## GCC 安装

Tree-sitter 依赖 CGO，需要 C 编译器：

| 平台 | 安装命令 |
|------|---------|
| macOS | `xcode-select --install` |
| Ubuntu / Debian | `sudo apt install build-essential` |
| CentOS / RHEL | `sudo yum install gcc` |
| Windows | 安装 [MSYS2](https://www.msys2.org/)，运行 `pacman -S mingw-w64-ucrt-x86_64-gcc`，将 `C:\msys64\ucrt64\bin` 加入 PATH |

---

## 参与贡献

欢迎贡献代码！流程如下：

1. Fork 本仓库
2. 创建特性分支（`git checkout -b feature/amazing-feature`）
3. 提交更改（`git commit -m 'feat: 添加某某功能'`）
4. 推送分支（`git push origin feature/amazing-feature`）
5. 发起 Pull Request

---

## 许可证

[MIT](LICENSE)

---

<div align="center">

**[详细文档](docs/MANUAL.md)** · **[报告问题](https://github.com/Lion-Leporidae/sourcelex/issues)** · **[功能建议](https://github.com/Lion-Leporidae/sourcelex/issues)**

</div>
