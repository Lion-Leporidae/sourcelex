// Package chunker 提供基于符号边界的代码分块功能
// 对应架构文档: RepoMap 代码分块
//
// 核心思想:
// - 每个代码符号（函数/类/方法）作为一个分块
// - 分块包含完整的代码内容，而非仅签名
// - 用于向量化存储，支持精准的语义搜索
package chunker

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/entity"
)

// CodeChunk 表示基于符号边界的代码分块
// 每个分块对应一个完整的代码符号（函数/类/方法）
type CodeChunk struct {
	// ID 分块唯一标识 (格式: {file_path}::{qualified_name})
	ID string `json:"id"`

	// Entity 对应的实体信息
	Entity *entity.Entity `json:"entity"`

	// Content 分块的完整代码内容
	// 从 Entity.StartLine 到 Entity.EndLine 的源代码
	Content string `json:"content"`

	// Signature 符号签名（用于快速预览）
	Signature string `json:"signature"`

	// LineCount 代码行数
	LineCount int `json:"line_count"`

	// ChunkIndex 分块索引（大代码块被切分时的编号，从 0 开始）
	ChunkIndex int `json:"chunk_index"`

	// TotalChunks 总分块数（1 表示未被切分）
	TotalChunks int `json:"total_chunks"`

	// FileContext 文件上下文（package/import 声明）
	FileContext string `json:"file_context,omitempty"`

	// IsTruncated 是否因超过最大长度被截断
	IsTruncated bool `json:"is_truncated,omitempty"`
}

// ChunkOptions 分块选项
type ChunkOptions struct {
	// MaxChunkSize 最大分块大小（字符数）
	// 超过此大小的符号会在元数据中标记
	MaxChunkSize int

	// ChunkOverlap 分块重叠大小（行数）
	// 切分大代码块时，相邻分块之间重叠的行数
	ChunkOverlap int

	// IncludeContext 是否包含上下文（导入语句等）
	IncludeContext bool

	// RepoPath 仓库根路径（用于计算相对路径）
	RepoPath string
}

// DefaultChunkOptions 默认分块选项
var DefaultChunkOptions = ChunkOptions{
	MaxChunkSize:   4096,
	ChunkOverlap:   5,
	IncludeContext: true,
	RepoPath:       "",
}

// maxFileCacheSize 最大文件缓存数量，防止大仓库时内存无限增长
const maxFileCacheSize = 500

// Chunker 符号分块器接口
type Chunker interface {
	// ChunkEntities 将实体列表转换为代码分块
	ChunkEntities(ctx context.Context, entities []entity.Entity, opts ChunkOptions) ([]CodeChunk, error)
}

// SymbolChunker 基于 Tree-sitter 符号的分块器
// 将代码实体转换为可向量化的分块
type SymbolChunker struct {
	// fileCache 文件内容缓存（避免重复读取）
	// key: absolute file path
	// value: file lines ([]string)
	fileCache map[string][]string
	mu        sync.RWMutex
}

// NewSymbolChunker 创建符号分块器
func NewSymbolChunker() *SymbolChunker {
	return &SymbolChunker{
		fileCache: make(map[string][]string),
	}
}

// ChunkEntities 将实体列表转换为代码分块
// 主要步骤:
// 1. 按文件分组实体
// 2. 读取文件内容
// 3. 根据实体行号提取代码
// 4. 构建 CodeChunk
func (c *SymbolChunker) ChunkEntities(ctx context.Context, entities []entity.Entity, opts ChunkOptions) ([]CodeChunk, error) {
	if len(entities) == 0 {
		return nil, nil
	}

	chunks := make([]CodeChunk, 0, len(entities))

	// 按文件分组实体，减少文件 IO
	fileEntities := make(map[string][]entity.Entity)
	for i := range entities {
		e := &entities[i]
		filePath := e.FilePath
		if opts.RepoPath != "" {
			filePath = filepath.Join(opts.RepoPath, e.FilePath)
		}
		fileEntities[filePath] = append(fileEntities[filePath], *e)
	}

	// 处理每个文件
	for filePath, ents := range fileEntities {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 读取文件内容（带缓存）
		lines, err := c.readFileLines(filePath)
		if err != nil {
			// 文件读取失败，跳过该文件的所有实体
			continue
		}

		// 为每个实体创建分块（支持大代码块切分）
		for i := range ents {
			e := &ents[i]
			chunk := c.createChunk(e, lines, opts)
			if chunk != nil {
				subChunks := c.splitLargeChunk(chunk, lines, opts)
				chunks = append(chunks, subChunks...)
			}
		}
	}

	return chunks, nil
}

