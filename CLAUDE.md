# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is RepoMind

RepoMind is a code knowledge base system written in Go. It analyzes code repositories using Tree-sitter AST parsing, stores entities and call relationships in vector + graph databases, and exposes them via an MCP (Model Context Protocol) HTTP/SSE server for AI assistant integration (Cursor, Claude, etc.).

## Build & Run

```bash
# Build (requires GCC for Tree-sitter CGO)
go build -o repomind ./cmd/repomind

# Index a local repository
./repomind store --path /path/to/repo
./repomind store --repo https://github.com/user/repo --branch main
./repomind store --path . --force   # force rebuild, deletes existing data

# Start MCP server
./repomind serve --port 8000

# Run all tests
go test ./...

# Run a single package's tests
go test ./internal/analyzer/...

# Run a specific test
go test ./internal/analyzer/... -run TestFunctionName

# Test with verbose output
go test -v ./internal/...

# Test with race detector
go test -race ./...
```

## Configuration

Config is loaded via Viper from `./configs/config.yaml`, `./config.yaml`, or `~/.repomind/config.yaml`. Environment variables with `REPOMIND_` prefix override config (e.g., `REPOMIND_VECTOR_STORE_HUGGINGFACE_API_TOKEN`). See `config.example.yaml` for all options.

Required: HuggingFace API token for embedding (`vector_store.huggingface.api_token`).

Optional: Agent configuration for AI-powered code Q&A (`agent.provider`: `openai` or `anthropic`, with respective API keys). See `config.example.yaml` for all options including Qdrant remote vector store and Git credential settings.

## Architecture

Three-layer design: **CLI commands** → **Business logic** (analyzer + store) → **Storage** (vector + graph).

### Data Pipeline (store command)

```
Git repo → FileScanner (incremental via hash+mtime)
         → Tree-sitter AST parse
         → Entity extraction (functions, classes, methods)
         → Call relation extraction (symbol table based)
         → SymbolChunker (full code blocks for embedding)
         → HuggingFace embedding
         → chromem-go vector store + SQLite graph store
```

### Query Pipeline (serve command)

The MCP server (Gin-based, `internal/mcp/`) exposes 23 endpoints across 8 categories:

- **Search**: `POST /api/v1/search/semantic`, `/search/hybrid` (vector + keyword reranking), `/search/context` (context-aware)
- **Entity**: `GET /api/v1/entity/:id` — entity info from graph
- **Call Graph**: `GET /api/v1/callmap/:id`, `/callers/:id?depth=N`, `/callees/:id?depth=N` — full JSON call graph traversal
- **Compact Call Graph** (token-optimized for AI): `GET /api/v1/callchain/:id?depth=N` (95% smaller than JSON), `/graph/summary?file=PATTERN` (adjacency list format)
- **Function Graph**: `GET /api/v1/graph/function`, `/graph/subgraph/:id?depth=2`, `/graph/path?from=X&to=Y`, `/graph/cycles`, `/graph/topo-sort`
- **History** (Git integration): `GET /api/v1/history/commits`, `/history/commit/:hash`, `/history/file?path=`, `/history/blame?path=`, `/history/entity?id=`
- **RAG**: `POST /api/v1/rag/context` — retrieval + graph context + file context assembly with reranking
- **MCP Protocol**: `GET /mcp/sse` (SSE streaming), `POST /mcp/request`
- **Utility**: `GET /health`, `GET /api/v1/workspace` (store stats)

### Key Abstractions

- **`vector.Store` interface** (`internal/store/vector/store.go`) — implementations: `ChromemStore` (local, default), `QdrantStore`
- **`graph.Store` interface** (`internal/store/graph/store.go`) — implementations: `SQLiteStore` (default), `MemoryStore`
- **`vector.Embedder` interface** — implementation: `HuggingFaceEmbedder`
- **`KnowledgeStore`** (`internal/store/knowledge.go`) — facade that composes vector + graph stores
- **RAG pipeline** (`internal/store/rag.go`) — retrieval-augmented generation: vector retrieval → graph context (caller/callee) → file context → reranking → context length limiting (default 16000 chars)
- **`CodeAgent`** (`internal/agent/`) — AI-powered code Q&A with tool calling loop, supports OpenAI/Anthropic providers (`internal/agent/llm/`)
- **`ResourceMonitor`** (`internal/monitor/`) — system/process/Go runtime resource sampling during indexing
- **Graph algorithms** (`internal/store/graph/algorithms.go`) — cycle detection, topological sort, BFS path finding, subgraph extraction

### Analyzer Two-Phase Design

