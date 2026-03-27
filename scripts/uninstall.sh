#!/bin/bash
# Sourcelex 卸载脚本
# 移除 Skill 和 MCP 配置
set -e

SKILL_NAME="sourcelex-code-navigator"

echo "🗑️  Sourcelex 卸载"
echo "=================="
echo ""

# 移除 Skill
for IDE_DIR in "$HOME/.codebuddy" "$HOME/.cursor" "$HOME/.windsurf"; do
  SKILL_DIR="$IDE_DIR/skills/$SKILL_NAME"
  if [ -d "$SKILL_DIR" ]; then
    rm -rf "$SKILL_DIR"
    echo "✅ 已移除 Skill: $SKILL_DIR"
  fi
done

# 从 MCP 配置中移除 sourcelex
remove_mcp() {
  local MCP_FILE="$1"
  if [ ! -f "$MCP_FILE" ]; then return; fi
  if ! grep -q '"sourcelex"' "$MCP_FILE" 2>/dev/null; then return; fi

  python3 -c "
import json
with open('$MCP_FILE', 'r') as f:
    data = json.load(f)
if 'mcpServers' in data and 'sourcelex' in data['mcpServers']:
    del data['mcpServers']['sourcelex']
with open('$MCP_FILE', 'w') as f:
    json.dump(data, f, indent=2)
" 2>/dev/null && echo "✅ 已从 $MCP_FILE 移除 MCP 配置"
}

for IDE_DIR in "$HOME/.codebuddy" "$HOME/.cursor" "$HOME/.windsurf"; do
  remove_mcp "$IDE_DIR/mcp.json"
done
remove_mcp "$HOME/Library/Application Support/Claude/claude_desktop_config.json"

echo ""
echo "✅ 卸载完成。数据目录 (data/) 未删除，如需清理请手动删除。"