// createChunk 为单个实体创建代码分块
// 如果代码超过 MaxChunkSize，返回 nil（由 createChunks 处理切分）
func (c *SymbolChunker) createChunk(e *entity.Entity, lines []string, opts ChunkOptions) *CodeChunk {
	content := c.extractCode(lines, int(e.StartLine), int(e.EndLine))
	if content == "" {
		return nil
	}

	id := fmt.Sprintf("%s::%s", e.FilePath, e.QualifiedName)
	lineCount := int(e.EndLine) - int(e.StartLine) + 1

	fileContext := ""
	if opts.IncludeContext {
		fileContext = c.extractFileContext(lines)
	}

	return &CodeChunk{
		ID:          id,
		Entity:      e,
		Content:     content,
		Signature:   e.Signature,
		LineCount:   lineCount,
		ChunkIndex:  0,
		TotalChunks: 1,
		FileContext: fileContext,
	}
}

// splitLargeChunk 将超过最大大小的分块切分为多个重叠的子分块
func (c *SymbolChunker) splitLargeChunk(chunk *CodeChunk, lines []string, opts ChunkOptions) []CodeChunk {
	if opts.MaxChunkSize <= 0 || len(chunk.Content) <= opts.MaxChunkSize {
		return []CodeChunk{*chunk}
	}

	startLine := int(chunk.Entity.StartLine)
	endLine := int(chunk.Entity.EndLine)
	overlap := opts.ChunkOverlap
	if overlap <= 0 {
		overlap = 5
	}

	// 按行估算每分块的行数
	totalLines := endLine - startLine + 1
	avgCharPerLine := len(chunk.Content) / totalLines
	if avgCharPerLine == 0 {
		avgCharPerLine = 40
	}
	linesPerChunk := opts.MaxChunkSize / avgCharPerLine
	if linesPerChunk < 10 {
		linesPerChunk = 10
	}

	var subChunks []CodeChunk
	chunkStart := startLine

	for chunkStart <= endLine {
		chunkEnd := chunkStart + linesPerChunk - 1
		if chunkEnd > endLine {
			chunkEnd = endLine
		}

		content := c.extractCode(lines, chunkStart, chunkEnd)
		if content == "" {
			break
		}

		subChunk := CodeChunk{
			ID:          fmt.Sprintf("%s::%s#%d", chunk.Entity.FilePath, chunk.Entity.QualifiedName, len(subChunks)),
			Entity:      chunk.Entity,
			Content:     content,
			Signature:   chunk.Signature,
			LineCount:   chunkEnd - chunkStart + 1,
			ChunkIndex:  len(subChunks),
			FileContext: chunk.FileContext,
			IsTruncated: chunkEnd < endLine,
		}
		subChunks = append(subChunks, subChunk)

		// 下一个分块的起始位置（减去重叠行数）
		chunkStart = chunkEnd + 1 - overlap
		if chunkStart <= chunkEnd-linesPerChunk+1 {
			chunkStart = chunkEnd + 1
		}
	}

	// 设置总分块数
	for i := range subChunks {
		subChunks[i].TotalChunks = len(subChunks)
	}

	return subChunks
}

