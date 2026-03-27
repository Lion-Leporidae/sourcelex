#!/bin/bash
# Sourcelex 一键安装脚本
# 支持：CodeBuddy / Cursor / Windsurf / Claude Code CLI / Codex CLI
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
SKILL_NAME="sourcelex-code-navigator"
DEFAULT_PORT=9000
PORT="${SOURCELEX_PORT:-$DEFAULT_PORT}"

echo "🚀 Sourcelex 安装脚本"
echo "===================="
echo ""

# ==================== 1. 编译 ====================
echo "📦 编译 sourcelex..."
cd "$REPO_DIR"
if command -v go &>/dev/null; then
  go build -o sourcelex ./cmd/sourcelex/
  echo "   ✅ 编译完成: $REPO_DIR/sourcelex"
else
  echo "   ⚠️  未安装 Go，跳过编译"
fi

# ==================== 2. CodeBuddy / Cursor / Windsurf Skill + MCP ====================
echo ""
echo "🧠 配置 AI IDE..."

configure_json_mcp() {
  local MCP_FILE="$1"
  local IDE_NAME="$2"

  if [ ! -f "$MCP_FILE" ]; then
    echo '{"mcpServers":{}}' > "$MCP_FILE"
  fi

  if grep -q '"sourcelex"' "$MCP_FILE" 2>/dev/null; then
    echo "   ⏩ $IDE_NAME MCP 已配置，跳过"
    return
  fi

  python3 -c "
import json
with open('$MCP_FILE', 'r') as f:
    data = json.load(f)
if 'mcpServers' not in data:
    data['mcpServers'] = {}
data['mcpServers']['sourcelex'] = {
    'type': 'sse',
    'url': 'http://localhost:$PORT/mcp/sse'
}
with open('$MCP_FILE', 'w') as f:
    json.dump(data, f, indent=2)
" 2>/dev/null && echo "   ✅ $IDE_NAME MCP 已配置" || echo "   ⚠️  $IDE_NAME MCP 配置失败"
}

for IDE_DIR in "$HOME/.codebuddy" "$HOME/.cursor" "$HOME/.windsurf"; do
  IDE_NAME=$(basename "$IDE_DIR")
  if [ -d "$IDE_DIR" ]; then
    # Skill
    mkdir -p "$IDE_DIR/skills"
    rm -rf "$IDE_DIR/skills/$SKILL_NAME"
    cp -r "$REPO_DIR/skills/$SKILL_NAME" "$IDE_DIR/skills/$SKILL_NAME"
    echo "   ✅ $IDE_NAME Skill 已安装"
    # MCP
    configure_json_mcp "$IDE_DIR/mcp.json" "$IDE_NAME"
  fi
done

# Claude Desktop
CLAUDE_DESKTOP_DIR="$HOME/Library/Application Support/Claude"
if [ -d "$CLAUDE_DESKTOP_DIR" ]; then
  configure_json_mcp "$CLAUDE_DESKTOP_DIR/claude_desktop_config.json" "Claude Desktop"
fi

# ==================== 3. Claude Code CLI ====================
echo ""
echo "🔧 配置 Claude Code CLI..."

if command -v claude &>/dev/null; then
  # 添加 MCP 服务器
  if claude mcp list 2>/dev/null | grep -q "sourcelex"; then
    echo "   ⏩ Claude Code MCP 已配置，跳过"
  else
    claude mcp add --transport http sourcelex "http://localhost:$PORT/mcp/sse" --scope user 2>/dev/null \
      && echo "   ✅ Claude Code MCP 已配置" \
      || echo "   ⚠️  Claude Code MCP 配置失败（可手动运行: claude mcp add --transport http sourcelex http://localhost:$PORT/mcp/sse）"
  fi

  # 写入项目级 CLAUDE.md（如果不存在或未包含 sourcelex 说明）
  CLAUDE_MD="$REPO_DIR/CLAUDE.md"
  if [ ! -f "$CLAUDE_MD" ] || ! grep -q "sourcelex" "$CLAUDE_MD" 2>/dev/null; then
    # 追加（不覆盖已有内容）
    cat >> "$CLAUDE_MD" << 'CLAUDE_EOF'

## Sourcelex 代码导航工具

本项目已配置 Sourcelex MCP 服务，提供以下代码分析能力：

### 可用工具
- `semantic_search` — 语义搜索代码实体（函数、类、方法）
- `get_callchain` — 获取紧凑调用链（推荐，节省 95% token）
- `get_graph_summary` — 获取完整调用图摘要
- `get_callers` / `get_callees` — 查询调用者/被调用者
- `get_entity` — 获取实体详情（签名、文件、行号）
- `list_repos` / `set_active_repo` — 多仓库管理
- `grep_code` / `read_file_lines` — 代码搜索和文件读取

