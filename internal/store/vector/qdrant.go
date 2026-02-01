// Package vector 提供向量存储的 Qdrant 实现
// 对应架构文档: VectorDB - Qdrant
//
// Qdrant 是一个高性能的向量数据库，支持:
// - 高效的向量相似度搜索
// - 元数据过滤
// - 实时更新
// - 分布式部署
package vector

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// QdrantStore 是 Qdrant 向量数据库的实现
// Qdrant 官方 Go SDK: github.com/qdrant/go-client
type QdrantStore struct {
	// client Qdrant 客户端
	client *qdrant.Client

	// collectionName 集合名称（类似数据库表）
	collectionName string

	// vectorDim 向量维度（由嵌入模型决定）
	vectorDim uint64
}

// QdrantConfig Qdrant 连接配置
type QdrantConfig struct {
	// Host Qdrant 服务地址
	Host string

	// Port gRPC 端口（默认 6334）
	Port int

	// CollectionName 集合名称
	CollectionName string

	// VectorDim 向量维度
	VectorDim uint64

	// APIKey 可选的 API 密钥
	APIKey string
}

// NewQdrantStore 创建 Qdrant 存储实例
// 参数:
//   - cfg: Qdrant 配置
//
// 返回:
//   - *QdrantStore: 存储实例
//   - error: 连接错误
//
// 使用示例:
//
//	store, err := NewQdrantStore(QdrantConfig{
//	    Host: "localhost",
//	    Port: 6334,
//	    CollectionName: "code_vectors",
//	    VectorDim: 384,
//	})
func NewQdrantStore(cfg QdrantConfig) (*QdrantStore, error) {
	// 连接 Qdrant 服务
	// Qdrant Go SDK 使用 gRPC 协议进行通信
	client, err := qdrant.NewClient(&qdrant.Config{
		Host:   cfg.Host,
		Port:   cfg.Port,
		APIKey: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 Qdrant 失败: %w", err)
	}

	store := &QdrantStore{
		client:         client,
		collectionName: cfg.CollectionName,
		vectorDim:      cfg.VectorDim,
	}

	// 确保集合存在
	if err := store.ensureCollection(context.Background()); err != nil {
		client.Close()
		return nil, err
	}

	return store, nil
}

// ensureCollection 确保集合存在，不存在则创建
// Qdrant 集合相当于关系数据库中的表
func (s *QdrantStore) ensureCollection(ctx context.Context) error {
	// 检查集合是否存在
	exists, err := s.client.CollectionExists(ctx, s.collectionName)
	if err != nil {
		return fmt.Errorf("检查集合失败: %w", err)
	}

	if exists {
		return nil
	}

	// 创建集合
	// VectorParams 指定向量配置:
	// - Size: 向量维度
	// - Distance: 距离度量方式（Cosine 余弦相似度最常用于文本嵌入）
	err = s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: s.collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     s.vectorDim,
			Distance: qdrant.Distance_Cosine, // 余弦相似度
		}),
	})
	if err != nil {
		return fmt.Errorf("创建集合失败: %w", err)
	}

	return nil
}

// Upsert 插入或更新文档
// 将文档转换为 Qdrant 的 Point 格式并批量上传
func (s *QdrantStore) Upsert(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	// 将 Document 转换为 Qdrant Point
	// Point 是 Qdrant 中的基本数据单位，包含:
	// - Id: 唯一标识符
	// - Vectors: 向量数据
	// - Payload: 元数据（用于过滤）
	points := make([]*qdrant.PointStruct, len(docs))
	for i, doc := range docs {
		// 构建 Payload（元数据）
		payload := make(map[string]*qdrant.Value)
		payload["content"] = qdrant.NewValueString(doc.Content)
		for k, v := range doc.Metadata {
			switch val := v.(type) {
			case string:
				payload[k] = qdrant.NewValueString(val)
			case int:
				payload[k] = qdrant.NewValueInt(int64(val))
			case int64:
				payload[k] = qdrant.NewValueInt(val)
			case float64:
				payload[k] = qdrant.NewValueDouble(val)
			case bool:
				payload[k] = qdrant.NewValueBool(val)
			}
		}

		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewIDUUID(doc.ID),
			Vectors: qdrant.NewVectors(doc.Vector...),
			Payload: payload,
		}
	}

	// 批量插入
	// Wait: true 表示等待操作完成再返回
	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.collectionName,
		Points:         points,
		Wait:           qdrant.PtrOf(true),
	})
	if err != nil {
		return fmt.Errorf("插入文档失败: %w", err)
	}

	return nil
}

