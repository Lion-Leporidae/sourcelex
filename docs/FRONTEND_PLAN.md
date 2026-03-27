# Sourcelex 前端重写计划

## 目标

将嵌入式 JS/CSS 前端重写为 **Vite + React + TypeScript** 独立项目，采用**维基百科风格**的美术设计。

## 设计风格：维基百科 + 技术文档

### 核心设计语言

| 元素 | 维基风格特征 |
|------|------------|
| **色调** | 白底/浅灰底、深蓝标题（#0645AD）、黑色正文、左侧导航深灰 |
| **字体** | 衬线标题（Georgia/Linux Libertine）、无衬线正文（Helvetica）、等宽代码 |
| **布局** | 左侧目录导航 + 主内容区（文章式）、无花哨装饰 |
| **交互** | 内联链接蓝色下划线、折叠面板、标签页切换、面包屑导航 |
| **信息密度** | 高密度文本+表格，类似技术百科 |
| **图表** | 清晰的 SVG 图谱，浅色背景，黑色边+彩色节点 |

### 页面结构

```
┌──────────────────────────────────────────────┐
│  Sourcelex · 代码知识图谱            🔍 搜索  │  ← 顶部栏（简洁白底蓝字）
├──────────┬───────────────────────────────────┤
│ 目录     │ > sourcelex / internal / store     │  ← 面包屑
│          │                                    │
│ ▸ 概览   │  StoreEntities                     │
│ ▸ 文件树 │  ━━━━━━━━━━━━━━━                   │
│ ▾ 调用图 │  函数签名                           │
│   全部   │  func (ks *KnowledgeStore)          │
│   函数   │    StoreEntities(ctx, entities,     │
│   类     │    relations) error                 │
│   方法   │                                    │
│ ▸ 统计   │  描述                               │
│          │  ━━━━━━━                            │
│          │  将 CodeAnalyzer 提取的实体存储到    │
│          │  知识库中...                         │
│          │                                    │
│          │  调用关系                            │
│          │  ━━━━━━━━                           │
│          │  ┌────────────────────────┐         │
│          │  │    [调用图谱 SVG]       │         │
│          │  └────────────────────────┘         │
│          │                                    │
│          │  调用者（2）                         │
│          │  ├ cmd.runStore  store.go:235       │
│          │  └ cmd.runServe  serve.go:127       │
│          │                                    │
│          │  被调用（5）                         │
│          │  ├ graph.AddNodes  sqlite.go:159    │
│          │  ├ vector.Upsert   chromem.go:99    │
│          │  └ ...                              │
│          │                                    │
│          │  源代码                              │
│          │  ━━━━━━━                            │
│          │  ┌─ knowledge.go:81-240 ──────────┐│
│          │  │ 81  func (ks *KnowledgeStore)  ││
│          │  │ 82    StoreEntities(ctx ...     ││
│          │  └────────────────────────────────┘│
├──────────┴───────────────────────────────────┤
│  Sourcelex v0.1 · MIT · GitHub               │  ← 页脚
└──────────────────────────────────────────────┘
```

## 技术栈

| 层面 | 选型 | 原因 |
|------|------|------|
| 构建 | Vite 6 | 快速 HMR，原生 TS 支持 |
| 框架 | React 19 | 组件化，生态丰富 |
| 语言 | TypeScript 5.6 | 类型安全 |
| 路由 | React Router 7 | SPA 路由 |
| 图谱 | D3.js 7 | 直接用 SVG，灵活度高 |
| 代码高亮 | Shiki | 更好的 Tree-sitter 语法高亮 |
| HTTP | fetch + SWR | 数据获取和缓存 |
| 样式 | CSS Modules | 作用域隔离，维基风格不需要 UI 库 |

## 目录结构

```
web/
├── frontend/                  # React 前端项目
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── index.html
│   └── src/
│       ├── main.tsx            # 入口
│       ├── App.tsx             # 路由和布局
│       ├── api/                # API 客户端
│       │   ├── client.ts       # fetch 封装
│       │   ├── types.ts        # API 响应类型
│       │   └── hooks.ts        # useSWR hooks
│       ├── components/         # 共享组件
│       │   ├── Layout/
│       │   │   ├── Header.tsx
│       │   │   ├── Sidebar.tsx
│       │   │   ├── Footer.tsx
│       │   │   └── Breadcrumb.tsx
│       │   ├── CodeViewer/
│       │   │   └── CodeViewer.tsx
│       │   ├── Graph/
│       │   │   ├── CallGraph.tsx
│       │   │   └── FileGroupGraph.tsx
│       │   ├── FileTree/
│       │   │   └── FileTree.tsx
│       │   └── Search/
│       │       └── SearchBar.tsx
│       ├── pages/              # 路由页面
│       │   ├── Overview.tsx    # 仓库概览（首页）
│       │   ├── Explorer.tsx    # 调用图浏览器
│       │   ├── Entity.tsx      # 实体详情（维基文章页）
│       │   ├── File.tsx        # 文件内容页
│       │   └── Stats.tsx       # 统计页
│       └── styles/             # CSS Modules
│           ├── global.css      # 维基百科全局风格
│           ├── wiki.module.css # 文章页样式
│           └── graph.module.css # 图谱样式
├── handler.go                  # Go 嵌入 + 路由（改为代理 dist/）
└── static/                     # Vite 构建输出（嵌入到 Go）
    └── (build artifacts)
```

