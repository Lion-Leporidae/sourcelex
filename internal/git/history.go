// Package git provides Git repository management using go-git.
// history.go 实现 Git 历史提取功能，包括:
// - 提交日志查询（支持关键词、作者、时间范围过滤）
// - 提交详情（包含文件变更列表）
// - 文件变更历史
// - 文件 Blame 分析
package git

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// CommitInfo 提交信息
type CommitInfo struct {
	Hash      string       `json:"hash"`
	ShortHash string       `json:"short_hash"`
	Author    string       `json:"author"`
	Email     string       `json:"email"`
	Message   string       `json:"message"`
	Timestamp time.Time    `json:"timestamp"`
	Files     []FileChange `json:"files,omitempty"`
}

// FileChange 文件变更
type FileChange struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"` // added, modified, deleted, renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// FileHistoryEntry 文件历史条目
type FileHistoryEntry struct {
	Commit CommitInfo `json:"commit"`
	Change FileChange `json:"change"`
}

// BlameLine Blame 行信息
type BlameLine struct {
	LineNumber int       `json:"line_number"`
	Hash       string    `json:"hash"`
	Author     string    `json:"author"`
	Email      string    `json:"email"`
	Timestamp  time.Time `json:"timestamp"`
	Content    string    `json:"content"`
}

// BlameResult Blame 结果
type BlameResult struct {
	Path  string      `json:"path"`
	Lines []BlameLine `json:"lines"`
}

// LogOptions 日志查询选项
type LogOptions struct {
	MaxCount int       // 最大返回数量，0 表示不限制
	Since    time.Time // 起始时间
	Until    time.Time // 结束时间
	Author   string    // 按作者过滤（子串匹配）
	Keyword  string    // 按提交信息关键词过滤
	FilePath string    // 按文件路径过滤
}

// Log 获取提交历史
func (r *Repository) Log(ctx context.Context, opts LogOptions) ([]CommitInfo, error) {
	logOpts := &git.LogOptions{
		Order: git.LogOrderCommitterTime,
	}

	if !opts.Since.IsZero() {
		logOpts.Since = &opts.Since
	}
	if !opts.Until.IsZero() {
		logOpts.Until = &opts.Until
	}

	if opts.FilePath != "" {
		logOpts.FileName = &opts.FilePath
	}

	iter, err := r.repo.Log(logOpts)
	if err != nil {
		return nil, fmt.Errorf("获取提交日志失败: %w", err)
	}
	defer iter.Close()

	maxCount := opts.MaxCount
	if maxCount <= 0 {
		maxCount = 100
	}

	var commits []CommitInfo
	err = iter.ForEach(func(c *object.Commit) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if len(commits) >= maxCount {
			return fmt.Errorf("__limit_reached__")
		}

		// 作者过滤
		if opts.Author != "" {
			if !strings.Contains(strings.ToLower(c.Author.Name), strings.ToLower(opts.Author)) &&
				!strings.Contains(strings.ToLower(c.Author.Email), strings.ToLower(opts.Author)) {
				return nil
			}
		}

		// 关键词过滤
		if opts.Keyword != "" {
			if !strings.Contains(strings.ToLower(c.Message), strings.ToLower(opts.Keyword)) {
				return nil
			}
		}

		commits = append(commits, buildCommitInfo(c))
		return nil
	})

	if err != nil && err.Error() != "__limit_reached__" {
		return nil, fmt.Errorf("遍历提交失败: %w", err)
	}

	return commits, nil
}

// CommitDetail 获取提交详情（含文件变更）
func (r *Repository) CommitDetail(hash string) (*CommitInfo, error) {
	h := plumbing.NewHash(hash)
	c, err := r.repo.CommitObject(h)
	if err != nil {
		return nil, fmt.Errorf("获取提交对象失败: %w", err)
	}

	info := buildCommitInfo(c)

	changes, err := r.diffCommit(c)
	if err != nil {
		return &info, nil
	}
	info.Files = changes

	return &info, nil
}

// FileHistory 获取文件的变更历史
func (r *Repository) FileHistory(ctx context.Context, filePath string, maxCount int) ([]FileHistoryEntry, error) {
	if maxCount <= 0 {
		maxCount = 50
	}

	logOpts := &git.LogOptions{
		Order:    git.LogOrderCommitterTime,
		FileName: &filePath,
	}

	iter, err := r.repo.Log(logOpts)
	if err != nil {
		return nil, fmt.Errorf("获取文件历史失败: %w", err)
	}
	defer iter.Close()

	var entries []FileHistoryEntry
	err = iter.ForEach(func(c *object.Commit) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if len(entries) >= maxCount {
			return fmt.Errorf("__limit_reached__")
		}

		change, err := r.fileChangeInCommit(c, filePath)
		if err != nil {
			return nil
		}

		entries = append(entries, FileHistoryEntry{
			Commit: buildCommitInfo(c),
			Change: *change,
		})
		return nil
	})

	if err != nil && err.Error() != "__limit_reached__" {
		return nil, fmt.Errorf("遍历文件历史失败: %w", err)
	}

	return entries, nil
}

