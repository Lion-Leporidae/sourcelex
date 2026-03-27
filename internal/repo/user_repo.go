package repo

import "sync"

// UserRepoManager 管理用户（或 session）的活跃仓库绑定
// Phase 1 用 sessionID 做 key，Phase 2 改为 userID
type UserRepoManager struct {
	mu       sync.RWMutex
	active   map[string]string // sessionID/userID → repoKey
	fallback string            // 默认仓库 key（最近索引的）
}

// NewUserRepoManager 创建管理器
func NewUserRepoManager(fallback string) *UserRepoManager {
	return &UserRepoManager{
		active:   make(map[string]string),
		fallback: fallback,
	}
}

// SetActive 设置用户的活跃仓库
func (m *UserRepoManager) SetActive(sessionID, repoKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[sessionID] = repoKey
}

// GetActive 获取用户的活跃仓库 key
func (m *UserRepoManager) GetActive(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if key, ok := m.active[sessionID]; ok {
		return key
	}
	return m.fallback
}

// SetFallback 设置默认仓库
func (m *UserRepoManager) SetFallback(repoKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallback = repoKey
}

// Remove 移除用户绑定
func (m *UserRepoManager) Remove(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, sessionID)
}
