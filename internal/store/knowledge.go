// Package store 提供统一知识存储层
// 对应架构文档: KnowledgeStore (统一知识存储)
//
// KnowledgeStore 整合向量存储和图存储，提供统一的 API:
// - 向量存储: 支持语义搜索（通过嵌入向量匹配）
// - 图存储: 支持调用关系分析（通过图遍历）
//
// 这是存储层的门面（Facade）模式实现
// 上层服务只需与 KnowledgeStore 交互，无需关心底层存储细节
package store

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/chunker"
	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/entity"
	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/relation"
	"github.com/Lion-Leporidae/sourcelex/internal/logger"
	"github.com/Lion-Leporidae/sourcelex/internal/store/graph"
	"github.com/Lion-Leporidae/sourcelex/internal/store/vector"
)

// KnowledgeStore 统一知识存储
// 整合向量存储和图存储
type KnowledgeStore struct {
	// vectorStore 向量存储（语义搜索）
	vectorStore vector.Store

	// graphStore 图存储（调用关系）
	graphStore graph.Store

	// embedder 文本嵌入器
	embedder vector.Embedder

	// chunker 符号分块器（用于提取完整代码）
	chunker *chunker.SymbolChunker

	// repoPath 仓库路径（用于读取代码内容）
	repoPath string

	// log 日志器
	log *logger.Logger
}

// Config KnowledgeStore 配置
type Config struct {
	// VectorStore 向量存储实例
	VectorStore vector.Store

	// GraphStore 图存储实例
	GraphStore graph.Store

	// Embedder 嵌入器实例
	Embedder vector.Embedder

	// RepoPath 仓库路径（用于读取代码内容）
	RepoPath string

	// Log 日志器
	Log *logger.Logger
}

// New 创建 KnowledgeStore
func New(cfg Config) *KnowledgeStore {
	log := cfg.Log
	if log == nil {
		log = logger.NewDefault()
	}
	return &KnowledgeStore{
		vectorStore: cfg.VectorStore,
		graphStore:  cfg.GraphStore,
		embedder:    cfg.Embedder,
		chunker:     chunker.NewSymbolChunker(),
		repoPath:    cfg.RepoPath,
		log:         log,
	}
}

