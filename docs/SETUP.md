# Sourcelex 安装与配置指南

## 快速开始

### 1. 安装 Sourcelex

```bash
git clone https://github.com/Lion-Leporidae/sourcelex.git
cd sourcelex
go build -o sourcelex ./cmd/sourcelex/
```

### 2. 配置

复制配置文件并填入 API Key：

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，至少需要配置嵌入模型的 API Token：

```yaml
vector_store:
  huggingface:
    api_token: "hf_your_token_here"  # HuggingFace API Token
```

### 3. 索引仓库

```bash
# 远程仓库
./sourcelex store --repo https://github.com/user/repo

# 本地仓库
./sourcelex store --path /path/to/local/repo
```

### 4. 启动服务

```bash
./sourcelex serve --port 9000
```

---

## 在 AI IDE 中使用

Sourcelex 提供两种使用方式：**MCP 工具** 和 **Skill**。两者配合使用效果最佳。

### 方式一：MCP 连接（提供工具能力）

MCP 让 AI 能够**调用** sourcelex 的搜索、调用链分析等工具。

#### CodeBuddy / Cursor / Windsurf 配置

编辑 MCP 配置文件：

| IDE | 配置文件路径 |
|-----|-------------|
| CodeBuddy | `~/.codebuddy/mcp.json` |
| Cursor | `~/.cursor/mcp.json` |
| Windsurf | `~/.windsurf/mcp.json` |
| Claude Desktop | `~/Library/Application Support/Claude/claude_desktop_config.json` |

添加 sourcelex MCP 服务：

```json
{
  "mcpServers": {
    "sourcelex": {
      "type": "sse",
      "url": "http://localhost:9000/mcp/sse"
    }
  }
}
```

> **注意**：`type: "sse"` 是必须的。如果服务部署在远程服务器，将 `localhost:9000` 替换为实际地址。

配置完成后重启 IDE，AI 就能使用以下工具：
- `semantic_search` — 语义搜索代码实体
- `get_callchain` — 获取调用链
- `get_callers` / `get_callees` — 调用者/被调用者
- `list_repos` / `set_active_repo` — 多仓库管理
- 等 11 个工具

### 方式二：Skill 安装（提供使用智慧）

Skill 告诉 AI **什么时候该用 sourcelex** 以及 **怎么组合使用这些工具**。没有 Skill，AI 可能不知道该在何时调用 sourcelex 的工具。

#### 安装方式 A：复制到用户目录（推荐）

```bash
# 从 sourcelex 仓库复制 skill 到用户 skill 目录
cp -r skills/sourcelex-code-navigator ~/.codebuddy/skills/
```

> 安装后对**所有项目**生效。

#### 安装方式 B：放到项目目录（团队共享）

```bash
# 复制到当前项目的 .codebuddy 目录
cp -r skills/sourcelex-code-navigator .codebuddy/skills/
```

> 仅对该项目生效，适合团队统一配置。

#### 安装方式 C：通过 zip 包安装

如果通过 Marketplace 或其他渠道获得 `sourcelex-code-navigator.zip`：

```bash
# 解压到用户 skill 目录
unzip sourcelex-code-navigator.zip -d ~/.codebuddy/skills/
```

#### 验证安装

重启 IDE 后，在新对话中输入：

```
帮我看看 SemanticSearch 的调用链
```

如果 AI 自动调用了 sourcelex 的 `get_callchain` 工具，说明 Skill + MCP 都已生效。

---

## MCP 和 Skill 的关系

```
┌─────────────────────────────────────────────┐
│  Skill (SKILL.md)                           │
│  "教 AI 什么时候用、怎么用"                    │
│  - 触发词：调用链、代码结构、谁调用了...        │
│  - 推荐工作流：先搜索 → 再查调用链 → 再读源码   │
│  - 结果解读：置信度含义、命名规范...            │
├─────────────────────────────────────────────┤
│  MCP (mcp.json)                             │
│  "提供实际的工具能力"                          │
│  - semantic_search, get_callchain, ...       │
│  - 连接到 sourcelex 服务端                    │
├─────────────────────────────────────────────┤
│  Sourcelex 服务端                            │
│  "后端引擎"                                   │
│  - Tree-sitter 解析 + 向量嵌入 + 图数据库     │
│  - HTTP API + MCP SSE 协议                   │
└─────────────────────────────────────────────┘
```

- **只配 MCP 不装 Skill**：AI 能用工具，但不知道何时主动用
- **只装 Skill 不配 MCP**：AI 知道该用，但没有实际工具可调用
- **两者都配**：最佳体验，AI 在合适的时机自动使用正确的工具

---

## 多仓库 & 认证（高级）

### 索引多个仓库

```bash
./sourcelex store --repo https://github.com/gin-gonic/gin
./sourcelex store --repo https://github.com/facebook/react
./sourcelex list  # 查看已索引仓库
```

### 在 AI 对话中切换仓库

安装 Skill 后，直接对 AI 说：

```
切换到 gin 仓库
```

AI 会自动调用 `list_repos` → `set_active_repo`。

### GitHub OAuth 认证（服务器部署）

```yaml
# config.yaml
auth:
  enabled: true
  github_client_id: "your_client_id"
  github_client_secret: "your_client_secret"
  jwt_secret: "random-strong-secret"
```

启用后不同用户的活跃仓库相互隔离。