### 使用建议
- 查函数调用链时优先用 `get_callchain`（比 JSON 节省 95% token）
- 找代码时先 `semantic_search`，再 `get_entity` 查详情
- 实体名使用 QualifiedName 格式：`package.FunctionName`
CLAUDE_EOF
    echo "   ✅ CLAUDE.md 已更新"
  else
    echo "   ⏩ CLAUDE.md 已包含 sourcelex 说明"
  fi
else
  echo "   ⏩ 未安装 Claude Code CLI，跳过"
fi

# ==================== 4. Codex CLI ====================
echo ""
echo "🤖 配置 Codex CLI..."

if command -v codex &>/dev/null || [ -d "$HOME/.codex" ]; then
  CODEX_CONFIG="$HOME/.codex/config.toml"
  mkdir -p "$HOME/.codex"

  # MCP 配置
  if [ -f "$CODEX_CONFIG" ] && grep -q "sourcelex" "$CODEX_CONFIG" 2>/dev/null; then
    echo "   ⏩ Codex MCP 已配置，跳过"
  else
    cat >> "$CODEX_CONFIG" << CODEX_TOML

# Sourcelex 代码知识图谱 MCP 服务
[mcp_servers.sourcelex]
url = "http://localhost:$PORT/mcp/sse"
startup_timeout_sec = 15
tool_timeout_sec = 30
CODEX_TOML
    echo "   ✅ Codex MCP 已配置 ($CODEX_CONFIG)"
  fi

  # AGENTS.md
  AGENTS_MD="$REPO_DIR/AGENTS.md"
  if [ ! -f "$AGENTS_MD" ] || ! grep -q "sourcelex" "$AGENTS_MD" 2>/dev/null; then
    cat >> "$AGENTS_MD" << 'AGENTS_EOF'

## Sourcelex 代码导航

本项目集成了 Sourcelex 代码知识图谱。当需要分析代码结构时，使用以下 MCP 工具：

### 工作流
1. **查找函数**: `semantic_search` → 自然语言搜索代码实体
2. **查看调用链**: `get_callchain` → 紧凑文本格式（推荐）
3. **调用者/被调用者**: `get_callers` / `get_callees`
4. **读源码**: `read_file_lines` → 按文件路径和行号读取
5. **切换仓库**: `list_repos` → `set_active_repo`

### 注意
- 实体名使用限定名格式: `package.FunctionName`（如 `store.SemanticSearch`）
- `get_callchain` 比 `get_callers` + `get_callees` 节省 95% token
- 置信度 ≥0.8 表示高置信调用关系
AGENTS_EOF
    echo "   ✅ AGENTS.md 已创建"
  else
    echo "   ⏩ AGENTS.md 已包含 sourcelex 说明"
  fi
else
  echo "   ⏩ 未安装 Codex CLI，跳过"
fi

# ==================== 5. 配置文件 ====================
echo ""
if [ ! -f "$REPO_DIR/config.yaml" ]; then
  if [ -f "$REPO_DIR/config.example.yaml" ]; then
    cp "$REPO_DIR/config.example.yaml" "$REPO_DIR/config.yaml"
    echo "📋 已创建 config.yaml（请编辑填入 HuggingFace API Token）"
  fi
else
  echo "📋 config.yaml 已存在"
fi

# ==================== 完成 ====================
echo ""
echo "============================================"
echo "✅ 安装完成！"
echo ""
echo "已配置的平台："
command -v claude &>/dev/null && echo "  • Claude Code CLI ✓"
[ -d "$HOME/.codebuddy" ] && echo "  • CodeBuddy ✓"
[ -d "$HOME/.cursor" ] && echo "  • Cursor ✓"
[ -d "$HOME/.windsurf" ] && echo "  • Windsurf ✓"
(command -v codex &>/dev/null || [ -d "$HOME/.codex" ]) && echo "  • Codex CLI ✓"
echo ""
echo "下一步："
echo "  1. 编辑 config.yaml 填入 HuggingFace API Token"
echo "  2. 索引仓库:  ./sourcelex store --repo <URL>"
echo "  3. 启动服务:  ./sourcelex serve --port $PORT"
echo "  4. 重启 IDE / 终端，开始使用！"
echo ""
echo "试试对 AI 说："
echo '  "帮我看看 main 函数的调用链"'
echo '  "搜索认证相关的函数"'
echo "============================================"
