// Package vector 提供向量存储接口和实现
// 对应架构文档: VectorStore (向量存储)
//
// 向量存储用于存储代码块的嵌入向量，支持语义搜索。
// 主要功能:
// - 存储代码片段及其向量表示
// - 基于向量相似度的语义搜索
// - 支持元数据过滤
package vector

import (
	"context"
)

// Document 表示一个可被向量化的文档（代码块）
// 对应架构文档中的 VectorDataInfo
type Document struct {
	// ID 文档唯一标识符
	ID string `json:"id"`

	// Content 文档原始内容（代码文本）
	Content string `json:"content"`

	// Vector 文档的嵌入向量（由 Embedder 生成）
	Vector []float32 `json:"vector,omitempty"`

	// Metadata 元数据，用于过滤
	// 常见字段: file_path, entity_name, entity_type, language
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SearchResult 表示搜索结果
type SearchResult struct {
	// Document 匹配的文档
	Document Document `json:"document"`

	// Score 相似度分数（0-1，越高越相似）
	Score float32 `json:"score"`
}

// SearchOptions 搜索选项
type SearchOptions struct {
	// TopK 返回的结果数量
	TopK int

	// Filter 元数据过滤条件
	// 例如: {"language": "go", "entity_type": "function"}
	Filter map[string]interface{}

	// MinScore 最低相似度阈值（可选）
	MinScore float32
}

// Store 向量存储接口
// 定义了向量数据库的基本操作
// 实现可以是 Qdrant、Milvus、FAISS 等
type Store interface {
	// Upsert 插入或更新文档
	// 如果文档已存在（相同 ID），则更新
	Upsert(ctx context.Context, docs []Document) error

	// Search 语义搜索
	// 根据查询向量找到最相似的文档
	Search(ctx context.Context, queryVector []float32, opts SearchOptions) ([]SearchResult, error)

	// Delete 删除文档
	Delete(ctx context.Context, ids []string) error

	// Count 返回存储的文档数量
	Count(ctx context.Context) (int64, error)

	// Close 关闭连接
	Close() error
}

// DefaultTopK 默认返回结果数量
const DefaultTopK = 10

// DefaultMinScore 默认最低相似度
const DefaultMinScore = 0.5
