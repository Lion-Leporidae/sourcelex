// Package repo 提供多仓库管理和用户隔离
// RepoRegistry 是仓库连接池，按需懒加载 KnowledgeStore，LRU 淘汰释放内存
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// MultiSearchResult 跨仓库搜索单个仓库的结果
type MultiSearchResult struct {
	RepoKey string
	RepoID  string
	Branch  string
	Results []store.SearchResult
}

// SearchAll 在所有已索引仓库中并行语义搜索，合并结果按分数排序
// repoKeys 为空则搜索全部仓库；否则只搜索指定仓库
func (r *Registry) SearchAll(ctx context.Context, query string, topK int, repoKeys []string) ([]MultiSearchResult, error) {
	var targets []RepoMetadata
	if len(repoKeys) > 0 {
		// 指定仓库
		for _, key := range repoKeys {
			repoID, branch := parseRepoKey(key)
			targets = append(targets, RepoMetadata{RepoID: repoID, Branch: branch})
		}
	} else {
		targets = r.List()
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("没有可搜索的仓库")
	}

	type result struct {
		msr MultiSearchResult
		err error
	}

	ch := make(chan result, len(targets))
	for _, meta := range targets {
		go func(m RepoMetadata) {
			key := RepoKey(m.RepoID, m.Branch)
			rc, err := r.Get(key)
			if err != nil {
				ch <- result{err: fmt.Errorf("加载仓库 %s 失败: %w", key, err)}
				return
			}
			defer rc.Release()

			results, err := rc.Store.SemanticSearch(ctx, query, topK)
			if err != nil {
				ch <- result{err: nil} // 单个仓库搜索失败不中断
				return
			}

			ch <- result{msr: MultiSearchResult{
				RepoKey: key,
				RepoID:  m.RepoID,
				Branch:  m.Branch,
				Results: results,
			}}
		}(meta)
	}

	var allResults []MultiSearchResult
	for i := 0; i < len(targets); i++ {
		r := <-ch
		if r.err == nil && len(r.msr.Results) > 0 {
			allResults = append(allResults, r.msr)
		}
	}

	// 按每个仓库最高分降序排列
	sort.Slice(allResults, func(i, j int) bool {
		var si, sj float32
		if len(allResults[i].Results) > 0 {
			si = allResults[i].Results[0].Score
		}
		if len(allResults[j].Results) > 0 {
			sj = allResults[j].Results[0].Score
		}
		return si > sj
	})

	return allResults, nil
}

// MultiRAGSource 跨仓库 RAG 来源（带仓库标识）
type MultiRAGSource struct {
	RepoKey   string  `json:"repo_key"`
	RepoID    string  `json:"repo_id"`
	EntityID  string  `json:"entity_id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	FilePath  string  `json:"file_path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float32 `json:"score"`
	Reason    string  `json:"reason"`
}

// MultiRAGResponse 跨仓库 RAG 响应
type MultiRAGResponse struct {
	Context string           `json:"context"`
	Sources []MultiRAGSource `json:"sources"`
	Stats   MultiRAGStats    `json:"stats"`
}

// MultiRAGStats 跨仓库 RAG 统计
type MultiRAGStats struct {
	RepoCount     int `json:"repo_count"`
	TotalSources  int `json:"total_sources"`
	ContextLength int `json:"context_length"`
}

