// Package store 提供 RAG (Retrieval-Augmented Generation) 管线
// 将向量检索、图分析和重排序组合成完整的代码上下文组装流程
package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/repomind/repomind-go/internal/store/vector"
)

// RAGRequest RAG 检索请求
type RAGRequest struct {
	// Query 用户查询
	Query string `json:"query"`

	// TopK 向量检索数量
	TopK int `json:"top_k,omitempty"`

	// MinScore 最低相似度阈值
	MinScore float32 `json:"min_score,omitempty"`

	// IncludeCallGraph 是否包含调用图上下文
	IncludeCallGraph bool `json:"include_call_graph,omitempty"`

	// CallGraphDepth 调用图遍历深度
	CallGraphDepth int `json:"call_graph_depth,omitempty"`

	// IncludeFileContext 是否包含同文件的其他实体
	IncludeFileContext bool `json:"include_file_context,omitempty"`

	// EnableReranking 是否启用重排序
	EnableReranking bool `json:"enable_reranking,omitempty"`

	// Filters 元数据过滤条件
	Filters map[string]interface{} `json:"filters,omitempty"`

	// MaxContextLength 最大上下文长度（字符数）
	MaxContextLength int `json:"max_context_length,omitempty"`
}

// RAGResponse RAG 检索响应
type RAGResponse struct {
	// Context 组装好的上下文文本（可直接喂给 LLM）
	Context string `json:"context"`

	// Sources 来源信息
	Sources []RAGSource `json:"sources"`

	// Stats 检索统计
	Stats RAGStats `json:"stats"`
}

// RAGSource 上下文来源
type RAGSource struct {
	EntityID  string  `json:"entity_id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	FilePath  string  `json:"file_path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float32 `json:"score"`
	Reason    string  `json:"reason"`
}

// RAGStats RAG 统计
type RAGStats struct {
	VectorResults    int `json:"vector_results"`
	GraphResults     int `json:"graph_results"`
	FileResults      int `json:"file_results"`
	TotalSources     int `json:"total_sources"`
	ContextLength    int `json:"context_length"`
	RerankedResults  int `json:"reranked_results"`
}

// RAGPipeline RAG 管线
func (ks *KnowledgeStore) RAGPipeline(ctx context.Context, req RAGRequest) (*RAGResponse, error) {
	if ks.vectorStore == nil || ks.embedder == nil {
		return nil, fmt.Errorf("向量存储或嵌入器未初始化")
	}

	// 设置默认值
	if req.TopK <= 0 {
		req.TopK = 10
	}
	if req.MinScore <= 0 {
		req.MinScore = 0.3
	}
	if req.CallGraphDepth <= 0 {
		req.CallGraphDepth = 1
	}
	if req.MaxContextLength <= 0 {
		req.MaxContextLength = 16000
	}

	stats := RAGStats{}

	// ========== 第一阶段：向量检索 ==========
	queryVec, err := ks.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("嵌入查询失败: %w", err)
	}

	// 检索更多结果用于重排序
	retrieveK := req.TopK
	if req.EnableReranking {
		retrieveK = req.TopK * 3
	}

	vectorResults, err := ks.vectorStore.Search(ctx, queryVec, vector.SearchOptions{
		TopK:     retrieveK,
		MinScore: req.MinScore,
		Filter:   req.Filters,
	})
	if err != nil {
		return nil, fmt.Errorf("向量搜索失败: %w", err)
	}
	stats.VectorResults = len(vectorResults)

	// ========== 第二阶段：重排序 ==========
	if req.EnableReranking && len(vectorResults) > 0 {
		reranker := vector.NewCodeAwareReranker()
		vectorResults = reranker.Rerank(req.Query, vectorResults)
		stats.RerankedResults = len(vectorResults)
	}

	// 截断到 TopK
	if len(vectorResults) > req.TopK {
		vectorResults = vectorResults[:req.TopK]
	}

	// 收集所有来源（用于去重）
	sourceMap := make(map[string]*RAGSource)
	var orderedIDs []string

	for _, vr := range vectorResults {
		id := vr.Document.ID
		if _, exists := sourceMap[id]; exists {
			continue
		}
		src := &RAGSource{
			EntityID: id,
			Score:    vr.Score,
			Reason:   "向量语义匹配",
		}
		if name, ok := vr.Document.Metadata["name"].(string); ok {
			src.Name = name
		}
		if t, ok := vr.Document.Metadata["type"].(string); ok {
			src.Type = t
		}
		if fp, ok := vr.Document.Metadata["file_path"].(string); ok {
			src.FilePath = fp
		}
		sourceMap[id] = src
		orderedIDs = append(orderedIDs, id)
	}

	// ========== 第三阶段：调用图扩展（紧凑文本模式） ==========
	// 不再将调用者/被调用者作为独立代码块（浪费 token），
	// 改为收集紧凑的调用关系文本，附加在上下文末尾
	var callChainEntityIDs []string
	if req.IncludeCallGraph && ks.graphStore != nil {
		for _, vr := range vectorResults {
			callChainEntityIDs = append(callChainEntityIDs, vr.Document.ID)
		}
	}

	// ========== 第四阶段：同文件上下文扩展 ==========
	if req.IncludeFileContext && ks.graphStore != nil {
		fileSet := make(map[string]bool)
		for _, vr := range vectorResults {
			if fp, ok := vr.Document.Metadata["file_path"].(string); ok {
				fileSet[fp] = true
			}
		}

		for fp := range fileSet {
			fileNodes, _ := ks.graphStore.GetNodesByFile(ctx, fp)
			for _, node := range fileNodes {
				if _, exists := sourceMap[node.ID]; !exists {
					sourceMap[node.ID] = &RAGSource{
						EntityID:  node.ID,
						Name:      node.Name,
						Type:      string(node.Type),
						FilePath:  node.FilePath,
						StartLine: node.StartLine,
						EndLine:   node.EndLine,
						Score:     0.1,
						Reason:    "同文件实体",
					}
					orderedIDs = append(orderedIDs, node.ID)
					stats.FileResults++
				}
			}
		}
	}

	// ========== 第五阶段：组装上下文 ==========
	// 按分数排序所有来源
	sources := make([]RAGSource, 0, len(sourceMap))
	for _, id := range orderedIDs {
		if src, ok := sourceMap[id]; ok {
			sources = append(sources, *src)
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Score > sources[j].Score
	})

	stats.TotalSources = len(sources)

	// 组装上下文文本
	contextBuilder := &strings.Builder{}
	contextBuilder.WriteString(fmt.Sprintf("# 代码上下文 (查询: %s)\n\n", req.Query))

	currentLen := contextBuilder.Len()
	includedSources := make([]RAGSource, 0)

	for i, src := range sources {
		// 从向量结果中获取代码内容
		var codeContent string
		for _, vr := range vectorResults {
			if vr.Document.ID == src.EntityID {
				codeContent = vr.Document.Content
				break
			}
		}

		// 如果向量结果中没有，尝试从图中获取签名
		if codeContent == "" && ks.graphStore != nil {
			if node, err := ks.graphStore.GetNode(ctx, src.EntityID); err == nil {
				codeContent = node.Signature
			}
		}

		if codeContent == "" {
			continue
		}

		// 构建单个来源的上下文
		entry := fmt.Sprintf("## %d. [%s] %s\n", i+1, src.Type, src.Name)
		entry += fmt.Sprintf("File: %s (L%d-L%d) | Score: %.3f | %s\n\n", src.FilePath, src.StartLine, src.EndLine, src.Score, src.Reason)
		entry += "```\n" + codeContent + "\n```\n\n"

		if currentLen+len(entry) > req.MaxContextLength {
			break
		}

		contextBuilder.WriteString(entry)
		currentLen += len(entry)
		includedSources = append(includedSources, src)
	}

	// 附加紧凑调用链段落（如果启用了调用图）
	if len(callChainEntityIDs) > 0 {
		callChainSection := ks.BuildCallChainSection(ctx, callChainEntityIDs)
		if callChainSection != "" && currentLen+len(callChainSection) <= req.MaxContextLength {
			contextBuilder.WriteString("\n")
			contextBuilder.WriteString(callChainSection)
			stats.GraphResults = len(callChainEntityIDs)
		}
	}

	stats.ContextLength = contextBuilder.Len()

	return &RAGResponse{
		Context: contextBuilder.String(),
		Sources: includedSources,
		Stats:   stats,
	}, nil
}

