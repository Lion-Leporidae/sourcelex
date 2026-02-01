# 🧠 RepoMind Go

> 代码知识库系统 - Go 语言实现

RepoMind 是一个智能代码分析和检索系统，提供代码语义搜索、函数调用链分析和 MCP 协议服务。

## ✨ 功能特性

- **🔍 语义搜索** - 基于向量嵌入的代码语义检索
- **🕸️ 调用关系分析** - 函数调用链上下追溯
- **🌳 多语言 AST 解析** - 支持 Python、Go、Java、JavaScript/TypeScript
- **📡 MCP 协议** - 与 AI 助手（Cursor、Claude）无缝集成
- **⚡ 增量更新** - 基于文件哈希的智能增量索引

## 🛠️ 技术栈

| 模块 | 技术 |
|------|------|
| CLI | [Cobra](https://github.com/spf13/cobra) |
| 配置 | [Viper](https://github.com/spf13/viper) |
| 日志 | [Zap](https://github.com/uber-go/zap) |
| Git | [go-git](https://github.com/go-git/go-git) |
| AST | [go-tree-sitter](https://github.com/smacker/go-tree-sitter) |
| 向量存储 | [Qdrant](https://github.com/qdrant/go-client) |
| 嵌入 | HuggingFace Inference API |

## 📦 安装

### 前置要求

- Go 1.21+
- GCC (用于 Tree-sitter CGO)
  - Windows: [MSYS2](https://www.msys2.org/) + MinGW-w64
  - macOS: Xcode Command Line Tools
  - Linux: `build-essential`

### 构建

```bash
# 克隆仓库
git clone https://github.com/your-username/repomind-go.git
cd repomind-go

# 安装依赖
go mod tidy

# 构建 (Windows)
$env:Path = "C:\msys64\ucrt64\bin;" + $env:Path
go build -o repomind.exe ./cmd/repomind

# 构建 (Linux/macOS)
go build -o repomind ./cmd/repomind
```

## 🚀 快速开始

### 1. 配置 HuggingFace API

编辑 `configs/config.yaml`:

```yaml
vector_store:
  huggingface:
    api_token: "hf_xxxxxxxxxx"  # 从 huggingface.co/settings/tokens 获取
```

### 2. 索引代码仓库

```bash
# 本地仓库
./repomind store --path /path/to/your/repo

# 远程仓库
./repomind store --repo https://github.com/user/repo --branch main
```

### 3. 启动 MCP 服务

```bash
./repomind serve --port 8000
```

## 📁 项目结构

```
repomind-go/
├── cmd/repomind/          # 程序入口
├── internal/
│   ├── analyzer/          # 代码分析
│   │   ├── parser/        # Tree-sitter AST 解析
│   │   └── entity/        # 实体提取
│   ├── cmd/               # CLI 命令
│   ├── config/            # 配置管理
│   ├── git/               # Git 仓库管理
│   ├── logger/            # 日志系统
│   └── store/             # 存储层
│       ├── vector/        # 向量存储
│       └── graph/         # 图存储
├── configs/               # 配置文件
└── go.mod
```

## ⚙️ 配置说明

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `paths.data_dir` | 数据存储目录 | `./data` |
| `vector_store.type` | 向量数据库类型 | `qdrant` |
| `vector_store.huggingface.model_id` | 嵌入模型 | `all-MiniLM-L6-v2` |
| `graph_store.type` | 图存储类型 | `memory` |
| `mcp.port` | MCP 服务端口 | `8000` |
| `logging.level` | 日志级别 | `info` |

## 📝 CLI 命令

```bash
# 查看帮助
./repomind --help

# 索引仓库
./repomind store --path . 

# 启动服务
./repomind serve

# 查看版本
./repomind version
```

## 🔧 开发状态

- [x] Phase 1: 基础设施 (CLI, 配置, 日志)
- [x] Phase 2: 代码分析 (Git, AST, 实体提取)
- [x] Phase 3: 存储层 (向量存储, 图存储)
- [ ] Phase 4: MCP 服务 (HTTP, SSE, 工具实现)

## 📄 License

MIT License
