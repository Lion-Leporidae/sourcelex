// Package analyzer provides code analysis functionality.
// Corresponds to: REPOMIND_ARCHITECTURE_MINDMAP.md - 代码分析阶段 (CodeAnalyzer)
package analyzer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/repomind/repomind-go/internal/logger"
)

// SupportedLanguages maps file extensions to language names
var SupportedLanguages = map[string]string{
	".py":   "python",
	".go":   "go",
	".java": "java",
	".js":   "javascript",
	".ts":   "typescript",
	".c":    "c",
	".cpp":  "cpp",
	".cc":   "cpp",
	".h":    "c",
	".hpp":  "cpp",
}

// FileState stores metadata for incremental update detection
type FileState struct {
	Path    string    `json:"path"`
	ModTime time.Time `json:"mod_time"`
	Hash    string    `json:"hash"`
	Size    int64     `json:"size"`
}

// FileScanner scans repository files with incremental update detection
type FileScanner struct {
	repoPath  string
	cache     map[string]FileState
	cachePath string
	log       *logger.Logger
}

// NewFileScanner creates a new file scanner
func NewFileScanner(repoPath string, log *logger.Logger) *FileScanner {
	scanner := &FileScanner{
		repoPath:  repoPath,
		cache:     make(map[string]FileState),
		cachePath: filepath.Join(repoPath, ".repomind_cache.json"),
		log:       log,
	}
	scanner.loadCache()
	return scanner
}

// ScanResult holds the result of a file scan
type ScanResult struct {
	NewFiles       []string // 新增文件
	ModifiedFiles  []string // 修改的文件
	DeletedFiles   []string // 删除的文件
	UnchangedFiles []string // 未变化的文件
	TotalFiles     int
}

// Scan scans the repository and detects changed files
// Implements: REPOMIND_ARCHITECTURE_MINDMAP.md - 步骤2: 检查增量更新
func (s *FileScanner) Scan(ctx context.Context) (*ScanResult, error) {
	result := &ScanResult{}
	currentFiles := make(map[string]bool)

	err := filepath.WalkDir(s.repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// 检查上下文取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 跳过目录和隐藏文件
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// 检查是否为支持的源代码文件
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := SupportedLanguages[ext]; !ok {
			return nil
		}

		relPath, _ := filepath.Rel(s.repoPath, path)
		currentFiles[relPath] = true

		// 获取文件信息
		info, err := d.Info()
		if err != nil {
			return nil // 跳过无法读取的文件
		}

		// 检查缓存
		cached, exists := s.cache[relPath]
		if !exists {
			// 新文件
			result.NewFiles = append(result.NewFiles, relPath)
			s.updateCache(relPath, path, info)
			return nil
		}

		// 快速检查：mtime 未变化则跳过
		if cached.ModTime.Equal(info.ModTime()) && cached.Size == info.Size() {
			result.UnchangedFiles = append(result.UnchangedFiles, relPath)
			return nil
		}

		// mtime 变化，计算 hash 确认
		hash, err := s.calculateHash(path)
		if err != nil {
			return nil
		}

		if hash == cached.Hash {
			// 仅 mtime 变化（如 touch），更新缓存但不重新分析
			s.cache[relPath] = FileState{
				Path:    relPath,
				ModTime: info.ModTime(),
				Hash:    hash,
				Size:    info.Size(),
			}
			result.UnchangedFiles = append(result.UnchangedFiles, relPath)
		} else {
			// 内容实际变化
			result.ModifiedFiles = append(result.ModifiedFiles, relPath)
			s.cache[relPath] = FileState{
				Path:    relPath,
				ModTime: info.ModTime(),
				Hash:    hash,
				Size:    info.Size(),
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("扫描文件失败: %w", err)
	}

	// 检测删除的文件
	for path := range s.cache {
		if !currentFiles[path] {
			result.DeletedFiles = append(result.DeletedFiles, path)
			delete(s.cache, path)
		}
	}

	result.TotalFiles = len(result.NewFiles) + len(result.ModifiedFiles) + len(result.UnchangedFiles)

	// 持久化扫描缓存
	if err := s.SaveCache(); err != nil {
		s.log.Warn("保存扫描缓存失败", "error", err)
	}

	return result, nil
}

// GetFilesToAnalyze returns files that need to be analyzed
func (s *FileScanner) GetFilesToAnalyze(result *ScanResult) []string {
	files := make([]string, 0, len(result.NewFiles)+len(result.ModifiedFiles))
	files = append(files, result.NewFiles...)
	files = append(files, result.ModifiedFiles...)
	return files
}

// updateCache updates the cache for a file
func (s *FileScanner) updateCache(relPath, absPath string, info os.FileInfo) {
	hash, _ := s.calculateHash(absPath)
	s.cache[relPath] = FileState{
		Path:    relPath,
		ModTime: info.ModTime(),
		Hash:    hash,
		Size:    info.Size(),
	}
}

// calculateHash calculates SHA1 hash of a file
func (s *FileScanner) calculateHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// GetLanguage returns the language for a file path
func GetLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := SupportedLanguages[ext]; ok {
		return lang
	}
	return ""
}

// loadCache 从文件加载扫描缓存
func (s *FileScanner) loadCache() {
	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		return
	}

	var cached map[string]FileState
	if err := json.Unmarshal(data, &cached); err != nil {
		s.log.Debug("缓存文件解析失败，将重建", "error", err)
		return
	}

	s.cache = cached
	s.log.Debug("已加载扫描缓存", "entries", len(cached))
}

// SaveCache 将扫描缓存持久化到文件
func (s *FileScanner) SaveCache() error {
	data, err := json.Marshal(s.cache)
	if err != nil {
		return fmt.Errorf("序列化缓存失败: %w", err)
	}

	if err := os.WriteFile(s.cachePath, data, 0644); err != nil {
		return fmt.Errorf("写入缓存文件失败: %w", err)
	}

	s.log.Debug("已保存扫描缓存", "entries", len(s.cache))
	return nil
}

// SetCachePath 设置缓存文件路径
func (s *FileScanner) SetCachePath(path string) {
	s.cachePath = path
	s.loadCache()
}