// RAGAll 在所有（或指定）仓库中并行执行 RAG 管线，合并上下文
func (r *Registry) RAGAll(ctx context.Context, req store.RAGRequest, repoKeys []string) (*MultiRAGResponse, error) {
	var targets []RepoMetadata
	if len(repoKeys) > 0 {
		for _, key := range repoKeys {
			repoID, branch := parseRepoKey(key)
			targets = append(targets, RepoMetadata{RepoID: repoID, Branch: branch})
		}
	} else {
		targets = r.List()
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("没有可搜索的仓库")
	}

	// 并行 RAG
	type repoRAGResult struct {
		repoKey string
		repoID  string
		resp    *store.RAGResponse
	}

	ch := make(chan *repoRAGResult, len(targets))
	for _, meta := range targets {
		go func(m RepoMetadata) {
			key := RepoKey(m.RepoID, m.Branch)
			rc, err := r.Get(key)
			if err != nil {
				ch <- nil
				return
			}
			defer rc.Release()

			resp, err := rc.Store.RAGPipeline(ctx, req)
			if err != nil || resp == nil {
				ch <- nil
				return
			}
			ch <- &repoRAGResult{repoKey: key, repoID: m.RepoID, resp: resp}
		}(meta)
	}

	// 收集结果
	var allSources []MultiRAGSource
	repoCount := 0

	for i := 0; i < len(targets); i++ {
		rr := <-ch
		if rr == nil || rr.resp == nil {
			continue
		}
		repoCount++
		for _, src := range rr.resp.Sources {
			ms := MultiRAGSource{
				RepoKey:   rr.repoKey,
				RepoID:    rr.repoID,
				EntityID:  src.EntityID,
				Name:      src.Name,
				Type:      src.Type,
				FilePath:  src.FilePath,
				StartLine: src.StartLine,
				EndLine:   src.EndLine,
				Score:     src.Score,
				Reason:    src.Reason,
			}
			allSources = append(allSources, ms)
		}
	}

	// 按分数全局排序
	sort.Slice(allSources, func(i, j int) bool {
		return allSources[i].Score > allSources[j].Score
	})

	// 组装跨仓库上下文
	maxLen := req.MaxContextLength
	if maxLen <= 0 {
		maxLen = 16000
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# 跨仓库代码上下文 (查询: %s)\n\n", req.Query))

	includedSources := make([]MultiRAGSource, 0)
	currentLen := b.Len()

	// 为每个来源获取代码内容
	for idx, src := range allSources {
		rc, err := r.Get(src.RepoKey)
		if err != nil {
			continue
		}

		// 从图存储获取签名
		var codeContent string
		if node, err := rc.Store.GetEntity(ctx, src.EntityID); err == nil {
			codeContent = fmt.Sprintf("[%s] %s\nFile: %s:%d-%d",
				string(node.Type), node.ID, node.FilePath, node.StartLine, node.EndLine)
			if node.Signature != "" {
				codeContent += "\n" + node.Signature
			}
		}
		rc.Release()

		if codeContent == "" {
			codeContent = fmt.Sprintf("[%s] %s\nFile: %s:%d-%d", src.Type, src.EntityID, src.FilePath, src.StartLine, src.EndLine)
		}

		entry := fmt.Sprintf("## %d. [%s] %s  (%s)\n", idx+1, src.Type, src.Name, src.RepoKey)
		entry += fmt.Sprintf("File: %s (L%d-L%d) | Score: %.3f | %s\n\n", src.FilePath, src.StartLine, src.EndLine, src.Score, src.Reason)
		entry += "```\n" + codeContent + "\n```\n\n"

		if currentLen+len(entry) > maxLen {
			break
		}

		b.WriteString(entry)
		currentLen += len(entry)
		includedSources = append(includedSources, src)
	}

	// 附加调用关系摘要（每个仓库取 top 结果的调用链）
	if req.IncludeCallGraph {
		repoEntityIDs := make(map[string][]string) // repoKey → entityIDs
		for _, src := range includedSources {
			repoEntityIDs[src.RepoKey] = append(repoEntityIDs[src.RepoKey], src.EntityID)
		}
		var callSections []string
		for rk, ids := range repoEntityIDs {
			rc, err := r.Get(rk)
			if err != nil {
				continue
			}
			section := rc.Store.BuildCallChainSection(ctx, ids)
			rc.Release()
			if section != "" {
				callSections = append(callSections, fmt.Sprintf("### %s\n%s", rk, section))
			}
		}
		if len(callSections) > 0 {
			combined := strings.Join(callSections, "\n")
			if currentLen+len(combined) <= maxLen {
				b.WriteString("\n---\n## 跨仓库调用关系\n\n")
				b.WriteString(combined)
				currentLen += len(combined) + 30
			}
		}
	}

	return &MultiRAGResponse{
		Context: b.String(),
		Sources: includedSources,
		Stats: MultiRAGStats{
			RepoCount:     repoCount,
			TotalSources:  len(includedSources),
			ContextLength: b.Len(),
		},
	}, nil
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
