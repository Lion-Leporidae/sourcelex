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
	"log"

	"github.com/repomind/repomind-go/internal/analyzer/chunker"
	"github.com/repomind/repomind-go/internal/analyzer/entity"
	"github.com/repomind/repomind-go/internal/store/graph"
	"github.com/repomind/repomind-go/internal/store/vector"
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
}

// New 创建 KnowledgeStore
func New(cfg Config) *KnowledgeStore {
	return &KnowledgeStore{
		vectorStore: cfg.VectorStore,
		graphStore:  cfg.GraphStore,
		embedder:    cfg.Embedder,
		chunker:     chunker.NewSymbolChunker(),
		repoPath:    cfg.RepoPath,
	}
}

// StoreEntities 存储实体列表
// 将 CodeAnalyzer 提取的实体存储到知识库中
// 流程:
// 1. 使用 SymbolChunker 将实体转换为代码分块
// 2. 存储图节点
// 3. 为每个分块生成嵌入向量并存储
// 注意: 即使嵌入失败，图节点也会被存储
func (ks *KnowledgeStore) StoreEntities(ctx context.Context, entities []entity.Entity) error {
	if len(entities) == 0 {
		return nil
	}

	// 1. 使用 SymbolChunker 生成代码分块
	chunkOpts := chunker.ChunkOptions{
		RepoPath:       ks.repoPath,
		MaxChunkSize:   4096,
		IncludeContext: true,
	}
	chunks, _ := ks.chunker.ChunkEntities(ctx, entities, chunkOpts)

	// 构建分块查找表 (entity qualified_name -> chunk)
	chunkMap := make(map[string]*chunker.CodeChunk)
	for i := range chunks {
		ch := &chunks[i]
		if ch.Entity != nil {
			chunkMap[ch.Entity.QualifiedName] = ch
		}
	}

	// 2. 构建并存储所有图节点
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

	// 存储到图数据库
	if ks.graphStore != nil && len(nodes) > 0 {
		if err := ks.graphStore.AddNodes(ctx, nodes); err != nil {
			return fmt.Errorf("存储节点失败: %w", err)
		}
	}

	// 3. 尝试为每个实体生成嵌入向量
	if ks.embedder == nil || ks.vectorStore == nil {
		return nil
	}

	docs := make([]vector.Document, 0, len(entities))
	embedFailCount := 0
	firstError := ""

	for _, e := range entities {
		// 构建文档内容（用于向量化）
		// 优先使用分块内容（包含完整代码）
		var content string
		if ch, ok := chunkMap[e.QualifiedName]; ok && ch.Content != "" {
			content = chunker.BuildEmbeddingContent(ch)
		} else {
			// 回退到简单内容（仅签名）
			content = fmt.Sprintf("[%s] %s\n%s\n%s",
				string(e.Type),
				e.QualifiedName,
				e.Signature,
				e.FilePath,
			)
		}

		// 生成嵌入向量
		vec, err := ks.embedder.Embed(ctx, content)
		if err != nil {
			embedFailCount++
			if firstError == "" {
				firstError = err.Error()
			}
			continue
		}

		// 构建向量文档
		docs = append(docs, vector.Document{
			ID:      e.QualifiedName,
			Content: content,
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
		})
	}

	// 打印嵌入结果统计
	if embedFailCount > 0 {
		log.Printf("[WARN] 嵌入失败: %d/%d, 首个错误: %s", embedFailCount, len(entities), firstError)
	}
	log.Printf("[INFO] 嵌入成功: %d/%d", len(docs), len(entities))

	// 存储到向量数据库
	if len(docs) > 0 {
		if err := ks.vectorStore.Upsert(ctx, docs); err != nil {
			return fmt.Errorf("存储向量失败: %w", err)
		}
	}

	return nil
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
			Source: r.SourceID,
			Target: r.TargetID,
			Type:   graph.EdgeType(r.Type),
		})
	}

	return ks.graphStore.AddEdges(ctx, edges)
}

// Relation 调用关系
type Relation struct {
	SourceID string
	TargetID string
	Type     string // calls, inherits, imports
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