// StoreEntities 存储实体列表（RepoMap 模式）
// 向量库只存函数签名+调用关系摘要（类似 aider RepoMap），不存完整代码
// 完整代码通过 MCP 工具 read_file_lines / grep_code 实时读取
func (ks *KnowledgeStore) StoreEntities(ctx context.Context, entities []entity.Entity, relations []relation.CallRelation) error {
	if len(entities) == 0 {
		return nil
	}

	// 1. 存储所有图节点
	nodes := make([]graph.Node, 0, len(entities))
	for _, e := range entities {
		nodes = append(nodes, graph.Node{
			ID:        e.QualifiedName,
			Name:      e.Name,
			Type:      graph.NodeType(e.Type),
			FilePath:  e.FilePath,
			StartLine: int(e.StartLine),
			EndLine:   int(e.EndLine),
			Signature: e.Signature,
		})
	}

	if ks.graphStore != nil && len(nodes) > 0 {
		if err := ks.graphStore.AddNodes(ctx, nodes); err != nil {
			return fmt.Errorf("存储节点失败: %w", err)
		}
	}
	nodes = nil

	// 2. 构建调用关系索引（用于 RepoMap 嵌入内容）
	calleesMap := make(map[string][]string) // entityID -> []calleeName
	callersMap := make(map[string][]string) // entityID -> []callerName

	// 构建实体名称查找表
	entityNames := make(map[string]string) // qualifiedName -> name
	for _, e := range entities {
		entityNames[e.QualifiedName] = e.Name
	}

	for _, r := range relations {
		calleeName := r.CalleeID
		if name, ok := entityNames[r.CalleeID]; ok {
			calleeName = name
		}
		callerName := r.CallerID
		if name, ok := entityNames[r.CallerID]; ok {
			callerName = name
		}
		calleesMap[r.CallerID] = append(calleesMap[r.CallerID], calleeName)
		callersMap[r.CalleeID] = append(callersMap[r.CalleeID], callerName)
	}

	// 去重调用关系
	for k, v := range calleesMap {
		calleesMap[k] = uniqueStrings(v)
	}
	for k, v := range callersMap {
		callersMap[k] = uniqueStrings(v)
	}

	// 3. 生成 RepoMap 嵌入内容并存储到向量库
	if ks.embedder == nil || ks.vectorStore == nil {
		return nil
	}

	const embedBatchSize = 32
	const maxRetries = 3
	const maxConcurrent = 4 // 并发嵌入请求数

	type embedItem struct {
		entity  entity.Entity
		content string
	}

	// 构建所有嵌入内容（RepoMap 摘要，每个约 100-300 字节，非常轻量）
	items := make([]embedItem, 0, len(entities))
	for _, e := range entities {
		content := chunker.BuildRepoMapContent(&e, calleesMap[e.QualifiedName], callersMap[e.QualifiedName])
		items = append(items, embedItem{entity: e, content: content})
	}

	// 将 items 分批
	type batch struct {
		idx   int
		items []embedItem
	}
	var batches []batch
	for start := 0; start < len(items); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, batch{idx: len(batches) + 1, items: items[start:end]})
	}
	totalBatches := len(batches)

	// 并发嵌入结果
	type batchResult struct {
		docs      []vector.Document
		failCount int
		firstErr  string
	}

	var (
		mu             sync.Mutex
		totalSuccess   int
		embedFailCount int
		firstError     string
	)

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	embedStartTime := time.Now()

	for _, b := range batches {
		wg.Add(1)
		go func(b batch) {
			defer wg.Done()
			sem <- struct{}{}        // 获取并发槽
			defer func() { <-sem }() // 释放并发槽

			texts := make([]string, len(b.items))
			for i, item := range b.items {
				texts[i] = item.content
			}

			ks.log.Info("嵌入中",
				"embed_batch", fmt.Sprintf("%d/%d", b.idx, totalBatches),
				"size", len(b.items),
			)

			// 带重试的批量嵌入
			var vectors [][]float32
			var embedErr error
			for retry := 0; retry < maxRetries; retry++ {
				vectors, embedErr = ks.embedder.EmbedBatch(ctx, texts)
				if embedErr == nil {
					break
				}
				ks.log.Warn("批量嵌入重试", "batch", b.idx, "retry", retry+1, "error", embedErr)
				time.Sleep(time.Duration(retry+1) * 2 * time.Second)
			}

			var res batchResult

			if embedErr != nil {
				ks.log.Warn("批量嵌入失败，回退到逐条嵌入", "batch", b.idx, "error", embedErr)
				for _, item := range b.items {
					vec, err := ks.embedder.Embed(ctx, item.content)
					if err != nil {
						res.failCount++
						if res.firstErr == "" {
							res.firstErr = err.Error()
						}
						continue
					}
					res.docs = append(res.docs, ks.buildVectorDoc(item.entity, item.content, vec))
				}
			} else {
				for i, item := range b.items {
					if i < len(vectors) && vectors[i] != nil {
						res.docs = append(res.docs, ks.buildVectorDoc(item.entity, item.content, vectors[i]))
					} else {
						res.failCount++
					}
				}
			}

			// 写入向量库
			if len(res.docs) > 0 {
				if err := ks.vectorStore.Upsert(ctx, res.docs); err != nil {
					ks.log.Warn("存储向量失败", "batch", b.idx, "error", err)
					res.failCount += len(res.docs)
					res.docs = nil
				}
			}

			mu.Lock()
			totalSuccess += len(res.docs)
			embedFailCount += res.failCount
			if firstError == "" && res.firstErr != "" {
				firstError = res.firstErr
			}
			mu.Unlock()

			ks.log.Info("嵌入批次完成", "embed_batch", b.idx, "success", len(res.docs))
		}(b)
	}

	wg.Wait()
	embedDuration := time.Since(embedStartTime)

	// 清理
	runtime.GC()

	if embedFailCount > 0 {
		ks.log.Warn("部分实体嵌入失败", "failed", embedFailCount, "total", len(entities), "first_error", firstError)
	}
	ks.log.Info("嵌入完成", "success", totalSuccess, "total", len(entities), "duration", embedDuration.Round(time.Millisecond))

	return nil
}

