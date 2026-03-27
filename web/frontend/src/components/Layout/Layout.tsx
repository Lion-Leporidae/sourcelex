import { ReactNode, useState, useEffect } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { getStats } from '../../api/client'
import type { StatsData } from '../../api/types'
import styles from './Layout.module.css'

export default function Layout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const [stats, setStats] = useState<StatsData | null>(null)
  const [searchQuery, setSearchQuery] = useState('')

  useEffect(() => {
    getStats().then(setStats).catch(() => {})
  }, [])

  const navItems = [
    { path: '/', label: '概览', icon: '📊' },
    { path: '/explorer', label: '调用图谱', icon: '🔗' },
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
