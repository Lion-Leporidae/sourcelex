import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { getGraphData, getCallers, getCallees, getFileLines } from '../api/client'
import type { GraphNode, GraphData, FileContent } from '../api/types'

export default function Entity() {
  const { id } = useParams<{ id: string }>()
  const entityId = decodeURIComponent(id || '')
  const [node, setNode] = useState<GraphNode | null>(null)
  const [callers, setCallers] = useState<GraphNode[]>([])
  const [callees, setCallees] = useState<GraphNode[]>([])
  const [source, setSource] = useState<FileContent | null>(null)

  useEffect(() => {
    if (!entityId) return

    getGraphData().then((data: GraphData) => {
      const found = data.nodes.find(n => n.id === entityId)
      if (found) {
        setNode(found)
        getFileLines(found.file_path, found.start_line, found.end_line).then(setSource).catch(() => {})
      }
    }).catch(() => {})

    getCallers(entityId, 2).then(setCallers).catch(() => {})
    getCallees(entityId, 2).then(setCallees).catch(() => {})
  }, [entityId])

  if (!node) {
    return (
      <article>
        <h1 className="wiki-h1">{entityId || '实体未找到'}</h1>
        <p className="muted">加载中...</p>
      </article>
    )
  }

  const badgeClass = `badge badge-${node.type}`
  const crossFileCallers = callers.filter(n => n.file_path !== node.file_path)
  const sameFileCallers = callers.filter(n => n.file_path === node.file_path)
  const crossFileCallees = callees.filter(n => n.file_path !== node.file_path)
  const sameFileCallees = callees.filter(n => n.file_path === node.file_path)

  return (
    <article>
      {/* Breadcrumb */}
      <div className="breadcrumb">
        <Link to="/">首页</Link>
        <span>›</span>
        <Link to={`/file/${node.file_path}`}>{node.file_path}</Link>
        <span>›</span>
        <strong>{node.name}</strong>
      </div>

      {/* Title */}
      <h1 className="wiki-h1">{node.name}</h1>

      {/* Infobox (Wikipedia-style right sidebar) */}
      <div className="wiki-infobox">
        <div className="wiki-infobox-title">{node.name}</div>
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">类型</div>
          <div className="wiki-infobox-value"><span className={badgeClass}>{node.type}</span></div>
        </div>
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">文件</div>
          <div className="wiki-infobox-value">
            <Link to={`/file/${node.file_path}`}>{node.file_path}</Link>
          </div>
        </div>
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">行号</div>
          <div className="wiki-infobox-value">{node.start_line} – {node.end_line}</div>
        </div>
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">限定名</div>
          <div className="wiki-infobox-value">{node.id}</div>
        </div>
        {node.signature && (
          <div className="wiki-infobox-row">
            <div className="wiki-infobox-label">签名</div>
            <div className="wiki-infobox-value">{node.signature}</div>
          </div>
        )}
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">调用者</div>
          <div className="wiki-infobox-value">{callers.length} 个</div>
        </div>
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">被调用</div>
          <div className="wiki-infobox-value">{callees.length} 个</div>
        </div>
      </div>

      {/* Description */}
      <p>
        <code>{node.id}</code> 是定义在 <Link to={`/file/${node.file_path}`}>{node.file_path}</Link> 中的
        <span className={badgeClass} style={{ marginLeft: 4 }}>{node.type}</span>，
        位于第 {node.start_line} 到 {node.end_line} 行。
      </p>

      {node.signature && (
        <div style={{ margin: '12px 0' }}>
          <code style={{ fontSize: '0.95em', padding: '4px 8px' }}>{node.signature}</code>
        </div>
      )}

      {/* Table of Contents */}
      <div style={{ background: 'var(--wiki-infobox-bg)', border: '1px solid var(--wiki-border-light)', padding: '10px 16px', margin: '16px 0', borderRadius: 4 }}>
        <strong style={{ fontSize: '0.9em' }}>目录</strong>
        <ol style={{ paddingLeft: 20, margin: '6px 0 0', fontSize: '0.9em', lineHeight: 1.8 }}>
          <li><a href="#callers" style={{ textDecoration: 'none' }}>调用者 ({callers.length})</a></li>
          <li><a href="#callees" style={{ textDecoration: 'none' }}>被调用 ({callees.length})</a></li>
          <li><a href="#source" style={{ textDecoration: 'none' }}>源代码</a></li>
        </ol>
      </div>

      {/* Callers Section */}
      <h2 className="wiki-h2" id="callers">调用者</h2>
      {callers.length === 0 ? (
        <p className="muted text-sm">没有发现调用此{node.type}的代码。</p>
      ) : (
        <>
          {crossFileCallers.length > 0 && (
            <>
              <h3 className="wiki-h3">跨文件调用者 ({crossFileCallers.length})</h3>
              <table className="wiki-table">
                <thead><tr><th>实体</th><th>类型</th><th>文件</th><th>行号</th></tr></thead>
                <tbody>
                  {crossFileCallers.map(n => (
                    <tr key={n.id}>
                      <td><Link to={`/entity/${encodeURIComponent(n.id)}`}>{n.name}</Link></td>
                      <td><span className={`badge badge-${n.type}`}>{n.type}</span></td>
                      <td className="mono text-sm"><Link to={`/file/${n.file_path}`}>{n.file_path}</Link></td>
                      <td className="mono">{n.start_line}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
          {sameFileCallers.length > 0 && (
            <>
              <h3 className="wiki-h3">同文件调用者 ({sameFileCallers.length})</h3>
              <table className="wiki-table">
                <thead><tr><th>实体</th><th>类型</th><th>行号</th></tr></thead>
                <tbody>
                  {sameFileCallers.map(n => (
                    <tr key={n.id}>
                      <td><Link to={`/entity/${encodeURIComponent(n.id)}`}>{n.name}</Link></td>
                      <td><span className={`badge badge-${n.type}`}>{n.type}</span></td>
                      <td className="mono">{n.start_line}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </>
      )}

      {/* Callees Section */}
      <h2 className="wiki-h2" id="callees">被调用</h2>
      {callees.length === 0 ? (
        <p className="muted text-sm">此{node.type}没有调用其他实体。</p>
      ) : (
        <>
          {crossFileCallees.length > 0 && (
            <>
              <h3 className="wiki-h3">跨文件被调用 ({crossFileCallees.length})</h3>
              <table className="wiki-table">
                <thead><tr><th>实体</th><th>类型</th><th>文件</th><th>行号</th></tr></thead>
                <tbody>
                  {crossFileCallees.map(n => (
                    <tr key={n.id}>
                      <td><Link to={`/entity/${encodeURIComponent(n.id)}`}>{n.name}</Link></td>
                      <td><span className={`badge badge-${n.type}`}>{n.type}</span></td>
                      <td className="mono text-sm"><Link to={`/file/${n.file_path}`}>{n.file_path}</Link></td>
                      <td className="mono">{n.start_line}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
          {sameFileCallees.length > 0 && (
            <>
              <h3 className="wiki-h3">同文件被调用 ({sameFileCallees.length})</h3>
              <table className="wiki-table">
                <thead><tr><th>实体</th><th>类型</th><th>行号</th></tr></thead>
                <tbody>
                  {sameFileCallees.map(n => (
                    <tr key={n.id}>
                      <td><Link to={`/entity/${encodeURIComponent(n.id)}`}>{n.name}</Link></td>
                      <td><span className={`badge badge-${n.type}`}>{n.type}</span></td>
                      <td className="mono">{n.start_line}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </>
      )}

      {/* Source Code */}
      <h2 className="wiki-h2" id="source">源代码</h2>
      {source ? (
        <div className="wiki-code-block">
          <div className="wiki-code-header">
            <span>{node.file_path}:{node.start_line}-{node.end_line}</span>
          </div>
          <div className="wiki-code-content">
            {source.lines.map((line, i) => {
              const lineNum = (source.start_line || 1) + i
              return (
                <div key={i} className="wiki-code-line">
                  <span className="wiki-code-linenum">{lineNum}</span>
                  <span className="wiki-code-text">{line}</span>
                </div>
              )
            })}
          </div>
        </div>
      ) : (
        <p className="muted text-sm">加载源代码中...</p>
      )}

      <div style={{ clear: 'both' }} />
    </article>
  )
}
