// Package vector 提供文本嵌入（Embedding）接口和实现
// 对应架构文档: Embedder (HuggingFace, OpenAI, BERT)
//
// Embedder 将文本转换为固定维度的向量表示（嵌入向量）
// 这些向量可以用于语义相似度计算
//
// 工作原理:
// 1. 输入: 代码文本（如函数定义、注释等）
// 2. 通过神经网络模型处理
// 3. 输出: 固定长度的浮点向量（如 384 维）
//
// 语义相似的文本会产生相近的向量
// 例如: "计算两数之和" 和 "求和函数" 的向量会很接近
package vector

import (
	"context"
)

// Embedder 文本嵌入接口
// 将文本转换为向量表示
type Embedder interface {
	// Embed 将单个文本转换为向量
	// 参数:
	//   - ctx: 上下文（用于超时控制）
	//   - text: 要嵌入的文本
	// 返回:
	//   - []float32: 嵌入向量
	//   - error: 嵌入失败时的错误
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch 批量嵌入文本
	// 批量处理通常比单个处理更高效
	// 参数:
	//   - ctx: 上下文
	//   - texts: 要嵌入的文本列表
	// 返回:
	//   - [][]float32: 嵌入向量列表（顺序与输入对应）
	//   - error: 嵌入失败时的错误
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension 返回嵌入向量的维度
	// 不同模型有不同的维度:
	// - MiniLM: 384
	// - text-embedding-ada-002 (OpenAI): 1536
	// - BERT-base: 768
	Dimension() int
}