The analyzer in `internal/analyzer/analyzer.go` runs two phases with worker pools:
1. **Phase 1**: Parse files with Tree-sitter, extract entities (functions/classes/methods) per language
2. **Phase 2**: Build symbol table from Phase 1 entities, then re-parse files to extract call relationships

Both phases use the same worker pool pattern with `sync.WaitGroup` + channel-based job distribution.

### Entity Identification

Entities use `QualifiedName` as their universal ID across vector store, graph store, and API responses. Format: `ClassName.MethodName` for methods, `FunctionName` for top-level functions.

### Supported Languages

Tree-sitter grammars for: Python, Go, Java, JavaScript/TypeScript, C, C++. Language detection is by file extension in `internal/analyzer/scanner.go` (`GetLanguage` function).

## Module Dependencies

```
cmd/repomind/main.go → internal/cmd/ (Cobra commands)
  cmd/store.go → analyzer, git, store, monitor
  cmd/serve.go → store, mcp, git (for history queries)
  cmd/root.go  → config, logger

analyzer → parser (Tree-sitter), entity, relation, chunker
store/knowledge.go → store/vector, store/graph, chunker
store/rag.go → store/vector, store/graph (RAG pipeline)
mcp/server.go → store (KnowledgeStore), git (Repository), logger
agent → agent/llm (OpenAI/Anthropic), store (KnowledgeStore)
```

## Code Conventions

- Log messages and comments are in Chinese
- All internal packages live under `internal/` (not importable externally)
- Interfaces are defined in `store.go` files alongside their package; implementations are in separate files (e.g., `chromem.go`, `sqlite.go`)
- Config structs use `mapstructure` tags for Viper binding
- The project uses `context.Context` throughout for cancellation propagation

---

## Development Rules

### Rule 1: No Fabricated Documentation

- **NEVER** generate fake API documentation, architecture diagrams, or design docs based on assumptions
- **NEVER** create placeholder README, CHANGELOG, or doc files unless explicitly asked
- All documentation content must be derived from actual code reading, not speculation
- If documenting an API endpoint, read the actual handler code in `internal/mcp/handlers.go` first
- If describing a data structure, read the actual struct definition first

### Rule 2: Real Repo Verification Required

- All new features, bug fixes, and refactors **MUST** be verified against a real repository
- Use `https://github.com/Lion-Leporidae/repomind-go.git` as the primary test repository
- Verification workflow:
  1. Build: `go build -o repomind ./cmd/repomind`
  2. Index the test repo: `./repomind store --repo https://github.com/Lion-Leporidae/repomind-go.git --branch main --force`
  3. Start server: `./repomind serve --port 8000` (background)
  4. Test endpoints with curl:
     - `curl http://localhost:8000/health`
     - `curl -X POST http://localhost:8000/api/v1/search/semantic -H "Content-Type: application/json" -d '{"query": "代码分析", "top_k": 5}'`
     - `curl http://localhost:8000/api/v1/workspace`
  5. Verify results are non-empty and structurally correct
- **NEVER** claim a feature works without running it against the test repo
- For analyzer changes, verify entity count and relation count are reasonable (this Go repo should produce 30+ entities)

### Rule 3: Code Quality Gates

- Run `go build ./...` before claiming any code change is complete
- Run `go vet ./...` to check for common issues
- Run `go test ./...` if tests exist for the modified package
- Run `go test -race ./...` for any code involving goroutines or shared state (analyzer worker pools, SSE handlers)
- CGO is required (Tree-sitter): ensure `CGO_ENABLED=1` in build environment

### Rule 4: Interface-First Design

When adding new storage backends, embedders, or analyzers:
- Define or extend the interface in the package's `store.go` / main interface file first
- Implement in a separate file named after the backend (e.g., `elasticsearch.go`, `openai.go`)
- Follow existing patterns: `vector.Store`, `graph.Store`, `vector.Embedder`
- The `KnowledgeStore` facade must remain the single entry point for all storage operations

### Rule 5: Respect Incremental Update Design

- The `FileScanner` uses hash+mtime for change detection — changes to scanner logic must preserve this invariant
- Do NOT reprocess files that haven't changed unless `--force` flag is set
- New entity types or relation types must be additive — don't break existing stored data formats

### Rule 6: Chinese-First Logging, English-First Code

- Log messages (via `logger.Logger`): Chinese (e.g., `log.Info("索引构建完成")`)
- Code comments: Chinese for inline explanations, English for godoc-style package/function docs
- Error messages returned in API responses: Chinese
- Variable names, function names, struct fields: English

### Rule 7: Worker Pool Safety

The analyzer uses concurrent worker pools in `analyzer.go`:
- Always use `sync.Mutex` when appending to shared slices
- Always check `ctx.Done()` in worker loops for cancellation
- Channel-based job distribution: create buffered channel, fill it, close it, then spawn workers
- Default worker count is 4 — changes must be configurable via `SetWorkers()`

