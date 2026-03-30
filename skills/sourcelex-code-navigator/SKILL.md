---
name: sourcelex-code-navigator
description: "This skill should be used when the user asks about code structure, function call chains, cross-file dependencies, or needs to navigate a codebase indexed by Sourcelex. Trigger phrases include 调用链, 谁调用了, 代码结构, 函数关系, call chain, who calls, code graph, find function, cross-file, 跨文件, 依赖关系, 代码图谱, or when the user references a Sourcelex-indexed repository. Also use when the user asks to search code semantically, explore entity relationships, or switch between indexed repositories."
---

# Sourcelex Code Navigator

Sourcelex indexes codebases with Tree-sitter and provides 5 MCP tools for code navigation.

## Tools

### `search` — Find code entities
```json
{ "query": "authentication middleware" }
{ "query": "store.SemanticSearch" }
```
Input natural language or a qualified name. Returns entity details, file location, signature.

### `callgraph` — View call relationships
```json
{ "query": "store.SemanticSearch" }
{ "query": "internal/mcp/handlers.go" }
{ "query": "" }
```
Input a function name for its call chain, a file path for file-level call graph, or empty for the full repo graph.

### `read_code` — Read source files
```json
{ "path": "internal/mcp/server.go:10-50" }
{ "path": "grep:func.*Handle" }
```
Input a file path (with optional :line-line range) or grep:pattern to search code.

### `history` — Git history
```json
{ "query": "internal/mcp/server.go" }
{ "query": "abc1234" }
{ "query": "blame:internal/mcp/server.go" }
{ "query": "fix auth" }
```
Input a file path for file history, a commit hash for details, blame:path for line attribution, or keywords to search commits.

### `switch_repo` — Switch repository
```json
{ "repo": "gin@main" }
{ }
```
Pass a repo key to switch, or omit to list all repos and current status.

## Usage
- Find code: `search` → get file:line → `read_code` to view source
- Call chains: `callgraph` with the entity name from search results
- Git history: `history` with file path or keywords
- Multiple repos: `switch_repo` to change context
