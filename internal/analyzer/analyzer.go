// Package analyzer provides the main code analysis orchestrator.
// Corresponds to: REPOMIND_ARCHITECTURE_MINDMAP.md - 代码分析阶段 (CodeAnalyzer)
package analyzer

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/repomind/repomind-go/internal/analyzer/entity"
	"github.com/repomind/repomind-go/internal/analyzer/parser"
	"github.com/repomind/repomind-go/internal/logger"
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
	FileCount     int
	EntityCount   int
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

	// 步骤3-7: 并行解析文件并提取实体
	entities, err := a.analyzeFiles(ctx, filesToAnalyze)
	if err != nil {
		return nil, err
	}

	a.log.Info("索引构建完成",
		"entities", len(entities),
		"files_analyzed", len(filesToAnalyze),
	)

	return &AnalysisResult{
		RepoPath:      a.scanner.repoPath,
		Entities:      entities,
		FileCount:     scanResult.TotalFiles,
		EntityCount:   len(entities),
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