// HybridSearch 混合搜索（向量 + 关键词）
func (ks *KnowledgeStore) HybridSearch(ctx context.Context, query string, topK int, filters map[string]interface{}) ([]SearchResult, error) {
	if ks.vectorStore == nil || ks.embedder == nil {
		return nil, fmt.Errorf("向量存储或嵌入器未初始化")
	}

	// 1. 向量搜索
	queryVec, err := ks.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("嵌入查询失败: %w", err)
	}

	vectorResults, err := ks.vectorStore.Search(ctx, queryVec, vector.SearchOptions{
		TopK:     topK * 2,
		MinScore: 0.3,
		Filter:   filters,
	})
	if err != nil {
		return nil, fmt.Errorf("向量搜索失败: %w", err)
	}

	// 2. 关键词重排序
	reranker := vector.NewCodeAwareReranker()
	reranked := reranker.Rerank(query, vectorResults)

	// 3. 截断到 TopK
	if len(reranked) > topK {
		reranked = reranked[:topK]
	}

	// 4. 转换结果
	results := make([]SearchResult, len(reranked))
	for i, r := range reranked {
		results[i] = SearchResult{
			EntityID: r.Document.ID,
			Content:  r.Document.Content,
			Score:    r.Score,
			Metadata: r.Document.Metadata,
		}
	}

	return results, nil
}

// ContextSearch 上下文感知搜索（搜索 + 自动扩展调用图上下文）
func (ks *KnowledgeStore) ContextSearch(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	// 1. 基础搜索
	results, err := ks.HybridSearch(ctx, query, topK, nil)
	if err != nil {
		return nil, err
	}

	if ks.graphStore == nil || len(results) == 0 {
		return results, nil
	}

	// 2. 为每个结果补充图上下文信息
	for i := range results {
		entityID := results[i].EntityID

		// 获取节点详细信息
		if node, err := ks.graphStore.GetNode(ctx, entityID); err == nil {
			if results[i].Metadata == nil {
				results[i].Metadata = make(map[string]interface{})
			}
			results[i].Metadata["signature"] = node.Signature
			results[i].Metadata["start_line"] = node.StartLine
			results[i].Metadata["end_line"] = node.EndLine
		}

		// 获取调用关系摘要
		callers, _ := ks.graphStore.GetCallersOf(ctx, entityID, 1)
		callees, _ := ks.graphStore.GetCalleesOf(ctx, entityID, 1)

		callerNames := make([]string, 0, len(callers))
		for _, c := range callers {
			callerNames = append(callerNames, c.Name)
		}
		calleeNames := make([]string, 0, len(callees))
		for _, c := range callees {
			calleeNames = append(calleeNames, c.Name)
		}

		if len(callerNames) > 0 {
			results[i].Metadata["callers"] = strings.Join(callerNames, ", ")
		}
		if len(calleeNames) > 0 {
			results[i].Metadata["callees"] = strings.Join(calleeNames, ", ")
		}
	}

	return results, nil
}
