import { ReactNode, useState, useEffect } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { getStats, getAuthMe, logout } from '../../api/client'
import type { StatsData } from '../../api/types'
import type { AuthInfo } from '../../api/client'
import styles from './Layout.module.css'

export default function Layout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const [stats, setStats] = useState<StatsData | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [authInfo, setAuthInfo] = useState<AuthInfo | null>(null)

  useEffect(() => {
    getStats().then(setStats).catch(() => {})
    getAuthMe().then(setAuthInfo).catch(() => {})
  }, [])

  const navItems = [
    { path: '/', label: '概览', icon: '📊' },
    { path: '/explorer', label: '调用图谱', icon: '🔗' },
    { path: '/chat', label: 'AI 对话', icon: '💬' },
  ]

  return (
    <div className={styles.root}>
      {/* Header */}
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <div className={styles.headerLeft}>
            <Link to="/" className={styles.logo}>
              <span className={styles.logoMark}>SL</span>
              <span className={styles.logoText}>Sourcelex</span>
            </Link>
            <span className={styles.subtitle}>代码知识图谱</span>
          </div>

          <div className={styles.headerCenter}>
            <div className={styles.searchBox}>
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
              </svg>
              <input
                type="text"
                placeholder="搜索函数、类、方法..."
                value={searchQuery}
                onChange={e => setSearchQuery(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter' && searchQuery.trim()) {
                    window.location.href = `/explorer?q=${encodeURIComponent(searchQuery.trim())}`
                  }
                }}
              />
            </div>
          </div>

          <div className={styles.headerRight}>
            {stats && (
              <span className={styles.stats}>
                {stats.node_count} 实体 · {stats.edge_count} 调用
              </span>
            )}
            {authInfo?.auth_enabled && !authInfo.authenticated && (
              <a href="/auth/github" style={{
                padding: '4px 12px', background: '#24292e', color: '#fff', borderRadius: 4,
                fontSize: 12, textDecoration: 'none', display: 'flex', alignItems: 'center', gap: 4
              }}>
                <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>
                登录
              </a>
            )}
            {authInfo?.authenticated && (
              <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                {authInfo.avatar_url && (
                  <img src={authInfo.avatar_url} alt="" style={{ width: 24, height: 24, borderRadius: '50%' }} />
                )}
                <span style={{ fontSize: 12, color: '#555' }}>{authInfo.login}</span>
                <button onClick={logout} style={{
                  padding: '2px 8px', background: 'none', border: '1px solid #ddd', borderRadius: 3,
                  fontSize: 11, cursor: 'pointer', color: '#999'
                }}>退出</button>
              </div>
            )}
          </div>
        </div>
      </header>

      <div className={styles.body}>
        {/* Sidebar */}
        <nav className={styles.sidebar}>
          <div className={styles.sidebarSection}>
            <div className={styles.sidebarTitle}>导航</div>
            {navItems.map(item => (
              <Link
                key={item.path}
                to={item.path}
                className={`${styles.sidebarItem} ${location.pathname === item.path ? styles.sidebarActive : ''}`}
              >
                <span className={styles.sidebarIcon}>{item.icon}</span>
                {item.label}
              </Link>
            ))}
          </div>

          <div className={styles.sidebarSection}>
            <div className={styles.sidebarTitle}>工具</div>
            <Link to="/explorer" className={styles.sidebarItem}>
              <span className={styles.sidebarIcon}>🌳</span>
              文件浏览
            </Link>
          </div>

          <div className={styles.sidebarSection}>
            <div className={styles.sidebarTitle}>统计</div>
            <div className={styles.sidebarStat}>
              <span>实体</span>
              <span className="mono">{stats?.node_count ?? '--'}</span>
            </div>
            <div className={styles.sidebarStat}>
              <span>调用关系</span>
              <span className="mono">{stats?.edge_count ?? '--'}</span>
            </div>
          </div>
        </nav>

        {/* Main Content */}
        <main className={styles.main}>
          {children}
        </main>
      </div>

      {/* Footer */}
      <footer className={styles.footer}>
        <span>Sourcelex v0.1.0 · MIT License</span>
        <a href="https://github.com/Lion-Leporidae/sourcelex" target="_blank" rel="noreferrer">GitHub</a>
      </footer>
    </div>
  )
}
