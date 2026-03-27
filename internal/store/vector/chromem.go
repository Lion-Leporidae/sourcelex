// Package vector 提供 chromem-go 向量存储实现
// chromem-go 是一个纯 Go 的嵌入式向量数据库
// 特点:
// - 无外部依赖（无需 Docker/Qdrant）
// - 数据持久化到本地文件
// - 支持语义搜索
package vector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/philippgille/chromem-go"
)

// ChromemStore 是 chromem-go 向量数据库的实现
// 使用本地文件存储，便于调试和验证
type ChromemStore struct {
	// db chromem 数据库实例（持久化用）
	db *chromem.DB

	// memDB 内存数据库（索引写入期间使用，避免每次 AddDocuments 全量序列化）
	memDB *chromem.DB

	// collection 当前使用的集合（指向 memDB 或 db 的集合）
	collection *chromem.Collection

	// collectionName 集合名称
	collectionName string

	// persistPath 数据持久化路径
	persistPath string

	// dbPath 数据库文件路径
	dbPath string

	// vectorDim 向量维度
	vectorDim int

	// dirty 是否有未持久化的写入
	dirty bool
}

// ChromemConfig chromem 存储配置
type ChromemConfig struct {
	// PersistPath 数据持久化目录
	PersistPath string

	// CollectionName 集合名称
	CollectionName string

	// VectorDim 向量维度（用于验证）
	VectorDim int
}

// NewChromemStore 创建 chromem 向量存储实例
// 索引写入期间使用内存 DB（快速），Close 时一次性持久化到磁盘
func NewChromemStore(cfg ChromemConfig) (*ChromemStore, error) {
	// 确保持久化目录存在
	if err := os.MkdirAll(cfg.PersistPath, 0755); err != nil {
		return nil, fmt.Errorf("创建向量存储目录失败: %w", err)
	}

	dbPath := filepath.Join(cfg.PersistPath, "chromem.db")

	// 检查是否有已存在的持久化数据
	var db *chromem.DB
	if _, err := os.Stat(dbPath); err == nil {
		// 已有数据，加载持久化 DB 用于查询
		db, err = chromem.NewPersistentDB(dbPath, false)
		if err != nil {
			return nil, fmt.Errorf("打开 chromem 数据库失败: %w", err)
		}

		collection, err := db.GetOrCreateCollection(
			cfg.CollectionName, nil, nil,
		)
		if err != nil {
			return nil, fmt.Errorf("获取集合失败: %w", err)
		}

		return &ChromemStore{
			db:             db,
			collection:     collection,
			collectionName: cfg.CollectionName,
			persistPath:    cfg.PersistPath,
			dbPath:         dbPath,
			vectorDim:      cfg.VectorDim,
		}, nil
	}

	// 没有已有数据：创建内存 DB 用于快速写入
	memDB := chromem.NewDB()
	collection, err := memDB.GetOrCreateCollection(
		cfg.CollectionName, nil, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("创建内存集合失败: %w", err)
	}

	return &ChromemStore{
		memDB:          memDB,
		collection:     collection,
		collectionName: cfg.CollectionName,
		persistPath:    cfg.PersistPath,
		dbPath:         dbPath,
		vectorDim:      cfg.VectorDim,
	}, nil
}

// Upsert 插入或更新文档
// 写入内存 DB（无磁盘 IO），Close 时一次性持久化
func (s *ChromemStore) Upsert(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	// 转换为 chromem 文档格式
	chromemDocs := make([]chromem.Document, len(docs))
	for i, doc := range docs {
		chromemDocs[i] = chromem.Document{
			ID:        doc.ID,
			Content:   doc.Content,
			Embedding: doc.Vector,
			Metadata:  s.convertMetadata(doc.Metadata),
		}
	}

	// 添加到集合（内存 DB 模式下无磁盘 IO，非常快）
	if err := s.collection.AddDocuments(ctx, chromemDocs, runtime.NumCPU()); err != nil {
		return fmt.Errorf("添加文档失败: %w", err)
	}

	s.dirty = true
	return nil
}

// convertMetadata 转换元数据格式
// chromem 要求元数据值为 string 类型
func (s *ChromemStore) convertMetadata(meta map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range meta {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

// Search 语义搜索
// 使用向量相似度找到最匹配的文档
func (s *ChromemStore) Search(ctx context.Context, queryVector []float32, opts SearchOptions) ([]SearchResult, error) {
	// 设置默认值
	if opts.TopK <= 0 {
		opts.TopK = DefaultTopK
	}

	// 执行查询
	// chromem.QueryEmbedding 直接使用向量进行搜索
	results, err := s.collection.QueryEmbedding(ctx, queryVector, opts.TopK, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	// 转换结果
	searchResults := make([]SearchResult, 0, len(results))
	for _, r := range results {
		// 过滤低分结果
		// chromem 返回的相似度分数范围是 0-1
		if opts.MinScore > 0 && r.Similarity < opts.MinScore {
			continue
		}

		doc := Document{
			ID:       r.ID,
			Content:  r.Content,
			Metadata: s.convertMetadataBack(r.Metadata),
		}

		searchResults = append(searchResults, SearchResult{
			Document: doc,
			Score:    r.Similarity,
		})
	}

	return searchResults, nil
}

// convertMetadataBack 将 string 元数据转回 interface{}
func (s *ChromemStore) convertMetadataBack(meta map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range meta {
		result[k] = v
	}
	return result
}

// Delete 删除文档
func (s *ChromemStore) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// chromem 目前不支持删除单个文档
	// 这是 chromem-go 的限制，需要删除整个集合后重建
	// TODO: 等待 chromem-go 支持删除功能
	return fmt.Errorf("chromem 暂不支持删除单个文档")
}

// Count 返回文档数量
func (s *ChromemStore) Count(ctx context.Context) (int64, error) {
	return int64(s.collection.Count()), nil
}

// Close 关闭连接并持久化数据
// 如果使用了内存 DB 且有写入，将数据导出到持久化 DB
func (s *ChromemStore) Close() error {
	if s.memDB != nil && s.dirty {
		// 将内存 DB 一次性导出到持久化文件
		if err := s.memDB.ExportToFile(s.dbPath, false, ""); err != nil {
			return fmt.Errorf("持久化向量数据失败: %w", err)
		}
	}

	return nil
}
