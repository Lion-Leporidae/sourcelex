---
name: sourcelex-code-navigator
description: "This skill should be used when the user asks about code structure, function call chains, cross-file dependencies, or needs to navigate a codebase indexed by Sourcelex. Trigger phrases include 调用链, 谁调用了, 代码结构, 函数关系, call chain, who calls, code graph, find function, cross-file, 跨文件, 依赖关系, 代码图谱, or when the user references a Sourcelex-indexed repository. Also use when the user asks to search code semantically, explore entity relationships, or switch between indexed repositories."
---

# Sourcelex Code Navigator

Sourcelex indexes codebases with Tree-sitter and provides 3 simple MCP tools.

## Tools

### `search` — Find code
One parameter: `query`. Works with natural language or exact function names.

```json
{ "query": "authentication middleware" }
{ "query": "store.SemanticSearch" }
```

Automatically tries exact match first, then semantic search. Results include entity details and call relationships.

### `read_code` — Read source
One parameter: `path`. Supports `file:line-line` format.

```json
{ "path": "internal/mcp/server.go" }
{ "path": "internal/mcp/server.go:10-50" }
```

### `switch_repo` — Switch repository
Optional parameter: `repo`. Without it, lists all available repos.

```json
{ "repo": "gin@main" }
{ }
```

## Usage
- Most questions: one `search` call is enough
- Need source code: `search` gives you file:line, then `read_code` to read it
- Multiple repos: `switch_repo` to change context