// uniqueStrings 字符串切片去重
func uniqueStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// buildVectorDoc 构建向量文档
// Content 只存摘要信息（减少 chromem 内存占用），完整代码通过 file_path + line 定位
func (ks *KnowledgeStore) buildVectorDoc(e entity.Entity, content string, vec []float32) vector.Document {
	// 构建精简的 Content：签名 + 文件位置（用于搜索结果展示）
	summary := fmt.Sprintf("[%s] %s\nFile: %s:%d-%d",
		string(e.Type), e.QualifiedName, e.FilePath, e.StartLine, e.EndLine)
	if e.Signature != "" {
		summary += "\n" + e.Signature
	}

	return vector.Document{
		ID:      e.QualifiedName,
		Content: summary,
		Vector:  vec,
		Metadata: map[string]interface{}{
			"name":       e.Name,
			"type":       string(e.Type),
			"file_path":  e.FilePath,
			"start_line": e.StartLine,
			"end_line":   e.EndLine,
			"language":   e.Language,
			"signature":  e.Signature,
		},
	}
}

// StoreRelations 存储调用关系
// 参数:
//   - relations: 调用关系列表（source -> target）
func (ks *KnowledgeStore) StoreRelations(ctx context.Context, relations []Relation) error {
	if ks.graphStore == nil || len(relations) == 0 {
		return nil
	}

	edges := make([]graph.Edge, 0, len(relations))
	for _, r := range relations {
		edges = append(edges, graph.Edge{
			Source:     r.SourceID,
			Target:     r.TargetID,
			Type:       graph.EdgeType(r.Type),
			SourceFile: r.SourceFile,
			Line:       r.Line,
			Confidence: r.Confidence,
		})
	}

	return ks.graphStore.AddEdges(ctx, edges)
}

// Relation 调用关系
type Relation struct {
	SourceID   string
	TargetID   string
	Type       string
	SourceFile string
	Line       int
	Confidence float64
}

// SemanticSearch 语义搜索
// 根据自然语言查询找到相关代码
func (ks *KnowledgeStore) SemanticSearch(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if ks.vectorStore == nil {
		return nil, fmt.Errorf("向量存储未初始化")
	}

	// 1. 将查询转换为向量
	queryVec, err := ks.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("嵌入查询失败: %w", err)
	}

	// 2. 向量搜索
	results, err := ks.vectorStore.Search(ctx, queryVec, vector.SearchOptions{
		TopK:     topK,
		MinScore: 0.5,
	})
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	// 3. 转换结果
	searchResults := make([]SearchResult, len(results))
	for i, r := range results {
		searchResults[i] = SearchResult{
			EntityID: r.Document.ID,
			Content:  r.Document.Content,
			Score:    r.Score,
			Metadata: r.Document.Metadata,
		}
	}

	return searchResults, nil
}

// SearchResult 搜索结果
type SearchResult struct {
	EntityID string
	Content  string
	Score    float32
	Metadata map[string]interface{}
}

// GetCallersOf 获取调用者
// 找到所有调用指定函数的函数
func (ks *KnowledgeStore) GetCallersOf(ctx context.Context, entityID string, depth int) ([]graph.Node, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}

	return ks.graphStore.GetCallersOf(ctx, entityID, depth)
}

// GetCalleesOf 获取被调用者
// 找到指定函数调用的所有函数
func (ks *KnowledgeStore) GetCalleesOf(ctx context.Context, entityID string, depth int) ([]graph.Node, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}

	return ks.graphStore.GetCalleesOf(ctx, entityID, depth)
}