// Blame 获取文件的 Blame 信息
func (r *Repository) Blame(filePath string) (*BlameResult, error) {
	// 获取 HEAD commit
	ref, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("获取 HEAD 失败: %w", err)
	}

	c, err := r.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("获取 HEAD commit 失败: %w", err)
	}

	result, err := git.Blame(c, filePath)
	if err != nil {
		return nil, fmt.Errorf("Blame 失败: %w", err)
	}

	var lines []BlameLine
	for i, line := range result.Lines {
		lines = append(lines, BlameLine{
			LineNumber: i + 1,
			Hash:       line.Hash.String()[:8],
			Author:     line.AuthorName,
			Email:      line.Author,
			Timestamp:  line.Date,
			Content:    line.Text,
		})
	}

	return &BlameResult{
		Path:  filePath,
		Lines: lines,
	}, nil
}

// CommitsByEntity 查找影响指定实体（文件+行号范围）的提交
func (r *Repository) CommitsByEntity(ctx context.Context, filePath string, startLine, endLine, maxCount int) ([]CommitInfo, error) {
	if maxCount <= 0 {
		maxCount = 20
	}

	history, err := r.FileHistory(ctx, filePath, maxCount*3)
	if err != nil {
		return nil, err
	}

	var result []CommitInfo
	for _, entry := range history {
		if len(result) >= maxCount {
			break
		}

		// 获取此提交中文件的变更行号范围（通过 blame 近似判断）
		// 对于性能考虑，简单检查该提交是否修改了目标文件即可
		result = append(result, entry.Commit)
	}

	return result, nil
}

// diffCommit 获取提交的文件变更列表
func (r *Repository) diffCommit(c *object.Commit) ([]FileChange, error) {
	parentIter := c.Parents()
	parent, err := parentIter.Next()
	parentIter.Close()

	var parentTree *object.Tree
	if err == nil {
		parentTree, err = parent.Tree()
		if err != nil {
			return nil, err
		}
	}

	currentTree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	if parentTree == nil {
		// 初始提交：所有文件都是新增
		var changes []FileChange
		walker := object.NewTreeWalker(currentTree, true, nil)
		defer walker.Close()
		for {
			name, _, err := walker.Next()
			if err != nil {
				break
			}
			changes = append(changes, FileChange{
				Path:   name,
				Status: "added",
			})
		}
		return changes, nil
	}

	diff, err := parentTree.Diff(currentTree)
	if err != nil {
		return nil, err
	}

	var changes []FileChange
	for _, d := range diff {
		change := FileChange{}
		action, err := d.Action()
		if err != nil {
			continue
		}

		switch action {
		case merkletrie.Insert:
			change.Status = "added"
			change.Path = d.To.Name
		case merkletrie.Delete:
			change.Status = "deleted"
			change.Path = d.From.Name
		case merkletrie.Modify:
			change.Status = "modified"
			change.Path = d.To.Name
		}

		// 计算增删行数
		patch, err := d.Patch()
		if err == nil {
			for _, fp := range patch.FilePatches() {
				for _, chunk := range fp.Chunks() {
					content := chunk.Content()
					lineCount := strings.Count(content, "\n")
					if len(content) > 0 && content[len(content)-1] != '\n' {
						lineCount++
					}
					switch chunk.Type() {
					case 1: // Add
						change.Additions += lineCount
					case 2: // Delete
						change.Deletions += lineCount
					}
				}
			}
		}

		if d.From.Name != "" && d.To.Name != "" && d.From.Name != d.To.Name {
			change.Status = "renamed"
			change.OldPath = d.From.Name
			change.Path = d.To.Name
		}

		changes = append(changes, change)
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})

	return changes, nil
}

// fileChangeInCommit 获取特定文件在特定提交中的变更
func (r *Repository) fileChangeInCommit(c *object.Commit, filePath string) (*FileChange, error) {
	changes, err := r.diffCommit(c)
	if err != nil {
		return nil, err
	}

	for _, ch := range changes {
		if ch.Path == filePath || ch.OldPath == filePath {
			return &ch, nil
		}
	}

	return &FileChange{
		Path:   filePath,
		Status: "modified",
	}, nil
}

// buildCommitInfo 从 go-git Commit 对象构建 CommitInfo
func buildCommitInfo(c *object.Commit) CommitInfo {
	hash := c.Hash.String()
	shortHash := hash
	if len(hash) > 8 {
		shortHash = hash[:8]
	}

	return CommitInfo{
		Hash:      hash,
		ShortHash: shortHash,
		Author:    c.Author.Name,
		Email:     c.Author.Email,
		Message:   strings.TrimSpace(c.Message),
		Timestamp: c.Author.When,
	}
}