// extractCode 从文件行中提取指定行范围的代码
// startLine 和 endLine 都是 1-indexed
func (c *SymbolChunker) extractCode(lines []string, startLine, endLine int) string {
	if startLine < 1 || endLine < startLine {
		return ""
	}

	// 转换为 0-indexed
	start := startLine - 1
	end := endLine

	if start >= len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}

	// 提取行并拼接
	selectedLines := lines[start:end]
	return strings.Join(selectedLines, "\n")
}

// readFileLines 读取文件内容为行列表（带缓存）
func (c *SymbolChunker) readFileLines(filePath string) ([]string, error) {
	// 先检查缓存
	c.mu.RLock()
	if lines, ok := c.fileCache[filePath]; ok {
		c.mu.RUnlock()
		return lines, nil
	}
	c.mu.RUnlock()

	// 读取文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	// 增大缓冲区以支持超长行
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 存入缓存（限制缓存大小）
	c.mu.Lock()
	if len(c.fileCache) >= maxFileCacheSize {
		// 简单淘汰策略：缓存满时清空一半
		count := 0
		for k := range c.fileCache {
			if count >= maxFileCacheSize/2 {
				break
			}
			delete(c.fileCache, k)
			count++
		}
	}
	c.fileCache[filePath] = lines
	c.mu.Unlock()

	return lines, nil
}

// ClearCache 清空文件缓存
func (c *SymbolChunker) ClearCache() {
	c.mu.Lock()
	c.fileCache = make(map[string][]string)
	c.mu.Unlock()
}

// extractFileContext 提取文件头部上下文（package 声明和 import 语句）
func (c *SymbolChunker) extractFileContext(lines []string) string {
	var contextLines []string
	inImportBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "package ") ||
			strings.HasPrefix(trimmed, "# ") ||
			strings.HasPrefix(trimmed, "from ") ||
			strings.HasPrefix(trimmed, "import ") {
			contextLines = append(contextLines, line)
			if strings.HasPrefix(trimmed, "import (") {
				inImportBlock = true
			}
			continue
		}

		if inImportBlock {
			contextLines = append(contextLines, line)
			if trimmed == ")" {
				inImportBlock = false
			}
			continue
		}

		// 停止在第一个非空、非注释、非 import 行
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "/*") && !strings.HasPrefix(trimmed, "*") {
			break
		}
	}

	if len(contextLines) == 0 {
		return ""
	}
	return strings.Join(contextLines, "\n")
}

// BuildEmbeddingContent 构建用于嵌入的文本
// 组合多种信息以增强语义理解
func BuildEmbeddingContent(chunk *CodeChunk) string {
	var parts []string

	// 1. 文件上下文（package/import，帮助理解代码所属模块）
	if chunk.FileContext != "" {
		parts = append(parts, chunk.FileContext)
	}

	// 2. 符号类型和名称（增强类型识别）
	typeName := string(chunk.Entity.Type)
	header := fmt.Sprintf("[%s] %s", typeName, chunk.Entity.QualifiedName)
	if chunk.TotalChunks > 1 {
		header += fmt.Sprintf(" (part %d/%d)", chunk.ChunkIndex+1, chunk.TotalChunks)
	}
	parts = append(parts, header)

	// 3. 文件路径（增强文件关联）
	parts = append(parts, fmt.Sprintf("File: %s", chunk.Entity.FilePath))

	// 4. 签名（快速理解函数作用）
	if chunk.Signature != "" {
		parts = append(parts, chunk.Signature)
	}

	// 5. 文档注释（自然语言描述）
	if chunk.Entity.DocComment != "" {
		doc := strings.Trim(chunk.Entity.DocComment, "\"'`")
		doc = strings.TrimSpace(doc)
		if doc != "" {
			parts = append(parts, doc)
		}
	}

	// 6. 代码内容
	parts = append(parts, chunk.Content)

	return strings.Join(parts, "\n\n")
}
