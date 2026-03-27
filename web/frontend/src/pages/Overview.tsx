import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getStats, getGraphData } from '../api/client'
import type { StatsData, GraphData } from '../api/types'

export default function Overview() {
  const [stats, setStats] = useState<StatsData | null>(null)
  const [graphData, setGraphData] = useState<GraphData | null>(null)

  useEffect(() => {
    getStats().then(setStats).catch(() => {})
    getGraphData().then(setGraphData).catch(() => {})
  }, [])

  const fileCount = graphData?.nodes
    ? new Set(graphData.nodes.map(n => n.file_path)).size
    : 0

  const topCallers = graphData?.edges ? (() => {
    const count: Record<string, number> = {}
    graphData.edges.forEach(e => {
      const src = typeof e.source === 'string' ? e.source : (e.source as any).id
      count[src] = (count[src] || 0) + 1
    })
    return Object.entries(count).sort((a, b) => b[1] - a[1]).slice(0, 10)
  })() : []

  return (
    <article>
      <h1 className="wiki-h1">Sourcelex 代码知识图谱</h1>

      <p style={{ marginBottom: 16, lineHeight: 1.8 }}>
        <strong>Sourcelex</strong> 是一个将代码仓库转化为可搜索知识库的工具。
        它通过 <a href="https://tree-sitter.github.io/" target="_blank" rel="noreferrer">Tree-sitter</a> 解析代码的 AST，
        提取函数、类、方法等实体及其跨文件调用关系，支持语义搜索和 <a href="https://spec.modelcontextprotocol.io/" target="_blank" rel="noreferrer">MCP 协议</a>接入。
      </p>

      {/* Stats Cards */}
      <h2 className="wiki-h2">统计概览</h2>
      <table className="wiki-table">
        <thead>
          <tr>
            <th>指标</th>
            <th>数值</th>
            <th>说明</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>代码实体</td>
            <td className="mono">{stats?.node_count ?? '--'}</td>
            <td>函数、类、方法等代码符号</td>
          </tr>
          <tr>
            <td>调用关系</td>
            <td className="mono">{stats?.edge_count ?? '--'}</td>
            <td>跨文件和文件内的函数调用边</td>
          </tr>
          <tr>
            <td>源文件</td>
            <td className="mono">{fileCount || '--'}</td>
            <td>已索引的代码文件数量</td>
          </tr>
        </tbody>
      </table>

      {/* Entity Type Distribution */}
      {graphData?.nodes && graphData.nodes.length > 0 && (
        <>
          <h2 className="wiki-h2">实体类型分布</h2>
          <table className="wiki-table">
            <thead>
              <tr>
                <th>类型</th>
                <th>数量</th>
                <th>占比</th>
              </tr>
            </thead>
            <tbody>
              {['function', 'class', 'method'].map(type => {
                const count = graphData.nodes.filter(n => n.type === type).length
                const pct = graphData.nodes.length > 0
                  ? ((count / graphData.nodes.length) * 100).toFixed(1) : '0'
                const badgeClass = `badge badge-${type}`
                return (
                  <tr key={type}>
                    <td><span className={badgeClass}>{type}</span></td>
                    <td className="mono">{count}</td>
                    <td>{pct}%</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </>
      )}

      {/* Top Callers */}
      {topCallers.length > 0 && (
        <>
          <h2 className="wiki-h2">最活跃的调用者</h2>
          <table className="wiki-table">
            <thead>
              <tr>
                <th>#</th>
                <th>实体</th>
                <th>调用次数</th>
              </tr>
            </thead>
            <tbody>
              {topCallers.map(([id, count], i) => (
                <tr key={id}>
                  <td>{i + 1}</td>
                  <td>
                    <Link to={`/entity/${encodeURIComponent(id)}`}>{id}</Link>
                  </td>
                  <td className="mono">{count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}

      {/* Quick Links */}
      <h2 className="wiki-h2">快速导航</h2>
      <ul style={{ paddingLeft: 24, lineHeight: 2 }}>
        <li><Link to="/explorer">调用图谱浏览器</Link> — 可视化查看跨文件调用关系</li>
        <li><Link to="/explorer">文件浏览器</Link> — 浏览源代码文件树</li>
      </ul>

      <h2 className="wiki-h2">参见</h2>
      <ul style={{ paddingLeft: 24, lineHeight: 2 }}>
        <li><a href="https://github.com/Lion-Leporidae/sourcelex" target="_blank" rel="noreferrer">GitHub 仓库</a></li>
        <li><a href="https://tree-sitter.github.io/" target="_blank" rel="noreferrer">Tree-sitter 解析器</a></li>
        <li><a href="https://spec.modelcontextprotocol.io/" target="_blank" rel="noreferrer">MCP 协议规范</a></li>
      </ul>
    </article>
  )
}
