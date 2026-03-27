#!/bin/bash
# Sourcelex 卸载脚本
# 支持：CodeBuddy / Cursor / Windsurf / Claude Code CLI / Codex CLI
set -e

SKILL_NAME="sourcelex-code-navigator"

echo "🗑️  Sourcelex 卸载"
echo "=================="
echo ""

# CodeBuddy / Cursor / Windsurf — Skill + MCP
for IDE_DIR in "$HOME/.codebuddy" "$HOME/.cursor" "$HOME/.windsurf"; do
  IDE_NAME=$(basename "$IDE_DIR")
  SKILL_DIR="$IDE_DIR/skills/$SKILL_NAME"
  if [ -d "$SKILL_DIR" ]; then
    rm -rf "$SKILL_DIR"
    echo "✅ 已移除 $IDE_NAME Skill"
  fi

  MCP_FILE="$IDE_DIR/mcp.json"
  if [ -f "$MCP_FILE" ] && grep -q '"sourcelex"' "$MCP_FILE" 2>/dev/null; then
    python3 -c "
import json
with open('$MCP_FILE', 'r') as f:
    data = json.load(f)
if 'mcpServers' in data and 'sourcelex' in data['mcpServers']:
    del data['mcpServers']['sourcelex']
with open('$MCP_FILE', 'w') as f:
    json.dump(data, f, indent=2)
" 2>/dev/null && echo "✅ 已从 $IDE_NAME 移除 MCP 配置"
  fi
done

# Claude Desktop
CLAUDE_CFG="$HOME/Library/Application Support/Claude/claude_desktop_config.json"
if [ -f "$CLAUDE_CFG" ] && grep -q '"sourcelex"' "$CLAUDE_CFG" 2>/dev/null; then
  python3 -c "
import json
with open('$CLAUDE_CFG', 'r') as f:
    data = json.load(f)
if 'mcpServers' in data and 'sourcelex' in data['mcpServers']:
    del data['mcpServers']['sourcelex']
with open('$CLAUDE_CFG', 'w') as f:
    json.dump(data, f, indent=2)
" 2>/dev/null && echo "✅ 已从 Claude Desktop 移除 MCP 配置"
fi

# Claude Code CLI
if command -v claude &>/dev/null; then
  claude mcp remove sourcelex --scope user 2>/dev/null && echo "✅ 已从 Claude Code CLI 移除 MCP" || true
fi

# Codex CLI
CODEX_CONFIG="$HOME/.codex/config.toml"
if [ -f "$CODEX_CONFIG" ] && grep -q "sourcelex" "$CODEX_CONFIG" 2>/dev/null; then
  # 移除 [mcp_servers.sourcelex] 段落
  python3 -c "
import re
with open('$CODEX_CONFIG', 'r') as f:
    content = f.read()
content = re.sub(r'\n*# Sourcelex.*?\n\[mcp_servers\.sourcelex\]\n.*?(?=\n\[|\Z)', '', content, flags=re.DOTALL)
with open('$CODEX_CONFIG', 'w') as f:
    f.write(content.strip() + '\n')
" 2>/dev/null && echo "✅ 已从 Codex CLI 移除 MCP 配置"
fi

echo ""
echo "✅ 卸载完成。"
echo "   注：CLAUDE.md / AGENTS.md 中的说明文本未自动删除（不影响功能）。"
echo "   数据目录 (data/) 未删除，如需清理请手动删除。"
