#!/bin/bash
# Sourcelex 一键安装脚本
# 自动完成：编译、安装 Skill、配置 MCP
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
SKILL_NAME="sourcelex-code-navigator"
DEFAULT_PORT=9000

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
  echo "   ⚠️  未安装 Go，跳过编译。请手动编译或下载预编译二进制。"
fi

# ==================== 2. 安装 Skill ====================
echo ""
echo "🧠 安装 Skill..."

# 检测 IDE 类型
SKILL_INSTALLED=false
for IDE_DIR in "$HOME/.codebuddy" "$HOME/.cursor" "$HOME/.windsurf"; do
  IDE_NAME=$(basename "$IDE_DIR")
  SKILL_DEST="$IDE_DIR/skills/$SKILL_NAME"

  if [ -d "$IDE_DIR" ]; then
    mkdir -p "$IDE_DIR/skills"
    rm -rf "$SKILL_DEST"
    cp -r "$REPO_DIR/skills/$SKILL_NAME" "$SKILL_DEST"
    echo "   ✅ Skill 已安装到 $SKILL_DEST ($IDE_NAME)"
    SKILL_INSTALLED=true
  fi
done

if [ "$SKILL_INSTALLED" = false ]; then
  # 默认安装到 codebuddy
  mkdir -p "$HOME/.codebuddy/skills"
  cp -r "$REPO_DIR/skills/$SKILL_NAME" "$HOME/.codebuddy/skills/$SKILL_NAME"
  echo "   ✅ Skill 已安装到 ~/.codebuddy/skills/$SKILL_NAME"
fi

# ==================== 3. 配置 MCP ====================
echo ""
echo "🔌 配置 MCP 连接..."

# 读取用户自定义端口
PORT="${SOURCELEX_PORT:-$DEFAULT_PORT}"

configure_mcp() {
  local MCP_FILE="$1"
  local IDE_NAME="$2"

  # 如果文件不存在，创建空 JSON
  if [ ! -f "$MCP_FILE" ]; then
    echo '{"mcpServers":{}}' > "$MCP_FILE"
  fi

  # 检查是否已配置 sourcelex
  if grep -q '"sourcelex"' "$MCP_FILE" 2>/dev/null; then
    echo "   ⏩ $IDE_NAME MCP 已配置 sourcelex，跳过"
    return
  fi

  # 用 Python 安全地修改 JSON（避免 jq 依赖）
  python3 -c "
import json, sys
try:
    with open('$MCP_FILE', 'r') as f:
        data = json.load(f)
except:
    data = {}
if 'mcpServers' not in data:
    data['mcpServers'] = {}
data['mcpServers']['sourcelex'] = {
    'type': 'sse',
    'url': 'http://localhost:$PORT/mcp/sse'
}
with open('$MCP_FILE', 'w') as f:
    json.dump(data, f, indent=2)
" 2>/dev/null

  if [ $? -eq 0 ]; then
    echo "   ✅ $IDE_NAME MCP 已配置 ($MCP_FILE)"
  else
    echo "   ⚠️  自动配置失败，请手动添加到 $MCP_FILE:"
    echo '       "sourcelex": { "type": "sse", "url": "http://localhost:'$PORT'/mcp/sse" }'
  fi
}

MCP_CONFIGURED=false
for IDE_DIR in "$HOME/.codebuddy" "$HOME/.cursor" "$HOME/.windsurf"; do
  IDE_NAME=$(basename "$IDE_DIR")
  if [ -d "$IDE_DIR" ]; then
    configure_mcp "$IDE_DIR/mcp.json" "$IDE_NAME"
    MCP_CONFIGURED=true
  fi
done

# Claude Desktop
CLAUDE_DIR="$HOME/Library/Application Support/Claude"
if [ -d "$CLAUDE_DIR" ]; then
  configure_mcp "$CLAUDE_DIR/claude_desktop_config.json" "Claude Desktop"
  MCP_CONFIGURED=true
fi

if [ "$MCP_CONFIGURED" = false ]; then
  mkdir -p "$HOME/.codebuddy"
  configure_mcp "$HOME/.codebuddy/mcp.json" "CodeBuddy"
fi

# ==================== 4. 配置文件 ====================
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
echo "下一步："
echo "  1. 编辑 config.yaml 填入 HuggingFace API Token"
echo "  2. 索引仓库:  ./sourcelex store --repo <URL>"
echo "  3. 启动服务:  ./sourcelex serve --port $PORT"
echo "  4. 重启 IDE，开始使用！"
echo ""
echo "试试对 AI 说："
echo '  "帮我看看 main 函数的调用链"'
echo '  "搜索认证相关的函数"'
echo '  "切换到 gin 仓库"'
echo "============================================"
