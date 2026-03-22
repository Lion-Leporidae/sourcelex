// Package vector 提供搜索结果重排序功能
// 支持多种重排序策略，提升检索精度
package vector

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Reranker 重排序器接口
type Reranker interface {
	// Rerank 对搜索结果进行重排序
	// query: 原始查询文本
	// results: 初始搜索结果
	// 返回重排序后的结果
	Rerank(query string, results []SearchResult) []SearchResult
}

// BM25Reranker 基于 BM25 算法的关键词重排序器
// 结合向量相似度和关键词匹配度进行混合排序
type BM25Reranker struct {
	// K1 词频饱和参数，典型值 1.2-2.0
	K1 float64
	// B 文档长度归一化参数，典型值 0.75
	B float64
	// VectorWeight 向量分数权重 (0-1)
	VectorWeight float64
	// BM25Weight BM25 分数权重 (0-1)
	BM25Weight float64
}

// NewBM25Reranker 创建 BM25 重排序器
func NewBM25Reranker() *BM25Reranker {
	return &BM25Reranker{
		K1:           1.5,
		B:            0.75,
		VectorWeight: 0.6,
		BM25Weight:   0.4,
	}
}

// Rerank 使用 BM25 + 向量分数混合重排序
func (r *BM25Reranker) Rerank(query string, results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return results
	}

	// 计算平均文档长度
	var totalLen float64
	for _, res := range results {
		totalLen += float64(len(tokenize(res.Document.Content)))
	}
	avgDL := totalLen / float64(len(results))
	if avgDL == 0 {
		avgDL = 1
	}

	// 计算 IDF
	idf := make(map[string]float64)
	n := float64(len(results))
	for _, term := range queryTerms {
		df := 0.0
		for _, res := range results {
			content := strings.ToLower(res.Document.Content)
			if strings.Contains(content, term) {
				df++
			}
		}
		idf[term] = math.Log((n-df+0.5)/(df+0.5) + 1)
	}

	type scoredResult struct {
		result     SearchResult
		finalScore float32
	}

	scored := make([]scoredResult, len(results))
	for i, res := range results {
		// BM25 分数
		docTokens := tokenize(res.Document.Content)
		dl := float64(len(docTokens))
		tf := make(map[string]float64)
		for _, t := range docTokens {
			tf[t]++
		}

		bm25Score := 0.0
		for _, term := range queryTerms {
			termTF := tf[term]
			termIDF := idf[term]
			numerator := termTF * (r.K1 + 1)
			denominator := termTF + r.K1*(1-r.B+r.B*dl/avgDL)
			if denominator > 0 {
				bm25Score += termIDF * numerator / denominator
			}
		}

		// 名称/签名匹配加分
		nameBoost := 0.0
		nameLower := strings.ToLower(res.Document.ID)
		sigLower := ""
		if sig, ok := res.Document.Metadata["signature"].(string); ok {
			sigLower = strings.ToLower(sig)
		}
		for _, term := range queryTerms {
			if strings.Contains(nameLower, term) {
				nameBoost += 2.0
			}
			if sigLower != "" && strings.Contains(sigLower, term) {
				nameBoost += 1.5
			}
		}
		bm25Score += nameBoost

		// 归一化 BM25 分数到 0-1
		normalizedBM25 := float32(math.Min(bm25Score/10.0, 1.0))

		// 混合分数
		finalScore := float32(r.VectorWeight)*res.Score + float32(r.BM25Weight)*normalizedBM25

		scored[i] = scoredResult{
			result:     res,
			finalScore: finalScore,
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].finalScore > scored[j].finalScore
	})

	reranked := make([]SearchResult, len(scored))
	for i, s := range scored {
		s.result.Score = s.finalScore
		reranked[i] = s.result
	}

	return reranked
}

// RRFReranker Reciprocal Rank Fusion 重排序器
// 融合多个排序列表的结果
type RRFReranker struct {
	// K RRF 常数，典型值 60
	K int
}

// NewRRFReranker 创建 RRF 重排序器
func NewRRFReranker() *RRFReranker {
	return &RRFReranker{K: 60}
}

// Fuse 融合多个搜索结果列表
func (r *RRFReranker) Fuse(resultSets ...[]SearchResult) []SearchResult {
	scores := make(map[string]float32)
	docs := make(map[string]SearchResult)

	for _, results := range resultSets {
		for rank, res := range results {
			id := res.Document.ID
			scores[id] += 1.0 / float32(rank+r.K)
			if _, exists := docs[id]; !exists {
				docs[id] = res
			}
		}
	}

	type idScore struct {
		id    string
		score float32
	}
	var pairs []idScore
	for id, score := range scores {
		pairs = append(pairs, idScore{id, score})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	fused := make([]SearchResult, 0, len(pairs))
	for _, p := range pairs {
		res := docs[p.id]
		res.Score = p.score
		fused = append(fused, res)
	}

	return fused
}

// CodeAwareReranker 代码感知重排序器
// 对代码搜索结果进行语义增强重排序
type CodeAwareReranker struct {
	bm25 *BM25Reranker
}

// NewCodeAwareReranker 创建代码感知重排序器
func NewCodeAwareReranker() *CodeAwareReranker {
	return &CodeAwareReranker{
		bm25: NewBM25Reranker(),
	}
}

// Rerank 代码感知重排序
func (r *CodeAwareReranker) Rerank(query string, results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	// 第一步：BM25 混合重排序
	reranked := r.bm25.Rerank(query, results)

	// 第二步：实体类型偏好加分
	queryLower := strings.ToLower(query)
	for i := range reranked {
		boost := float32(0.0)
		entityType, _ := reranked[i].Document.Metadata["type"].(string)

		// 查询包含 "函数"/"function"/"func" 时，函数类型加分
		if containsAny(queryLower, []string{"函数", "function", "func", "方法", "method"}) {
			if entityType == "function" || entityType == "method" {
				boost += 0.05
			}
		}
		if containsAny(queryLower, []string{"类", "class", "struct", "结构"}) {
			if entityType == "class" {
				boost += 0.05
			}
		}

		// 文档注释包含查询关键词加分
		if doc, ok := reranked[i].Document.Metadata["doc_comment"].(string); ok {
			queryTerms := tokenize(query)
			docLower := strings.ToLower(doc)
			for _, term := range queryTerms {
				if strings.Contains(docLower, term) {
					boost += 0.02
				}
			}
		}

		reranked[i].Score += boost
	}

	// 重新排序
	sort.Slice(reranked, func(i, j int) bool {
		return reranked[i].Score > reranked[j].Score
	})

	return reranked
}

// tokenize 简单分词：按非字母数字字符分割并转小写
func tokenize(text string) []string {
	text = strings.ToLower(text)
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	// 过滤太短的 token
	var result []string
	for _, t := range tokens {
		if len(t) >= 2 {
			result = append(result, t)
		}
	}
	return result
}

// containsAny 检查 s 是否包含 substrs 中的任意一个
func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
