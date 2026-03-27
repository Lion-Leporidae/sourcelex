// Package repo 提供多仓库管理和用户隔离
// RepoRegistry 是仓库连接池，按需懒加载 KnowledgeStore，LRU 淘汰释放内存
package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	repogit "github.com/Lion-Leporidae/sourcelex/internal/git"
	"github.com/Lion-Leporidae/sourcelex/internal/logger"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
	"github.com/Lion-Leporidae/sourcelex/internal/store/graph"
	"github.com/Lion-Leporidae/sourcelex/internal/store/vector"
)

// RepoMetadata 仓库元数据
type RepoMetadata struct {
	RepoID    string    `json:"repo_id"`
	RepoPath  string    `json:"repo_path"`
	RepoURL   string    `json:"repo_url,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	IndexedAt time.Time `json:"indexed_at"`
}

// RepoKey 唯一标识一个仓库实例（repoID@branch）
func RepoKey(repoID, branch string) string {
	if branch == "" {
		branch = "main"
	}
	return repoID + "@" + branch
}

// RepoContext 一个已打开的仓库上下文
type RepoContext struct {
	Store    *store.KnowledgeStore
	GitRepo  *repogit.Repository
	Meta     *RepoMetadata
	RepoPath string
	Key      string
	lastUsed time.Time
	refCount int32
}

func (rc *RepoContext) AddRef()     { atomic.AddInt32(&rc.refCount, 1) }
func (rc *RepoContext) Release()    { atomic.AddInt32(&rc.refCount, -1) }
func (rc *RepoContext) RefCount() int32 { return atomic.LoadInt32(&rc.refCount) }

// RegistryConfig 配置
type RegistryConfig struct {
	DataDir   string
	Embedder  vector.Embedder
	VectorDim int
	Log       *logger.Logger
	MaxOpen   int // 最大同时打开的仓库数，0 = 无限制
}

// Registry 多仓库连接池
type Registry struct {
	mu      sync.RWMutex
	repos   map[string]*RepoContext // key = "repoID@branch"
	dataDir string
	embedder vector.Embedder
	vectorDim int
	log     *logger.Logger
	maxOpen int
}

// NewRegistry 创建仓库注册表
func NewRegistry(cfg RegistryConfig) *Registry {
	if cfg.MaxOpen <= 0 {
		cfg.MaxOpen = 10
	}
	return &Registry{
		repos:    make(map[string]*RepoContext),
		dataDir:  cfg.DataDir,
		embedder: cfg.Embedder,
		vectorDim: cfg.VectorDim,
		log:      cfg.Log,
		maxOpen:  cfg.MaxOpen,
	}
}

// Get 获取或懒加载仓库 store（并发安全）
func (r *Registry) Get(key string) (*RepoContext, error) {
	r.mu.RLock()
	if rc, ok := r.repos[key]; ok {
		rc.lastUsed = time.Now()
		rc.AddRef()
		r.mu.RUnlock()
		return rc, nil
	}
	r.mu.RUnlock()

	// 需要加载，升级到写锁
	r.mu.Lock()
	defer r.mu.Unlock()

	// double check
	if rc, ok := r.repos[key]; ok {
		rc.lastUsed = time.Now()
		rc.AddRef()
		return rc, nil
	}

	// LRU 淘汰
	if r.maxOpen > 0 && len(r.repos) >= r.maxOpen {
		r.evictOldest()
	}

	rc, err := r.loadRepo(key)
	if err != nil {
		return nil, err
	}

	rc.lastUsed = time.Now()
	rc.AddRef()
	r.repos[key] = rc
	r.log.Info("仓库已加载", "key", key)
	return rc, nil
}

// loadRepo 加载一个仓库的所有存储
func (r *Registry) loadRepo(key string) (*RepoContext, error) {
	// 查找数据目录：先尝试 data/{repoID}/{branch}/，再尝试旧格式 data/{repoID}/
	repoID, branch := parseRepoKey(key)
	dataDir := r.findDataDir(repoID, branch)
	if dataDir == "" {
		return nil, fmt.Errorf("仓库数据不存在: %s", key)
	}

	// 加载元数据
	meta, err := LoadMetadata(dataDir)
	if err != nil {
		return nil, fmt.Errorf("加载元数据失败 %s: %w", key, err)
	}

	// 加载向量存储
	var vectorStore vector.Store
	vectorPath := filepath.Join(dataDir, "vectors")
	if _, err := os.Stat(vectorPath); err == nil {
		vs, err := vector.NewChromemStore(vector.ChromemConfig{
			PersistPath:    vectorPath,
			CollectionName: "code_vectors",
			VectorDim:      r.vectorDim,
		})
		if err != nil {
			r.log.Warn("加载向量存储失败", "key", key, "error", err)
		} else {
			vectorStore = vs
		}
	}

	// 加载图存储
	var graphStore graph.Store
	graphPath := filepath.Join(dataDir, "graph.db")
	if _, err := os.Stat(graphPath); err == nil {
		gs, err := graph.NewSQLiteStore(graph.SQLiteConfig{DBPath: graphPath})
		if err != nil {
			r.log.Warn("加载图存储失败，使用内存存储", "key", key, "error", err)
			graphStore = graph.NewMemoryStore()
		} else {
			graphStore = gs
		}
	} else {
		graphStore = graph.NewMemoryStore()
	}

	// 创建知识存储
	ks := store.New(store.Config{
		VectorStore: vectorStore,
		GraphStore:  graphStore,
		Embedder:    r.embedder,
		RepoPath:    meta.RepoPath,
		Log:         r.log,
	})

	// 打开 Git 仓库
	var gitRepo *repogit.Repository
	if meta.RepoPath != "" {
		if repo, err := repogit.Open(meta.RepoPath); err == nil {
			gitRepo = repo
		}
	}

	return &RepoContext{
		Store:    ks,
		GitRepo:  gitRepo,
		Meta:     meta,
		RepoPath: meta.RepoPath,
		Key:      key,
	}, nil
}

// findDataDir 查找仓库数据目录（兼容新旧格式）
func (r *Registry) findDataDir(repoID, branch string) string {
	// 新格式: data/{repoID}/{branch}/
	newDir := filepath.Join(r.dataDir, repoID, branch)
	if _, err := os.Stat(filepath.Join(newDir, "metadata.json")); err == nil {
		return newDir
	}
	// 旧格式: data/{repoID}/
	oldDir := filepath.Join(r.dataDir, repoID)
	if _, err := os.Stat(filepath.Join(oldDir, "metadata.json")); err == nil {
		return oldDir
	}
	return ""
}

// evictOldest 淘汰最久未使用且无引用的仓库
func (r *Registry) evictOldest() {
	var oldest string
	var oldestTime time.Time
	for key, rc := range r.repos {
		if rc.RefCount() > 0 {
			continue
		}
		if oldest == "" || rc.lastUsed.Before(oldestTime) {
			oldest = key
			oldestTime = rc.lastUsed
		}
	}
	if oldest != "" {
		if rc, ok := r.repos[oldest]; ok {
			rc.Store.Close()
			delete(r.repos, oldest)
			r.log.Info("仓库已淘汰（LRU）", "key", oldest)
		}
	}
}

// List 列出所有已索引仓库
func (r *Registry) List() []RepoMetadata {
	var result []RepoMetadata

	entries, err := os.ReadDir(r.dataDir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoID := entry.Name()
		repoDir := filepath.Join(r.dataDir, repoID)

		// 检查旧格式：data/{repoID}/metadata.json
		if meta, err := LoadMetadata(repoDir); err == nil {
			result = append(result, *meta)
			continue
		}

		// 新格式：data/{repoID}/{branch}/metadata.json
		branchEntries, err := os.ReadDir(repoDir)
		if err != nil {
			continue
		}
		for _, bEntry := range branchEntries {
			if !bEntry.IsDir() {
				continue
			}
			branchDir := filepath.Join(repoDir, bEntry.Name())
			if meta, err := LoadMetadata(branchDir); err == nil {
				result = append(result, *meta)
			}
		}
	}

	// 按索引时间降序
	sort.Slice(result, func(i, j int) bool {
		return result[i].IndexedAt.After(result[j].IndexedAt)
	})
	return result
}

// Close 关闭所有打开的仓库
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, rc := range r.repos {
		rc.Store.Close()
		delete(r.repos, key)
	}
}

// LoadMetadata 加载仓库元数据
func LoadMetadata(dir string) (*RepoMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, err
	}
	var meta RepoMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// parseRepoKey 解析 "repoID@branch" → (repoID, branch)
func parseRepoKey(key string) (string, string) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '@' {
			return key[:i], key[i+1:]
		}
	}
	return key, "main"
}