// GetEntity 获取实体信息
func (ks *KnowledgeStore) GetEntity(ctx context.Context, entityID string) (*graph.Node, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}

	return ks.graphStore.GetNode(ctx, entityID)
}

// Stats 返回存储统计信息
func (ks *KnowledgeStore) Stats(ctx context.Context) (*StoreStats, error) {
	stats := &StoreStats{}

	if ks.vectorStore != nil {
		count, _ := ks.vectorStore.Count(ctx)
		stats.VectorCount = count
	}

	if ks.graphStore != nil {
		nodeCount, _ := ks.graphStore.NodeCount(ctx)
		edgeCount, _ := ks.graphStore.EdgeCount(ctx)
		stats.NodeCount = nodeCount
		stats.EdgeCount = edgeCount
	}

	return stats, nil
}

// StoreStats 存储统计
type StoreStats struct {
	VectorCount int64
	NodeCount   int64
	EdgeCount   int64
}

// GetSubgraph 获取以指定实体为中心的子图
func (ks *KnowledgeStore) GetSubgraph(ctx context.Context, entityID string, depth int) (*graph.SubgraphResult, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.GetSubgraph(ctx, entityID, depth)
}

// GetAllNodes 获取所有节点
func (ks *KnowledgeStore) GetAllNodes(ctx context.Context) ([]graph.Node, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.GetAllNodes(ctx)
}

// GetAllEdges 获取所有边
func (ks *KnowledgeStore) GetAllEdges(ctx context.Context) ([]graph.Edge, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.GetAllEdges(ctx)
}

// GetNodesByFile 获取指定文件中的所有实体
func (ks *KnowledgeStore) GetNodesByFile(ctx context.Context, filePath string) ([]graph.Node, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.GetNodesByFile(ctx, filePath)
}

// GetNodesByType 获取指定类型的所有实体
func (ks *KnowledgeStore) GetNodesByType(ctx context.Context, nodeType graph.NodeType) ([]graph.Node, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.GetNodesByType(ctx, nodeType)
}

// FindPath 查找两个实体之间的路径
func (ks *KnowledgeStore) FindPath(ctx context.Context, sourceID, targetID string) (*graph.PathResult, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.FindPath(ctx, sourceID, targetID)
}

// DetectCycles 检测调用图中的循环依赖
func (ks *KnowledgeStore) DetectCycles(ctx context.Context) ([][]string, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.DetectCycles(ctx)
}

// TopologicalSort 获取调用图的拓扑排序
func (ks *KnowledgeStore) TopologicalSort(ctx context.Context) ([]string, error) {
	if ks.graphStore == nil {
		return nil, fmt.Errorf("图存储未初始化")
	}
	return ks.graphStore.TopologicalSort(ctx)
}