## 页面规划

### 1. 概览页 `/`
- 仓库名 + 描述
- 统计卡片（实体数、调用数、文件数）
- 最近索引时间
- 快速导航到各功能

### 2. 调用图浏览器 `/explorer`
- 左侧：侧边栏（文件树 + 筛选器）
- 中间：D3 按文件分组的调用图（维基风格 = 浅色背景 + 清晰线条）
- 可点击节点跳转到实体详情页

### 3. 实体详情页 `/entity/:id`（核心页面，维基文章风格）
- 面包屑导航
- 函数/类名作为文章标题
- 信息框（Infobox）：类型、文件、行号、签名
- 正文章节：
  - 描述（DocComment）
  - 调用图（小型内联 SVG）
  - 调用者表格
  - 被调用者表格
  - 源代码块
- 侧边目录（Table of Contents）

### 4. 文件页 `/file/:path`
- 面包屑路径导航
- 完整文件代码 + 行号
- 文件内实体列表（侧边链接）

### 5. 统计页 `/stats`
- 按文件/类型/包的分布图表
- 最多调用/被调用排行

## Go 后端集成方式

### 开发模式
```bash
cd web/frontend && npm run dev  # Vite dev server on :5173
sourcelex serve --port 9000     # Go API server
# Vite proxy /api → localhost:9000
```

### 生产模式
```bash
cd web/frontend && npm run build  # → web/static/
go build -o sourcelex ./cmd/sourcelex  # go:embed web/static/
```

`handler.go` 改为：
```go
//go:embed static
var staticFS embed.FS

func (h *Handler) SetupRoutes(router *gin.Engine) {
    // API 路由不变
    // 前端 SPA：所有非 /api 路径都返回 index.html
    router.NoRoute(func(c *gin.Context) {
        // 先尝试静态文件
        // 不存在则返回 index.html（SPA fallback）
    })
}
```

## 实施顺序

| 阶段 | 任务 | 预估时间 |
|------|------|----------|
| 1 | 创建 Vite+React+TS 项目骨架，配置构建 | 15min |
| 2 | 全局 CSS + 维基风格 Layout（Header/Sidebar/Footer） | 30min |
| 3 | API 客户端 + TypeScript 类型定义 | 15min |
| 4 | 概览页（Overview） | 15min |
| 5 | 实体详情页（Entity）— 维基文章风格 | 40min |
| 6 | 调用图组件（CallGraph）— D3 + 文件分组 | 30min |
| 7 | Explorer 页面 — 图谱浏览器 | 20min |
| 8 | 文件页 + 代码查看器 | 15min |
| 9 | 搜索功能 | 10min |
| 10 | Go 后端集成（embed + SPA fallback） | 15min |
| 11 | 统计页 | 10min |
| **合计** | | **~3.5h** |

## 维基风格 CSS 设计规范

```css
/* 核心配色 */
--wiki-bg: #f8f9fa;
--wiki-surface: #ffffff;
--wiki-border: #a2a9b1;
--wiki-text: #202122;
--wiki-link: #0645ad;
--wiki-link-visited: #0b0080;
--wiki-heading-border: #a2a9b1;
--wiki-infobox-bg: #f8f9fa;
--wiki-infobox-header: #cedff2;
--wiki-code-bg: #f5f5f5;
--wiki-sidebar-bg: #f6f6f6;
--wiki-sidebar-active: #eaecf0;

/* 字体 */
--font-heading: Georgia, "Linux Libertine", "Times New Roman", serif;
--font-body: -apple-system, "Segoe UI", Roboto, Helvetica, sans-serif;
--font-code: "JetBrains Mono", "Fira Code", "SF Mono", monospace;
```

## 备注

- 维基百科风格的核心是**信息密度 + 可读性 + 蓝色链接**，不是视觉炫酷
- 图谱部分用浅色背景 + 深色线条 + 彩色节点（不用暗色主题）
- 可以加一个暗色模式切换（暗色模式用深蓝底 + 浅蓝链接）
- 代码块保持深色主题（程序员习惯）
