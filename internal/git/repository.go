// Package git provides Git repository management using go-git.
// It supports cloning remote repositories and opening local ones.
// Corresponds to: REPOMIND_ARCHITECTURE_MINDMAP.md - Git仓库管理 (GitRepositoryManager)
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Repository wraps a git repository with additional metadata
type Repository struct {
	repo     *git.Repository
	path     string
	isRemote bool
}

// CloneOptions configures repository cloning
type CloneOptions struct {
	URL      string
	Branch   string
	Depth    int
	DestPath string
}

// Clone clones a remote repository to the specified path
func Clone(ctx context.Context, opts CloneOptions) (*Repository, error) {
	if opts.DestPath == "" {
		return nil, fmt.Errorf("目标路径不能为空")
	}

	cloneOpts := &git.CloneOptions{
		URL:      opts.URL,
		Progress: os.Stdout,
	}

	// 浅克隆优化
	if opts.Depth > 0 {
		cloneOpts.Depth = opts.Depth
	}

	// 指定分支
	if opts.Branch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(opts.Branch)
		cloneOpts.SingleBranch = true
	}

	repo, err := git.PlainCloneContext(ctx, opts.DestPath, false, cloneOpts)
	if err != nil {
		return nil, fmt.Errorf("克隆仓库失败: %w", err)
	}

	return &Repository{
		repo:     repo,
		path:     opts.DestPath,
		isRemote: true,
	}, nil
}

// Open opens an existing local repository
func Open(path string) (*Repository, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析路径失败: %w", err)
	}

	repo, err := git.PlainOpen(absPath)
	if err != nil {
		return nil, fmt.Errorf("打开仓库失败: %w", err)
	}

	return &Repository{
		repo:     repo,
		path:     absPath,
		isRemote: false,
	}, nil
}

// Path returns the repository root path
func (r *Repository) Path() string {
	return r.path
}

// Checkout switches to the specified branch or commit
func (r *Repository) Checkout(ref string) error {
	w, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("获取工作树失败: %w", err)
	}

	// 尝试作为分支名
	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(ref),
	})
	if err == nil {
		return nil
	}

	// 尝试作为 commit hash
	hash := plumbing.NewHash(ref)
	return w.Checkout(&git.CheckoutOptions{
		Hash: hash,
	})
}

// Head returns the current HEAD reference name
func (r *Repository) Head() (string, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	return ref.Name().Short(), nil
}

// ListTrackedFiles returns all files tracked by git (respecting .gitignore)
func (r *Repository) ListTrackedFiles() ([]string, error) {
	w, err := r.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("获取工作树失败: %w", err)
	}

	status, err := w.Status()
	if err != nil {
		return nil, fmt.Errorf("获取状态失败: %w", err)
	}

	var files []string
	// 遍历索引中的文件
	idx, err := r.repo.Storer.Index()
	if err != nil {
		// 回退到遍历目录
		return r.walkDirectory()
	}

	for _, entry := range idx.Entries {
		files = append(files, entry.Name)
	}

	// 添加未暂存的新文件
	for file, s := range status {
		if s.Worktree == git.Untracked {
			continue // 忽略未跟踪文件
		}
		// 检查是否已在列表中
		found := false
		for _, f := range files {
			if f == file {
				found = true
				break
			}
		}
		if !found {
			files = append(files, file)
		}
	}

	return files, nil
}

// walkDirectory walks the directory and returns all files
func (r *Repository) walkDirectory() ([]string, error) {
	var files []string
	err := filepath.Walk(r.path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// 跳过 .git 目录
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(r.path, path)
			files = append(files, relPath)
		}
		return nil
	})
	return files, err
}
