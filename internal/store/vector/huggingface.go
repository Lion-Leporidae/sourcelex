// Package vector 提供 HuggingFace Inference API 嵌入器实现
// 对应架构文档: Embedder - HuggingFace
//
// 使用 HuggingFace 的免费推理 API 进行代码嵌入。
// 推荐模型:
// - sentence-transformers/all-MiniLM-L6-v2 (通用, 384维)
// - microsoft/codebert-base (代码专用)
// - Salesforce/codet5-small (代码T5)
// - BAAI/bge-small-en-v1.5 (高效嵌入)
//
// API 端点: https://router.huggingface.co/hf-inference/models/{model_id}
// 需要: HuggingFace API Token (免费注册获取)
package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HuggingFaceEmbedder 使用 HuggingFace Inference API 的嵌入器
// 通过 HTTP 调用 HuggingFace 的模型服务
type HuggingFaceEmbedder struct {
	// apiToken HuggingFace API 令牌
	// 在 huggingface.co/settings/tokens 获取
	apiToken string

	// modelID 模型 ID
	// 推荐: sentence-transformers/all-MiniLM-L6-v2
	modelID string

	// baseURL API 基础 URL
	baseURL string

	// client HTTP 客户端
	client *http.Client

	// dimension 向量维度（由模型决定）
	dimension int
}

// HuggingFaceConfig HuggingFace 嵌入器配置
type HuggingFaceConfig struct {
	// APIToken HuggingFace API 令牌（必填）
	// 注册 huggingface.co 后在设置中获取
	APIToken string

	// ModelID 嵌入模型 ID（可选，默认 all-MiniLM-L6-v2）
	// 常用模型:
	// - sentence-transformers/all-MiniLM-L6-v2 (384维，通用)
	// - BAAI/bge-small-en-v1.5 (384维，高效)
	// - microsoft/codebert-base (768维，代码专用)
	ModelID string

	// Dimension 向量维度（必须与模型匹配）
	Dimension int

	// Timeout 请求超时时间
	Timeout time.Duration
}

// NewHuggingFaceEmbedder 创建 HuggingFace 嵌入器
// 参数:
//   - cfg: 配置（必须包含 APIToken）
//
// 返回:
//   - *HuggingFaceEmbedder: 嵌入器实例
//   - error: 配置错误
//
// 使用示例:
//
//	embedder, err := NewHuggingFaceEmbedder(HuggingFaceConfig{
//	    APIToken:  "hf_xxxxxxxxxxxxxxxx",
//	    ModelID:   "sentence-transformers/all-MiniLM-L6-v2",
//	    Dimension: 384,
//	})
func NewHuggingFaceEmbedder(cfg HuggingFaceConfig) (*HuggingFaceEmbedder, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("HuggingFace API Token 不能为空")
	}

	// 设置默认值
	if cfg.ModelID == "" {
		cfg.ModelID = "sentence-transformers/all-MiniLM-L6-v2"
	}
	if cfg.Dimension == 0 {
		cfg.Dimension = 384 // all-MiniLM-L6-v2 的维度
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &HuggingFaceEmbedder{
		apiToken:  cfg.APIToken,
		modelID:   cfg.ModelID,
		baseURL:   "https://router.huggingface.co/hf-inference/models",
		client:    &http.Client{Timeout: cfg.Timeout},
		dimension: cfg.Dimension,
	}, nil
}

// Embed 将单个文本转换为嵌入向量
// 调用 HuggingFace Inference API 的 feature-extraction 任务
//
// API 请求格式:
// POST /models/{model_id}
// Body: {"inputs": "text to embed"}
//
// API 响应格式（对于 sentence-transformers 模型）:
// [[0.123, 0.456, ...]] (二维数组，每个输入一个向量)
func (e *HuggingFaceEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// 构建请求 URL - 使用 feature-extraction pipeline
	url := fmt.Sprintf("%s/%s/pipeline/feature-extraction", e.baseURL, e.modelID)

	// 构建请求体
	reqBody := map[string]interface{}{
		"inputs": text,
		// 可选参数
		"options": map[string]interface{}{
			"wait_for_model": true, // 等待模型加载
		},
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Authorization", "Bearer "+e.apiToken)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		// 解析错误响应
		var errorResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errorResp) == nil && errorResp.Error != "" {
			return nil, fmt.Errorf("API 错误 (%d): %s", resp.StatusCode, errorResp.Error)
		}
		return nil, fmt.Errorf("API 请求失败: %d - %s", resp.StatusCode, string(body))
	}

	// 解析响应
	// sentence-transformers 模型返回 [[float...]] 格式
	// 先尝试解析为二维数组
	var embeddings2D [][]float32
	if err := json.Unmarshal(body, &embeddings2D); err == nil && len(embeddings2D) > 0 {
		return embeddings2D[0], nil
	}

	// 某些模型可能返回一维数组
	var embedding1D []float32
	if err := json.Unmarshal(body, &embedding1D); err == nil && len(embedding1D) > 0 {
		return embedding1D, nil
	}

	// 某些模型返回三维数组 (token级别嵌入)
	// 需要对所有token取平均
	var embeddings3D [][][]float32
	if err := json.Unmarshal(body, &embeddings3D); err == nil && len(embeddings3D) > 0 && len(embeddings3D[0]) > 0 {
		// 对所有 token 的嵌入取平均（mean pooling）
		return meanPooling(embeddings3D[0]), nil
	}

	return nil, fmt.Errorf("无法解析响应: %s", string(body))
}

// meanPooling 对多个向量取平均
// 用于将 token 级别的嵌入聚合为句子级别
func meanPooling(vectors [][]float32) []float32 {
	if len(vectors) == 0 {
		return nil
	}

	dim := len(vectors[0])
	result := make([]float32, dim)

	for _, vec := range vectors {
		for i, v := range vec {
			result[i] += v
		}
	}

	n := float32(len(vectors))
	for i := range result {
		result[i] /= n
	}

	return result
}

// EmbedBatch 批量嵌入文本
// HuggingFace API 支持批量输入，更高效
func (e *HuggingFaceEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// 构建请求（使用与 Embed 相同的 feature-extraction pipeline 端点）
	url := fmt.Sprintf("%s/%s/pipeline/feature-extraction", e.baseURL, e.modelID)
	reqBody := map[string]interface{}{
		"inputs": texts,
		"options": map[string]interface{}{
			"wait_for_model": true,
		},
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errorResp) == nil && errorResp.Error != "" {
			return nil, fmt.Errorf("API 错误 (%d): %s", resp.StatusCode, errorResp.Error)
		}
		return nil, fmt.Errorf("API 请求失败: %d - %s", resp.StatusCode, string(body))
	}

	// 解析响应 - 批量请求返回二维数组
	var embeddings [][]float32
	if err := json.Unmarshal(body, &embeddings); err == nil && len(embeddings) > 0 {
		return embeddings, nil
	}

	// 某些模型返回三维数组（每个输入返回 token 级别嵌入）
	var embeddings3D [][][]float32
	if err := json.Unmarshal(body, &embeddings3D); err == nil && len(embeddings3D) > 0 {
		results := make([][]float32, len(embeddings3D))
		for i, tokens := range embeddings3D {
			results[i] = meanPooling(tokens)
		}
		return results, nil
	}

	return nil, fmt.Errorf("无法解析批量响应")
}

// Dimension 返回向量维度
func (e *HuggingFaceEmbedder) Dimension() int {
	return e.dimension
}

// ModelID 返回当前使用的模型 ID
func (e *HuggingFaceEmbedder) ModelID() string {
	return e.modelID
}
