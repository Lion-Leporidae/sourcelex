<div align="center">

# Sourcelex

**Turn any codebase into a searchable, queryable knowledge base for AI.**

Semantic search, call graph analysis, and MCP protocol — all in a single binary.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-Protocol-8A2BE2)](https://modelcontextprotocol.io/)

[Quick Start](#-quick-start) · [Documentation](docs/MANUAL.md) · [API Reference](#-api-reference) · [MCP Integration](#-mcp-integration)

---

**English** | [中文](README_CN.md)

</div>

---

## Why Sourcelex?

AI coding assistants are powerful, but they lose context in large codebases. Sourcelex solves this by indexing your repository into a vector + graph knowledge base that AI tools can query in real-time.

```
You: "How does authentication work in this codebase?"

Sourcelex → vector search → finds authenticate(), verifyPassword(), createSession()
         → graph traversal → discovers call chain: main → handler → authenticate → verifyPassword
         → RAG assembly → returns structured context with code, relationships, and file locations
         → AI assistant → gives you an accurate, grounded answer
```

**Key differentiators:**

- **Zero external dependencies** — SQLite + chromem-go, no Docker, no database servers
- **Single binary** — `go build` and you're done
- **MCP native** — first-class integration with Cursor, Claude Desktop, and any MCP client
- **Call graph aware** — not just text search; understands function relationships
- **Multi-repo search** — search across all indexed repositories with a single query
- **API boundary detection** — auto-discovers HTTP route registrations via AST analysis
- **Incremental** — re-indexing only processes changed files

---

## Features

| | Feature | Description |
|---|---------|-------------|
| **Search** | Semantic, hybrid, context-aware | Natural language → code. Vector similarity + keyword reranking |
| **Call Graph** | Callers, callees, paths, cycles | Full bidirectional call chain traversal with configurable depth |
| **Multi-Repo** | Cross-repository search | Search across all indexed repos simultaneously with `scope=all` |
| **API Discovery** | HTTP route extraction | Auto-detect API endpoints from Gin, Echo, Flask, FastAPI, Express, Spring |
| **Multi-Language** | 7 languages | Python, Go, Java, JavaScript, TypeScript, C, C++ via Tree-sitter |
| **RAG Pipeline** | Context assembly for LLMs | Vector retrieval → graph expansion → file context → reranking → length control |
| **MCP Protocol** | AI assistant integration | Native SSE/HTTP support for Cursor, Claude Desktop, and custom clients |
| **AI Agent** | Built-in chat | OpenAI, Anthropic, DeepSeek, Ollama — ask questions about your code |
| **Git History** | Commits, blame, entity tracking | Who changed this function? When? What else was modified? |
| **Graph Analysis** | Algorithms | Cycle detection, topological sort, BFS path finding, subgraph extraction |

---

## Quick Start

### Prerequisites

- **Go 1.21+**
- **GCC** — required for Tree-sitter CGO ([install guide](#gcc-installation))
- **HuggingFace API Token** — free, get one at [huggingface.co/settings/tokens](https://huggingface.co/settings/tokens)

### Install

```bash
git clone https://github.com/Lion-Leporidae/sourcelex.git
cd sourcelex
go build -o sourcelex ./cmd/sourcelex
```

### Configure

```bash
cat > config.yaml << 'EOF'
vector_store:
  huggingface:
    api_token: "hf_your_token_here"
EOF
```

### Run

```bash
# 1. Index a repository
./sourcelex store --repo https://github.com/gin-gonic/gin.git --branch master

# 2. Start the server
./sourcelex serve --port 8000

# 3. Search
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "HTTP routing handler", "top_k": 5}'
```

That's it. Your codebase is now searchable by AI.

### Multi-Repo Mode

Index multiple repositories and search across all of them:

```bash
# Index multiple repos
./sourcelex store --repo https://github.com/gin-gonic/gin.git --branch master
./sourcelex store --repo https://github.com/labstack/echo.git --branch master

# Start server (auto-discovers all indexed repos)
./sourcelex serve --port 8000

# Search across all repos via MCP
# search(query="HTTP middleware", scope="all")

# Or via REST API
curl -X POST http://localhost:8000/api/v1/search/multi \
  -H "Content-Type: application/json" \
  -d '{"query": "HTTP middleware"}'
```

API endpoints (route registrations) are automatically extracted during indexing for frameworks like Gin, Echo, Flask, FastAPI, Express, and Spring.

---

## How It Works

```
                        ┌─────────────┐
                        │  Git Repo   │
                        └──────┬──────┘
                               │
                    ┌──────────▼──────────┐
                    │   File Scanner      │  hash + mtime change detection
                    │   (incremental)     │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │  Tree-sitter AST    │  Python, Go, Java, JS/TS, C, C++
                    │  Parser             │
                    └──────────┬──────────┘
                               │
            ┌──────────────┬───┴───┬──────────────┐
            │              │       │              │
    ┌───────▼──────┐ ┌────▼────┐ ┌▼──────────┐ ┌─▼────────────┐
    │   Entities   │ │Relations│ │  Chunks   │ │ API Endpoints│
    │  func/class/ │ │caller → │ │code blocks│ │ GET /api/... │
    │  method      │ │callee   │ │for embed  │ │ POST /api/.. │
    └───────┬──────┘ └────┬────┘ └─────┬─────┘ └──────┬───────┘
            │              │           │               │
            │              │  ┌────────▼────────┐      │
            │              │  │  HuggingFace    │      │
            │              │  │  Embedding API  │      │
            │              │  └────────┬────────┘      │
            │              │           │               │
     ┌──────▼──────┐ ┌────▼────┐ ┌────▼───────┐       │
     │   SQLite    │ │ SQLite  │ │ chromem-go │       │
     │   (nodes)   │ │ (edges) │ │ (vectors)  │       │
     └──────┬──────┘ └────┬────┘ └────┬───────┘       │
            │              │          │                │
            └──────────────┼──────────┼────────────────┘
                           │          │
                ┌──────────▼──────────▼──┐
                │   MCP Server (Gin)     │  25+ API endpoints
                │   REST + SSE           │  Multi-repo support
                └────────────────────────┘
```

---

## MCP Integration

Connect Sourcelex to your AI coding assistant in seconds.

### Cursor

Add to your MCP settings:

```json
{
  "mcpServers": {
    "sourcelex": {
      "url": "http://localhost:8000/mcp/sse"
    }
  }
}
```

### Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "sourcelex": {
      "url": "http://localhost:8000/mcp/sse"
    }
  }
}
```

Once connected, your AI assistant can search code semantically, trace call chains, query git history, and more — all through the MCP protocol.

---

## API Reference

All endpoints return `{"success": bool, "data": ..., "error": "..."}`.

### Search

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/search/semantic` | Semantic vector search |
| POST | `/api/v1/search/hybrid` | Vector + keyword reranking |
| POST | `/api/v1/search/context` | Search with call graph expansion |
| POST | `/api/v1/search/multi` | **Cross-repository search** (all indexed repos) |

```bash
# Single-repo search
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "database connection", "top_k": 5}'

# Multi-repo search
curl -X POST http://localhost:8000/api/v1/search/multi \
  -H "Content-Type: application/json" \
  -d '{"query": "authentication handler", "top_k": 5}'
```

### Call Graph

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/callmap/:id?depth=N` | Callers + callees (bidirectional) |
| GET | `/api/v1/callers/:id?depth=N` | Who calls this function? |
| GET | `/api/v1/callees/:id?depth=N` | What does this function call? |
| GET | `/api/v1/callchain/:id?depth=N` | **Compact text format** (95% fewer tokens) |
| GET | `/api/v1/graph/summary?file=X` | **Full graph as adjacency list** |

### Graph Analysis

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/graph/function?type=X&file=X` | Complete function graph (nodes + edges) |
| GET | `/api/v1/graph/subgraph/:id?depth=N` | Local neighborhood |
| GET | `/api/v1/graph/path?from=X&to=Y` | Shortest call path between functions |
| GET | `/api/v1/graph/cycles` | Circular dependency detection |
| GET | `/api/v1/graph/topo-sort` | Topological ordering |

### RAG (Context Assembly for LLMs)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/rag/context` | Multi-source context assembly |

```bash
curl -X POST http://localhost:8000/api/v1/rag/context \
  -H "Content-Type: application/json" \
  -d '{
    "query": "how does auth work",
    "top_k": 5,
    "include_call_graph": true,
    "call_graph_depth": 2,
    "include_file_context": true,
    "enable_reranking": true,
    "max_context_length": 16000
  }'
```

The RAG pipeline: **vector retrieval → reranking → call graph expansion → file context → assembly → length control**.

### Git History

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/history/commits?limit=N&author=X` | Commit history with filters |
| GET | `/api/v1/history/commit/:hash` | Commit detail with stats |
| GET | `/api/v1/history/file?path=X` | File change history |
| GET | `/api/v1/history/blame?path=X` | Line-by-line blame |
| GET | `/api/v1/history/entity?id=X` | Entity change history |

### Multi-Repo Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/repos` | List all indexed repositories |
| POST | `/api/v1/repos/active` | Switch active repository |
| GET | `/api/v1/repos/active` | Get current active repository |

### Other

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/api/v1/workspace` | Index statistics |
| GET | `/api/v1/entity/:id` | Entity detail |
| GET | `/mcp/sse` | MCP SSE stream |
| POST | `/mcp/request` | MCP protocol handler |

> Full API documentation with request/response schemas: [docs/MANUAL.md](docs/MANUAL.md)

---

## Configuration

Sourcelex uses YAML config with environment variable overrides.

**Config file locations** (in priority order):
1. `--config` flag
2. `./configs/config.yaml`
3. `./config.yaml`
4. `~/.sourcelex/config.yaml`

### Minimal

```yaml
vector_store:
  huggingface:
    api_token: "hf_xxxxx"
```

### Full

```yaml
paths:
  data_dir: "./data"
  temp_dir: "./temp"

git:
  clone_depth: 1
  credentials:
    # github.com: "ghp_xxxx"

vector_store:
  type: "chromem"                    # chromem (local) | qdrant (remote)
  embedder_type: "huggingface"
  chunk_size: 512
  chunk_overlap: 50
  huggingface:
    api_token: "hf_xxxxx"
    model_id: "sentence-transformers/all-MiniLM-L6-v2"
    dimension: 384
  qdrant:                            # only if type: "qdrant"
    host: "localhost"
    port: 6334
    collection_name: "code_vectors"

graph_store:
  type: "sqlite"                     # sqlite | memory

mcp:
  host: "0.0.0.0"
  port: 8000

agent:
  provider: ""                       # openai | anthropic | "" (disabled)
  openai:
    api_key: ""
    model: "gpt-4o"
    base_url: ""                     # custom endpoint for DeepSeek, Ollama, etc.
  anthropic:
    api_key: ""
    model: "claude-sonnet-4-20250514"

logging:
  level: "info"
  format: "text"
```

### Environment Variables

Override any config with `SOURCELEX_` prefix:

```bash
export SOURCELEX_VECTOR_STORE_HUGGINGFACE_API_TOKEN=hf_xxxxx
export SOURCELEX_AGENT_OPENAI_API_KEY=sk-xxxxx
export SOURCELEX_MCP_PORT=9000
```

---

## AI Agent

Enable the built-in AI assistant to answer questions about your codebase in natural language.

```yaml
# OpenAI
agent:
  provider: "openai"
  openai: { api_key: "sk-xxx", model: "gpt-4o" }

# Anthropic
agent:
  provider: "anthropic"
  anthropic: { api_key: "sk-ant-xxx", model: "claude-sonnet-4-20250514" }

# DeepSeek / Ollama / any OpenAI-compatible API
agent:
  provider: "openai"
  openai: { api_key: "sk-xxx", model: "deepseek-chat", base_url: "https://api.deepseek.com/v1" }
```

The agent automatically calls these tools to answer questions:

| Tool | Description |
|------|-------------|
| `semantic_search` | Find code by natural language |
| `get_entity` | Get function/class details |
| `get_callers` / `get_callees` | Trace call relationships |
| `get_subgraph` | Local call neighborhood |
| `find_path` | Call path between two functions |
| `detect_cycles` | Find circular dependencies |
| `get_workspace_stats` | Knowledge base statistics |

Web UI available at `http://localhost:8000` after starting the server.

---

## CLI Reference

```bash
sourcelex store   --path <dir>                    # Index local repository
sourcelex store   --repo <url> --branch <branch>  # Index remote repository
sourcelex store   --path . --force                 # Force full rebuild
sourcelex serve   --host 0.0.0.0 --port 8000      # Start MCP server
sourcelex version                                  # Show version info
```

---

## Project Structure

```
sourcelex/
├── cmd/sourcelex/              # Entry point
├── internal/
│   ├── agent/                 # AI chat agent
│   │   └── llm/               #   OpenAI / Anthropic providers
│   ├── analyzer/              # Code analysis engine
│   │   ├── parser/            #   Tree-sitter AST parsing
│   │   ├── entity/            #   Entity extraction (func/class/method)
│   │   ├── relation/          #   Call relationships + API endpoint detection
│   │   └── chunker/           #   Code chunking for embeddings
│   ├── cmd/                   # CLI commands (Cobra)
│   ├── config/                # Configuration (Viper)
│   ├── git/                   # Git operations (go-git)
│   ├── logger/                # Structured logging (Zap)
│   ├── mcp/                   # MCP server (Gin) — 25+ endpoints
│   ├── monitor/               # Resource monitoring
│   ├── repo/                  # Multi-repo registry + user session management
│   ├── store/                 # Storage facade
│   │   ├── vector/            #   chromem-go / Qdrant
│   │   └── graph/             #   SQLite / memory
│   └── web/                   # Web UI
├── configs/                   # Config templates
├── docs/                      # Documentation
└── go.mod
```

---

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25 |
| CLI | [Cobra](https://github.com/spf13/cobra) |
| Config | [Viper](https://github.com/spf13/viper) |
| Logging | [Zap](https://go.uber.org/zap) |
| HTTP | [Gin](https://github.com/gin-gonic/gin) |
| Git | [go-git](https://github.com/go-git/go-git) |
| AST Parsing | [go-tree-sitter](https://github.com/smacker/go-tree-sitter) |
| Vector Store | [chromem-go](https://github.com/philippgille/chromem-go) (default) / [Qdrant](https://qdrant.tech/) |
| Graph Store | SQLite (default) / In-memory |
| Embeddings | [HuggingFace Inference API](https://huggingface.co/inference-api) |

---

## Supported Languages

| Language | Extensions | Entity Types |
|----------|-----------|--------------|
| Python | `.py` | function, class, method |
| Go | `.go` | function, struct |
| Java | `.java` | class, method |
| JavaScript | `.js` | function |
| TypeScript | `.ts` | function, class |
| C | `.c` | function |
| C++ | `.cpp` `.cc` `.cxx` | class, method |

---

## GCC Installation

Tree-sitter requires CGO. Install a C compiler for your platform:

| Platform | Command |
|----------|---------|
| macOS | `xcode-select --install` |
| Ubuntu/Debian | `sudo apt install build-essential` |
| CentOS/RHEL | `sudo yum install gcc` |
| Windows | Install [MSYS2](https://www.msys2.org/), then `pacman -S mingw-w64-ucrt-x86_64-gcc` and add `C:\msys64\ucrt64\bin` to PATH |

---

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'feat: add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

## License

[MIT](LICENSE)

---

<div align="center">

**[Documentation](docs/MANUAL.md)** · **[Report Bug](https://github.com/Lion-Leporidae/sourcelex/issues)** · **[Request Feature](https://github.com/Lion-Leporidae/sourcelex/issues)**

</div>