// Search 语义搜索
// 使用向量相似度找到最匹配的文档
func (s *QdrantStore) Search(ctx context.Context, queryVector []float32, opts SearchOptions) ([]SearchResult, error) {
	// 设置默认值
	if opts.TopK <= 0 {
		opts.TopK = DefaultTopK
	}

	// 构建搜索请求
	searchReq := &qdrant.SearchPoints{
		CollectionName: s.collectionName,
		Vector:         queryVector,
		Limit:          uint64(opts.TopK),
		WithPayload:    qdrant.NewWithPayload(true), // 返回 payload
	}

	// 添加过滤条件
	if len(opts.Filter) > 0 {
		conditions := make([]*qdrant.Condition, 0, len(opts.Filter))
		for k, v := range opts.Filter {
			switch val := v.(type) {
			case string:
				conditions = append(conditions, qdrant.NewMatch(k, val))
			}
		}
		if len(conditions) > 0 {
			searchReq.Filter = &qdrant.Filter{
				Must: conditions,
			}
		}
	}

	// 执行搜索
	results, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.collectionName,
		Query:          qdrant.NewQuery(queryVector...),
		Limit:          qdrant.PtrOf(uint64(opts.TopK)),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	// 转换结果
	searchResults := make([]SearchResult, 0, len(results))
	for _, r := range results {
		// 过滤低分结果
		if opts.MinScore > 0 && r.Score < opts.MinScore {
			continue
		}

		doc := Document{
			ID:       r.Id.GetUuid(),
			Metadata: make(map[string]interface{}),
		}

		// 提取 payload
		for k, v := range r.Payload {
			if k == "content" {
				doc.Content = v.GetStringValue()
			} else {
				switch v.Kind.(type) {
				case *qdrant.Value_StringValue:
					doc.Metadata[k] = v.GetStringValue()
				case *qdrant.Value_IntegerValue:
					doc.Metadata[k] = v.GetIntegerValue()
				case *qdrant.Value_DoubleValue:
					doc.Metadata[k] = v.GetDoubleValue()
				case *qdrant.Value_BoolValue:
					doc.Metadata[k] = v.GetBoolValue()
				}
			}
		}

		searchResults = append(searchResults, SearchResult{
			Document: doc,
			Score:    r.Score,
		})
	}

	return searchResults, nil
}

// Delete 删除文档
func (s *QdrantStore) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// 构建 ID 列表
	pointIds := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		pointIds[i] = qdrant.NewIDUUID(id)
	}

	// 执行删除
	_, err := s.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: s.collectionName,
		Points:         qdrant.NewPointsSelector(pointIds...),
		Wait:           qdrant.PtrOf(true),
	})
	if err != nil {
		return fmt.Errorf("删除文档失败: %w", err)
	}

	return nil
}

// Count 返回文档数量
func (s *QdrantStore) Count(ctx context.Context) (int64, error) {
	info, err := s.client.GetCollectionInfo(ctx, s.collectionName)
	if err != nil {
		return 0, fmt.Errorf("获取集合信息失败: %w", err)
	}

	if info.PointsCount != nil {
		return int64(*info.PointsCount), nil
	}
	return 0, nil
}

// Close 关闭连接
func (s *QdrantStore) Close() error {
	return s.client.Close()
}