---

## Skill Usage Rules

The following skills are available and should be used at specific points in the development workflow:

### Planning Phase

- **Use `/brainstorming`** before creating any new feature, component, or behavior change. This is mandatory — do not skip to implementation.
- **Use `/writing-plans`** when you have clear requirements for a multi-step task. Write the plan before touching code.
- For tasks with 2+ independent subtasks, **use `/dispatching-parallel-agents`** to work them in parallel.

### Implementation Phase

- **Use `/test-driven-development`** when implementing any feature or bugfix. Write tests first, then implementation.
  - For RepoMind specifically: write test against real parsing output, not mocked data, when testing analyzer components
  - Test files go next to the code they test (e.g., `analyzer_test.go` alongside `analyzer.go`)
- **Use `/executing-plans`** when you have a written plan to execute. Follow the plan with review checkpoints.
- **Use `/subagent-driven-development`** when executing plans with independent tasks that can be parallelized.
- **Use `/using-git-worktrees`** when starting feature work that needs isolation from the current workspace.

### Debugging

- **Use `/systematic-debugging`** when encountering any bug, test failure, or unexpected behavior. Do NOT guess fixes — follow the systematic debugging process.
  - Common RepoMind issues to watch for:
    - Tree-sitter CGO build failures → check GCC and `CGO_ENABLED=1`
    - Empty entity extraction → check `GetLanguage()` returns correct language string
    - HuggingFace API errors → check token and model ID in config
    - SQLite lock errors → check for unclosed `graphStore.Close()` calls

### Verification Phase

- **Use `/verification-before-completion`** before claiming any work is done. Run actual commands and confirm output. This is non-negotiable.
  - Minimum verification for RepoMind changes:
    ```bash
    go build -o repomind ./cmd/repomind
    go vet ./...
    go test ./...
    # For analyzer/store changes, also run:
    ./repomind store --repo https://github.com/Lion-Leporidae/repomind-go.git --branch main --force
    ```
- **Use `/requesting-code-review`** after completing major features or before merging.
- **Use `/receiving-code-review`** when processing review feedback — verify suggestions technically before blindly implementing them.

### Completion Phase

- **Use `/finishing-a-development-branch`** when implementation is complete and tests pass, to decide on merge/PR/cleanup.
- **Use `/commit`** to create commits. Follow conventional commit format:
  - `feat:` new feature
  - `fix:` bug fix
  - `refactor:` code restructuring
  - `test:` adding tests
  - `docs:` documentation (only when explicitly requested)

### Skill Enforcement

- **NEVER** skip `/brainstorming` for new features — jumping to code without design leads to architectural inconsistency
- **NEVER** skip `/verification-before-completion` — evidence before assertions, always
- **NEVER** skip `/systematic-debugging` when a test fails — no guessing at fixes
- If unsure which skill to use, **use `/using-superpowers`** to find the right one

---

## Testing Strategy

### Test Repo

Primary: `https://github.com/Lion-Leporidae/repomind-go.git` (this repo itself)

This repo is a good test target because:
- It contains Go code (tests `extractGo` path in entity extractor)
- It has functions, methods, structs/types (tests all entity types)
- It has cross-package call relationships (tests relation extraction)
- It's a real-world codebase, not synthetic test data

### Integration Test Pattern

For end-to-end verification of the store → analyze → query pipeline:

```bash
# 1. Clean build
go build -o repomind ./cmd/repomind

# 2. Index with force rebuild
./repomind store --repo https://github.com/Lion-Leporidae/repomind-go.git --branch main --force

# 3. Start server in background
./repomind serve --port 8000 &
SERVER_PID=$!
sleep 2

# 4. Verify endpoints
curl -s http://localhost:8000/health | grep '"status":"ok"'
curl -s http://localhost:8000/api/v1/workspace | grep '"success":true'
curl -s -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "代码分析", "top_k": 5}' | grep '"success":true'

# 5. Cleanup
kill $SERVER_PID
```

### Unit Test Expectations

When writing unit tests for this project:
- `internal/analyzer/parser/` — test that Go/Python/Java/TS files produce non-nil AST trees
- `internal/analyzer/entity/` — test that known code snippets extract expected entities with correct QualifiedName
- `internal/analyzer/relation/` — test that call expressions are resolved against symbol table
- `internal/store/graph/` — test SQLite and memory store implementations satisfy the `graph.Store` interface contract
- `internal/store/vector/` — test chromem store CRUD operations
- `internal/mcp/` — test HTTP handlers return correct status codes and response shapes
