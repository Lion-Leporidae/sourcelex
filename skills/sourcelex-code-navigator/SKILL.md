---
name: sourcelex-code-navigator
description: "This skill should be used when the user asks about code structure, function call chains, cross-file dependencies, or needs to navigate a codebase indexed by Sourcelex. Trigger phrases include 调用链, 谁调用了, 代码结构, 函数关系, call chain, who calls, code graph, find function, cross-file, 跨文件, 依赖关系, 代码图谱, or when the user references a Sourcelex-indexed repository. Also use when the user asks to search code semantically, explore entity relationships, or switch between indexed repositories."
---

# Sourcelex Code Navigator

Sourcelex is a code knowledge graph system. It indexes repositories with Tree-sitter, extracts
entities and cross-file call relationships, and provides semantic search via embeddings.

## 4 Tools — Simple and Powerful

### `search`
Find code entities by natural language or exact qualified name.

```json
{ "query": "authentication middleware" }
{ "query": "store.SemanticSearch" }
```

If query contains `.`, it first tries exact entity lookup (returns details + call chain).
Otherwise falls back to semantic search. Always start here.

### `get_callchain`
Get call relationships for an entity, or the entire call graph for a file/repo.

```json
{ "entity_id": "store.SemanticSearch", "depth": 2 }
{ "file": "internal/mcp/handlers.go" }
{ }
```

- With `entity_id`: returns callers and callees in compact text
- With `file`: returns file-level call graph summary
- With neither: returns full repo call graph

### `read_code`
Read source code or search code with grep.

```json
{ "path": "internal/mcp/server.go", "start": 1, "end": 50 }
{ "grep": "func.*Handle", "file_pattern": "*.go" }
```

- With `path`: reads file lines (add `start`/`end` to narrow)
- With `grep`: regex search across the repo

### `manage_repo`
Manage indexed repositories.

```json
{ "action": "list" }
{ "action": "switch", "repo_key": "gin@main" }
{ "action": "status" }
```

## Workflow

1. User asks about code → `search` with natural language
2. Need call chain → `get_callchain` with the entity_id from search results
3. Need source code → `read_code` with the file_path and line numbers
4. Switch repo → `manage_repo` with action "switch"

Most questions can be answered in 1-2 tool calls.
