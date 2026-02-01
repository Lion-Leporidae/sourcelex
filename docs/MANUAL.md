# 📖 RepoMind Go 操作手册

## 目录

1. [环境准备](#1-环境准备)
2. [安装部署](#2-安装部署)
3. [基本使用](#3-基本使用)
4. [API 使用指南](#4-api-使用指南)
5. [与 AI 助手集成](#5-与-ai-助手集成)
6. [故障排查](#6-故障排查)

---

## 1. 环境准备

### 1.1 系统要求

| 项目 | 要求 |
|------|------|
| 操作系统 | Windows 10+, macOS 10.15+, Linux |
| Go 版本 | 1.21 或更高 |
| 内存 | 建议 4GB+ |
| 磁盘 | 依据仓库大小 |

### 1.2 安装 Go

**Windows:**
```powershell
# 下载安装包
https://go.dev/dl/

# 验证安装
go version
```

**macOS:**
```bash
brew install go
```

**Linux:**
```bash
sudo apt install golang-go  # Debian/Ubuntu
sudo yum install golang     # CentOS/RHEL
```

### 1.3 安装 GCC (Windows)

RepoMind 使用 Tree-sitter 进行代码解析，需要 CGO 支持。

```powershell
# 1. 下载 MSYS2
https://www.msys2.org/

# 2. 安装 MinGW-w64
pacman -S mingw-w64-ucrt-x86_64-gcc

# 3. 添加到 PATH
# 添加 C:\msys64\ucrt64\bin 到系统环境变量

# 4. 验证
gcc --version
```

---

## 2. 安装部署

### 2.1 获取源码

```bash
git clone https://github.com/your-username/repomind-go.git
cd repomind-go
```

### 2.2 安装依赖

```bash
go mod tidy
```

### 2.3 构建

**Windows (PowerShell):**
```powershell
$env:Path = "C:\msys64\ucrt64\bin;" + $env:Path
$env:CGO_ENABLED = "1"
go build -o repomind.exe ./cmd/repomind
```

**Linux/macOS:**
```bash
CGO_ENABLED=1 go build -o repomind ./cmd/repomind
```

### 2.4 配置

创建 `configs/config.yaml`:

```yaml
# 路径配置
paths:
  data_dir: ./data
  temp_dir: ./temp

# HuggingFace 嵌入配置（必填）
vector_store:
  huggingface:
    api_token: "hf_xxxxxxxxxx"  # 从 huggingface.co 获取
    model_id: "sentence-transformers/all-MiniLM-L6-v2"
    dimension: 384

# MCP 服务配置
mcp:
  host: 0.0.0.0
  port: 8000

# 日志配置
logging:
  level: info
```

---

## 3. 基本使用

### 3.1 索引本地仓库

```bash
# 索引当前目录
./repomind store --path .

# 索引指定目录
./repomind store --path /path/to/your/project
```

**输出示例:**
```
INFO  文件扫描完成  total=42 new=42 modified=0
INFO  实体统计     functions=28 classes=15 methods=64
```

### 3.2 索引远程仓库

```bash
# 克隆并索引 GitHub 仓库
./repomind store --repo https://github.com/user/repo

# 指定分支
./repomind store --repo https://github.com/user/repo --branch develop
```

### 3.3 启动服务

```bash
# 默认端口 8000
./repomind serve

# 指定端口
./repomind serve --port 9000

# 指定地址和端口
./repomind serve --host 127.0.0.1 --port 8080
```

**输出示例:**
```
INFO  MCP 服务器启动  address=0.0.0.0:8000
INFO  服务器已启动，按 Ctrl+C 停止
```

---

## 4. API 使用指南

### 4.1 健康检查

```bash
curl http://localhost:8000/health
```

**响应:**
```json
{"status": "ok", "service": "repomind-mcp"}
```

### 4.2 语义搜索

用自然语言描述你要找的代码：

```bash
curl -X POST http://localhost:8000/api/v1/search/semantic \
  -H "Content-Type: application/json" \
  -d '{
    "query": "处理用户登录验证的函数",
    "top_k": 5
  }'
```

**响应:**
```json
{
  "success": true,
  "data": [
    {
      "entity_id": "auth.validate_user",
      "name": "validate_user",
      "type": "function",
      "file_path": "src/auth/login.py",
      "start_line": 45,
      "end_line": 62,
      "score": 0.89
    }
  ]
}
```

### 4.3 获取实体详情

```bash
curl http://localhost:8000/api/v1/entity/auth.validate_user
```

### 4.4 查询调用关系

```bash
# 获取调用关系图（默认深度 1）
curl http://localhost:8000/api/v1/callmap/main.process

# 指定遍历深度
curl http://localhost:8000/api/v1/callmap/main.process?depth=3
```

**响应:**
```json
{
  "success": true,
  "data": {
    "entity_id": "main.process",
    "callers": [
      {"id": "main.run", "name": "run", "type": "function"}
    ],
    "callees": [
      {"id": "utils.parse", "name": "parse", "type": "function"},
      {"id": "db.save", "name": "save", "type": "function"}
    ]
  }
}
```

### 4.5 SSE 连接

```bash
curl -N http://localhost:8000/mcp/sse
```

---

## 5. 与 AI 助手集成

### 5.1 Cursor 集成

在 Cursor 的 MCP 配置中添加:

```json
{
  "mcpServers": {
    "repomind": {
      "url": "http://localhost:8000/mcp/sse"
    }
  }
}
```

### 5.2 自定义集成

```python
import requests

# 语义搜索
response = requests.post(
    "http://localhost:8000/api/v1/search/semantic",
    json={"query": "数据库连接池", "top_k": 5}
)
results = response.json()["data"]

for r in results:
    print(f"{r['name']} - {r['file_path']}:{r['start_line']}")
```

---

## 6. 故障排查

### 6.1 CGO 编译错误

**错误:** `gcc: command not found`

**解决:**
```powershell
# Windows: 确保 MinGW 在 PATH 中
$env:Path = "C:\msys64\ucrt64\bin;" + $env:Path
$env:CGO_ENABLED = "1"
```

### 6.2 HuggingFace API 错误

**错误:** `API 错误 (401): Invalid token`

**解决:**
1. 检查 `config.yaml` 中的 `api_token`
2. 确认 Token 在 [huggingface.co/settings/tokens](https://huggingface.co/settings/tokens) 有效

### 6.3 端口被占用

**错误:** `bind: address already in use`

**解决:**
```bash
# 使用其他端口
./repomind serve --port 9000

# 或查找占用进程
# Windows
netstat -ano | findstr :8000
# Linux/macOS
lsof -i :8000
```

### 6.4 服务无响应

1. 检查服务是否启动: `curl localhost:8000/health`
2. 检查日志输出
3. 确认防火墙设置

---

## 附录: 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `REPOMIND_MCP_PORT` | 服务端口 | 8000 |
| `REPOMIND_LOG_LEVEL` | 日志级别 | info |
| `CGO_ENABLED` | CGO 开关 | 1 |
| `CC` | C 编译器 | gcc |
