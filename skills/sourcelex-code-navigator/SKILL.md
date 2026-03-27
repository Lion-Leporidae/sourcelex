---
name: sourcelex-code-navigator
description: "This skill should be used when the user asks about code structure, function call chains, cross-file dependencies, or needs to navigate a codebase indexed by Sourcelex. Trigger phrases include 调用链, 谁调用了, 代码结构, 函数关系, call chain, who calls, code graph, find function, cross-file, 跨文件, 依赖关系, 代码图谱, or when the user references a Sourcelex-indexed repository. Also use when the user asks to search code semantically, explore entity relationships, or switch between indexed repositories."
---

# Sourcelex Code Navigator

Sourcelex is a code knowledge graph system that indexes repositories using Tree-sitter AST parsing,
extracts entities (functions, classes, methods) and their cross-file call relationships with
confidence scoring, and provides semantic search via vector embeddings.

## When to Use This Skill

- Answering questions about code structure, call chains, or cross-file dependencies
- Finding which functions call a specific function, or what a function calls
- Semantic code search ("find the function that handles authentication")
- Exploring entity details (signature, file location, callers/callees)
- Switching between indexed repositories in a multi-repo setup
- Getting a high-level overview of a codebase's call graph

## Available MCP Tools

The following tools are available through the `sourcelex` MCP server. To use them,
call the corresponding MCP tool.

### Core Navigation Tools

#### `semantic_search`
Search code entities by natural language description.

```json
{ "query": "authentication middleware", "top_k": 5 }
```

**Best for**: Finding relevant code when the exact name is unknown.

#### `get_entity`
Get detailed information about a specific code entity.

```json
{ "entity_id": "auth.Middleware" }
```

**Returns**: Type, file path, line numbers, signature.

#### `get_callchain`
Get a compact text representation of call relationships (95% fewer tokens than JSON).

```json
{ "entity_id": "store.SemanticSearch", "depth": 2 }
```

**Output format**:
```
SemanticSearch (store/knowledge.go:278)
  调用:
    → Embed (vector/hf.go:45)
    → Search (vector/chromem.go:23)
  被调用:
    ← handleSemanticSearch (mcp/handlers.go:190)
```

**Best for**: Understanding a function's position in the call graph. Prefer this over get_callers/get_callees for token efficiency.

#### `get_graph_summary`
Get the entire call graph as a compact text summary, grouped by file.

```json
{ "file": "" }
```

Pass `file` to filter by a specific file path, or leave empty for the full graph.

**Best for**: Getting a bird's-eye view of the codebase architecture.

### Relationship Tools

#### `get_callers`
Find all functions that call a specified function.

```json
{ "entity_id": "graph.NewSQLiteStore", "depth": 2 }
```

#### `get_callees`
Find all functions called by a specified function.

```json
{ "entity_id": "cmd.runServe", "depth": 2 }
```

### Repository Management Tools

#### `list_repos`
List all indexed repositories.

```json
{}
```

#### `set_active_repo`
Switch the active repository for all subsequent queries.

```json
{ "repo_key": "gin@main" }
```

The `repo_key` format is `repoID@branch`.

#### `get_active_repo`
Check which repository is currently active.

```json
{}
```

### File Access Tools

#### `grep_code`
Search for patterns in the repository source code.

```json
{ "pattern": "func.*Handler", "file_pattern": "*.go" }
```

#### `read_file_lines`
Read specific lines from a source file.

```json
{ "path": "internal/mcp/server.go", "start": 1, "end": 50 }
```

## Recommended Workflow

### When asked "What does function X do?"

1. Use `semantic_search` to find the entity if the exact qualified name is unknown
2. Use `get_entity` to get its signature, file location, and line numbers
3. Use `get_callchain` with depth=1 for a quick overview of callers and callees
4. Use `read_file_lines` to read the actual source code if needed

### When asked "Show me the call chain / 调用链"

1. Use `get_callchain` with depth=2 for a tree view
2. If the user wants the full picture, use `get_graph_summary`

### When asked "Who calls function X?" / "谁调用了"

1. Use `get_callers` with the entity's qualified name

### When asked about cross-file dependencies

1. Use `get_callchain` — cross-file calls are automatically included
2. The confidence score indicates resolution quality (1.0 = same file, 0.8 = import-resolved, 0.3 = guessed)

### When working with multiple repositories

1. Use `list_repos` to show available repos
2. Use `set_active_repo` to switch — all subsequent tool calls will query the selected repo
3. Use `get_active_repo` to confirm the current context

## Entity Naming Convention

Entities use **qualified names** in the format:
- Go: `package.FunctionName` (e.g., `store.SemanticSearch`, `graph.SQLiteStore.AddNode`)
- Python: `ClassName.method_name` or `function_name`
- Java: `ClassName.methodName`
- JavaScript: `functionName` or `ClassName`

When the user provides a short name, use `semantic_search` first to find the full qualified name.

## Confidence Scoring

Call relationship edges have confidence scores (0-1):
- **≥ 0.8**: High confidence (exact match or import-resolved)
- **0.5-0.7**: Medium confidence (type inference or same-directory)
- **≤ 0.3**: Low confidence (guessed or unresolved, likely stdlib/external)

When presenting results, mention confidence for cross-file calls to set appropriate expectations.
