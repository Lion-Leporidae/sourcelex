// Package analyzer provides the main code analysis orchestrator.
// Corresponds to: REPOMIND_ARCHITECTURE_MINDMAP.md - 代码分析阶段 (CodeAnalyzer)
package analyzer

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/entity"
	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/parser"
	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/relation"
	"github.com/Lion-Leporidae/sourcelex/internal/logger"
)

// Analyzer is the main code analyzer that orchestrates parsing and entity extraction
type Analyzer struct {
	parser  *parser.Parser
	scanner *FileScanner
	log     *logger.Logger
	workers int
}

// AnalysisResult holds the complete analysis result for a repository
type AnalysisResult struct {
	RepoPath      string
	Entities      []entity.Entity
	Relations     []relation.CallRelation  // 调用关系
	APIEndpoints  []relation.APIEndpoint   // API 端点（路由注册）
	FileCount     int
	EntityCount   int
	RelationCount int
	NewFiles      int
	ModifiedFiles int
	SkippedFiles  int
}

// New creates a new Analyzer
func New(repoPath string, log *logger.Logger) *Analyzer {
	return &Analyzer{
		parser:  parser.New(),
		scanner: NewFileScanner(repoPath, log),
		log:     log,
		workers: 4, // 默认 4 个 worker
	}
}

// SetWorkers sets the number of concurrent workers
func (a *Analyzer) SetWorkers(n int) {
	if n > 0 {
		a.workers = n
	}
}

// BuildIndex analyzes the repository and extracts all entities
// Implements: REPOMIND_ARCHITECTURE_MINDMAP.md - build_index() 主入口
func (a *Analyzer) BuildIndex(ctx context.Context) (*AnalysisResult, error) {
	a.log.Info("开始构建索引")

	// 步骤1-2: 扫描文件并检测增量更新
	scanResult, err := a.scanner.Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("扫描文件失败: %w", err)
	}

	a.log.Info("文件扫描完成",
		"total", scanResult.TotalFiles,
		"new", len(scanResult.NewFiles),
		"modified", len(scanResult.ModifiedFiles),
		"unchanged", len(scanResult.UnchangedFiles),
	)

	// 获取需要分析的文件
	filesToAnalyze := a.scanner.GetFilesToAnalyze(scanResult)
	if len(filesToAnalyze) == 0 {
		a.log.Info("没有需要分析的文件")
		return &AnalysisResult{
			RepoPath:     a.scanner.repoPath,
			FileCount:    scanResult.TotalFiles,
			SkippedFiles: len(scanResult.UnchangedFiles),
		}, nil
	}

	// 步骤3-7: 第一遍解析：并行提取实体
	entities, err := a.analyzeFiles(ctx, filesToAnalyze)
	if err != nil {
		return nil, err
	}

	// 步骤8: 第二遍解析：提取 import + 调用关系 + API 端点（需要第一遍的实体构建符号表）
	relations, apiEndpoints := a.extractRelationsAndAPIs(ctx, filesToAnalyze, entities)

	a.log.Info("索引构建完成",
		"entities", len(entities),
		"relations", len(relations),
		"api_endpoints", len(apiEndpoints),
		"files_analyzed", len(filesToAnalyze),
	)

	return &AnalysisResult{
		RepoPath:      a.scanner.repoPath,
		Entities:      entities,
		Relations:     relations,
		APIEndpoints:  apiEndpoints,
		FileCount:     scanResult.TotalFiles,
		EntityCount:   len(entities),
		RelationCount: len(relations),
		NewFiles:      len(scanResult.NewFiles),
		ModifiedFiles: len(scanResult.ModifiedFiles),
		SkippedFiles:  len(scanResult.UnchangedFiles),
	}, nil
}

// analyzeFiles analyzes files concurrently using worker pool
func (a *Analyzer) analyzeFiles(ctx context.Context, files []string) ([]entity.Entity, error) {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		entities []entity.Entity
	)

	// 创建任务通道
	jobs := make(chan string, len(files))
	for _, f := range files {
		jobs <- f
	}
	close(jobs)

	// 启动 worker
	for i := 0; i < a.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for relPath := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				absPath := filepath.Join(a.scanner.repoPath, relPath)
				lang := GetLanguage(relPath)
				if lang == "" {
					continue
				}

				// 解析文件 (Tree-sitter)
				result, err := a.parser.ParseFile(ctx, absPath, lang)
				if err != nil {
					a.log.Debug("解析文件失败", "file", relPath, "error", err)
					continue
				}

				// 提取实体
				extractor := entity.NewExtractor(result.Content, relPath, lang)
				fileEntities := extractor.Extract(result.Tree)
				result.Tree.Close()

				mu.Lock()
				entities = append(entities, fileEntities...)
				mu.Unlock()

				a.log.Debug("已分析文件", "file", relPath, "entities", len(fileEntities))
			}
		}()
	}

	wg.Wait()
	return entities, nil
}

// extractRelationsAndAPIs 提取调用关系和 API 端点
// 先构建符号表 + 提取 import 信息，然后遍历文件提取调用关系和 API 路由注册
func (a *Analyzer) extractRelationsAndAPIs(ctx context.Context, files []string, entities []entity.Entity) ([]relation.CallRelation, []relation.APIEndpoint) {
	// 1. 从实体构建符号表
	symbolTable := relation.BuildSymbolTableFromEntities(entities)
	a.log.Debug("符号表构建完成", "symbols", symbolTable.Size())

	// 2. 第一轮：提取所有文件的 import 信息，填充符号表
	for _, relPath := range files {
		absPath := filepath.Join(a.scanner.repoPath, relPath)
		lang := GetLanguage(relPath)
		if lang == "" {
			continue
		}
		result, err := a.parser.ParseFile(ctx, absPath, lang)
		if err != nil {
			continue
		}
		importExtractor := relation.NewExtractor(result.Content, relPath, lang, symbolTable)
		importExtractor.ExtractImports(result.Tree)
		result.Tree.Close()
	}
	a.log.Debug("import 信息提取完成")

	// 3. 第二轮：并行提取调用关系 + API 端点（符号表已包含 import 信息，解析更准确）
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		relations    []relation.CallRelation
		apiEndpoints []relation.APIEndpoint
	)

	jobs := make(chan string, len(files))
	for _, f := range files {
		jobs <- f
	}
	close(jobs)

	for i := 0; i < a.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for relPath := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				absPath := filepath.Join(a.scanner.repoPath, relPath)
				lang := GetLanguage(relPath)
				if lang == "" {
					continue
				}

				result, err := a.parser.ParseFile(ctx, absPath, lang)
				if err != nil {
					continue
				}

				extractor := relation.NewExtractor(result.Content, relPath, lang, symbolTable)

				// 提取调用关系
				fileRelations := extractor.Extract(result.Tree)

				// 提取 API 端点（复用同一个 extractor，不需要重新解析）
				fileEndpoints := extractor.ExtractAPIEndpoints(result.Tree)

				result.Tree.Close()

				mu.Lock()
				if len(fileRelations) > 0 {
					relations = append(relations, fileRelations...)
					a.log.Debug("提取调用关系", "file", relPath, "relations", len(fileRelations))
				}
				if len(fileEndpoints) > 0 {
					apiEndpoints = append(apiEndpoints, fileEndpoints...)
					a.log.Info("发现 API 端点", "file", relPath, "endpoints", len(fileEndpoints))
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	a.log.Info("调用关系提取完成", "total_relations", len(relations), "total_api_endpoints", len(apiEndpoints))
	return relations, apiEndpoints
}