// Close 关闭所有存储
func (ks *KnowledgeStore) Close() error {
	var lastErr error

	if ks.vectorStore != nil {
		if err := ks.vectorStore.Close(); err != nil {
			lastErr = err
		}
	}

	if ks.graphStore != nil {
		if err := ks.graphStore.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// ========== 紧凑调用链输出 ==========

// CallChainCompact 生成紧凑的调用链文本表示
// 设计目标：用最少的 token 表达调用链信息，方便 AI 助手理解代码结构
//
// depth=1 输出示例（~20 tokens vs JSON 的 500+ tokens）:
//
//	SemanticSearch (store/knowledge.go:278)
//	  → Embed, Search
//	  ← handleSemanticSearch, HybridSearch
//
// depth=2 输出示例（树形展开）:
//
//	SemanticSearch (store/knowledge.go:278)
//	  调用:
//	    → Embed (vector/hf.go:45)
//	    → Search (vector/chromem.go:23)
//	  被调用:
//	    ← handleSemanticSearch (mcp/handlers.go:190)
//	    ← HybridSearch (store/rag.go:303)
//	      ← ContextSearch (store/rag.go:347)
func (ks *KnowledgeStore) CallChainCompact(ctx context.Context, entityID string, depth int) (string, error) {
	if ks.graphStore == nil {
		return "", fmt.Errorf("图存储未初始化")
	}
	if depth <= 0 {
		depth = 1
	}

	node, err := ks.graphStore.GetNode(ctx, entityID)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s (%s:%d)\n", node.ID, shortPath(node.FilePath), node.StartLine))

	callees, _ := ks.graphStore.GetCalleesOf(ctx, entityID, depth)
	callers, _ := ks.graphStore.GetCallersOf(ctx, entityID, depth)

	if depth == 1 {
		if len(callees) > 0 {
			names := make([]string, len(callees))
			for i, c := range callees {
				names[i] = c.Name
			}
			b.WriteString(fmt.Sprintf("  → %s\n", strings.Join(names, ", ")))
		}
		if len(callers) > 0 {
			names := make([]string, len(callers))
			for i, c := range callers {
				names[i] = c.Name
			}
			b.WriteString(fmt.Sprintf("  ← %s\n", strings.Join(names, ", ")))
		}
	} else {
		subgraph, err := ks.graphStore.GetSubgraph(ctx, entityID, depth)
		if err != nil {
			return b.String(), nil
		}

		calleesAdj := make(map[string][]string)
		callersAdj := make(map[string][]string)
		nodeMap := make(map[string]*graph.Node)
		for i := range subgraph.Nodes {
			nodeMap[subgraph.Nodes[i].ID] = &subgraph.Nodes[i]
		}
		for _, e := range subgraph.Edges {
			if e.Type == graph.EdgeTypeCalls {
				calleesAdj[e.Source] = append(calleesAdj[e.Source], e.Target)
				callersAdj[e.Target] = append(callersAdj[e.Target], e.Source)
			}
		}

		if targets := calleesAdj[entityID]; len(targets) > 0 {
			b.WriteString("  调用:\n")
			writeCallTreeLines(&b, targets, calleesAdj, nodeMap, "    ", depth-1, make(map[string]bool), "→")
		}
		if sources := callersAdj[entityID]; len(sources) > 0 {
			b.WriteString("  被调用:\n")
			writeCallTreeLines(&b, sources, callersAdj, nodeMap, "    ", depth-1, make(map[string]bool), "←")
		}
	}

	return b.String(), nil
}

// writeCallTreeLines 递归写入调用树的每一行
func writeCallTreeLines(b *strings.Builder, ids []string, adj map[string][]string, nodeMap map[string]*graph.Node, indent string, remainDepth int, visited map[string]bool, arrow string) {
	for _, id := range ids {
		if visited[id] {
			continue
		}
		visited[id] = true

		if n, ok := nodeMap[id]; ok {
			b.WriteString(fmt.Sprintf("%s%s %s (%s:%d)\n", indent, arrow, n.Name, shortPath(n.FilePath), n.StartLine))
		} else {
			b.WriteString(fmt.Sprintf("%s%s %s\n", indent, arrow, id))
		}

		if remainDepth > 0 {
			if children := adj[id]; len(children) > 0 {
				writeCallTreeLines(b, children, adj, nodeMap, indent+"  ", remainDepth-1, visited, arrow)
			}
		}
	}
}

// CallGraphSummary 生成完整调用图的紧凑文本摘要
// 按文件分组的邻接表格式，一次请求获取全部调用关系
//
// 输出示例（100 个函数约 1000 tokens，JSON 需要 10000+）:
//
//	# 调用图 (45 个函数, 62 条调用)
//
//	## mcp/handlers.go
//	handleSemanticSearch → SemanticSearch
//	handleGetCallMap → GetCallersOf, GetCalleesOf
//
//	## store/knowledge.go
//	SemanticSearch → Embed, Search
func (ks *KnowledgeStore) CallGraphSummary(ctx context.Context, fileFilter string) (string, error) {
	if ks.graphStore == nil {
		return "", fmt.Errorf("图存储未初始化")
	}

	var nodes []graph.Node
	var err error
	if fileFilter != "" {
		nodes, err = ks.graphStore.GetNodesByFile(ctx, fileFilter)
	} else {
		nodes, err = ks.graphStore.GetAllNodes(ctx)
	}
	if err != nil {
		return "", err
	}

	edges, err := ks.graphStore.GetAllEdges(ctx)
	if err != nil {
		return "", err
	}

	nodeMap := make(map[string]*graph.Node)
	nodeSet := make(map[string]bool)
	for i := range nodes {
		nodeMap[nodes[i].ID] = &nodes[i]
		nodeSet[nodes[i].ID] = true
	}

	// 构建去重的邻接表: sourceID → {targetName: true}
	sourceCallees := make(map[string]map[string]bool)
	for _, e := range edges {
		if e.Type != graph.EdgeTypeCalls {
			continue
		}
		if nodeMap[e.Source] == nil {
			continue
		}
		if fileFilter != "" && !nodeSet[e.Source] {
			continue
		}
		if sourceCallees[e.Source] == nil {
			sourceCallees[e.Source] = make(map[string]bool)
		}
		targetName := e.Target
		if tn, ok := nodeMap[e.Target]; ok {
			targetName = tn.Name
		}
		sourceCallees[e.Source][targetName] = true
	}

	// 按文件分组，文件内按行号排序
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].FilePath != nodes[j].FilePath {
			return nodes[i].FilePath < nodes[j].FilePath
		}
		return nodes[i].StartLine < nodes[j].StartLine
	})

	fileGroups := make(map[string][]string)
	var fileOrder []string

	for _, n := range nodes {
		targets := sourceCallees[n.ID]
		if len(targets) == 0 {
			continue
		}
		fp := shortPath(n.FilePath)
		if _, exists := fileGroups[fp]; !exists {
			fileOrder = append(fileOrder, fp)
		}
		targetList := make([]string, 0, len(targets))
		for t := range targets {
			targetList = append(targetList, t)
		}
		sort.Strings(targetList)

		entry := fmt.Sprintf("%s → %s", n.Name, strings.Join(targetList, ", "))
		fileGroups[fp] = append(fileGroups[fp], entry)
	}

	var b strings.Builder
	callCount := 0
	for _, targets := range sourceCallees {
		callCount += len(targets)
	}
	b.WriteString(fmt.Sprintf("# 调用图 (%d 个函数, %d 条调用)\n\n", len(nodes), callCount))

	for _, fp := range fileOrder {
		entries := fileGroups[fp]
		b.WriteString(fmt.Sprintf("## %s\n", fp))
		for _, entry := range entries {
			b.WriteString(entry + "\n")
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

// BuildCallChainSection 为 RAG 上下文生成紧凑的调用关系段落
// 给定一组实体 ID，生成它们之间的调用关系摘要
func (ks *KnowledgeStore) BuildCallChainSection(ctx context.Context, entityIDs []string) string {
	if ks.graphStore == nil || len(entityIDs) == 0 {
		return ""
	}

	var lines []string
	seen := make(map[string]bool)

	for _, id := range entityIDs {
		callees, _ := ks.graphStore.GetCalleesOf(ctx, id, 1)
		callers, _ := ks.graphStore.GetCallersOf(ctx, id, 1)

		node, err := ks.graphStore.GetNode(ctx, id)
		if err != nil {
			continue
		}

		if len(callees) > 0 {
			names := make([]string, len(callees))
			for i, c := range callees {
				names[i] = c.Name
			}
			line := fmt.Sprintf("%s → %s", node.Name, strings.Join(names, ", "))
			if !seen[line] {
				lines = append(lines, line)
				seen[line] = true
			}
		}

		if len(callers) > 0 {
			names := make([]string, len(callers))
			for i, c := range callers {
				names[i] = c.Name
			}
			line := fmt.Sprintf("%s ← %s", node.Name, strings.Join(names, ", "))
			if !seen[line] {
				lines = append(lines, line)
				seen[line] = true
			}
		}
	}

	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("---\n## 调用关系\n")
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// shortPath 截取路径最后两级，减少 token 消耗
func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
