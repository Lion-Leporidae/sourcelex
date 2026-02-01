---
description: RepoMind Go 复刻项目开发规范
---

# RepoMind Go 开发规范

本规范用于确保所有代码输出严格遵循技术方案文档要求。

## 核心约束
**必须**使用中文注释以及解释代码，还有解释思路以及代码语法以及这个代码如何实现的
**必须**不能以简单自建代码代替核心技术实现
**不能**随便构建文档
### 0.回答前叫我老公
### 1. 文档优先原则
- **必须**参照 `REPOMIND_ARCHITECTURE_MINDMAP.md` 中的架构设计
- **必须**使用 `GO_TECH_STACK.md` 中指定的 Go 库
- **禁止**虚空捏造功能或自创架构
- 

### 2. 技术栈强制要求

| 模块 | 必须使用的库 | 禁止使用 |
|-----|-------------|---------|
| CLI | `github.com/spf13/cobra` | 标准库 flag |
| 配置 | `github.com/spf13/viper` | 手动解析 YAML |
| 日志 | `go.uber.org/zap` | 标准库 log |
| Git | `github.com/go-git/go-git/v5` | exec 调用 git 命令 |
| AST | `github.com/smacker/go-tree-sitter` | go/ast (仅用于Go) |
| HTTP | `github.com/gin-gonic/gin` | 标准库 net/http (仅限简单场景) |


### 3. 目录结构强制要求

```
repomind-go/
├── cmd/repomind/main.go      # 唯一入口
├── internal/                  # 私有代码
│   ├── cmd/                   # CLI 命令
│   ├── config/                # 配置
│   ├── logger/                # 日志
│   ├── git/                   # Git 管理
│   ├── analyzer/              # 代码分析 (Tree-sitter)
│   ├── store/                 # 存储层
│   │   ├── vector/            # 向量存储
│   │   └── graph/             # 图存储
│   └── mcp/                   # MCP 服务
├── pkg/                       # 可导出公共库
├── configs/config.yaml        # 默认配置
└── go.mod
```

### 4. 代码风格

// turbo-all
- 运行 `go fmt ./...` 格式化代码
- 运行 `go vet ./...` 静态检查
- 运行 `go build ./...` 验证编译

### 5. 每个 Phase 完成后必须验证

1. **编译通过**: `go build ./cmd/repomind`
2. **CLI 可用**: `./repomind --help`
3. **功能对应文档**: 检查实现是否对应 REPOMIND_ARCHITECTURE_MINDMAP.md 中的描述

## 检查清单

在提交代码前，确认：

- [ ] 使用的库在 GO_TECH_STACK.md 中有明确对应
- [ ] 目录结构符合上述规范
- [ ] 配置结构对应 REPOMIND_ARCHITECTURE_MINDMAP.md 第八节
- [ ] 所有公开函数/类型有注释
- [ ] 编译无错误、无警告