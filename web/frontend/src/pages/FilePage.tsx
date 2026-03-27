import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { getFileLines } from '../api/client'
import type { FileContent } from '../api/types'

export default function FilePage() {
  const location = useLocation()
  const filePath = location.pathname.replace(/^\/file\//, '')
  const [content, setContent] = useState<FileContent | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!filePath) return
    setContent(null)
    setError('')
    getFileLines(filePath)
      .then(setContent)
      .catch(() => setError('加载文件失败'))
  }, [filePath])

  const pathParts = filePath.split('/')
  const fileName = pathParts[pathParts.length - 1] || filePath

  return (
    <article>
      {/* Breadcrumb */}
      <div className="breadcrumb">
        <Link to="/">首页</Link>
        {pathParts.map((part, i) => (
          <span key={i}>
            <span>›</span>
            {i < pathParts.length - 1 ? (
              <Link to={`/file/${pathParts.slice(0, i + 1).join('/')}`}>{part}</Link>
            ) : (
              <strong>{part}</strong>
            )}
          </span>
        ))}
      </div>

      <h1 className="wiki-h1">{fileName}</h1>

      {/* File info infobox */}
      <div className="wiki-infobox" style={{ width: 240 }}>
        <div className="wiki-infobox-title">{fileName}</div>
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">路径</div>
          <div className="wiki-infobox-value">{filePath}</div>
        </div>
        {content && (
          <div className="wiki-infobox-row">
            <div className="wiki-infobox-label">总行数</div>
            <div className="wiki-infobox-value">{content.total_lines}</div>
          </div>
        )}
        <div className="wiki-infobox-row">
          <div className="wiki-infobox-label">扩展名</div>
          <div className="wiki-infobox-value">.{fileName.split('.').pop()}</div>
        </div>
      </div>

      <p>
        <code>{filePath}</code> 是项目中的源代码文件。
      </p>

      {error && <p style={{ color: 'var(--wiki-error)' }}>{error}</p>}

      {content && (
        <>
          <h2 className="wiki-h2">源代码</h2>
          <div className="wiki-code-block">
            <div className="wiki-code-header">
              <span>{filePath}</span>
              <span>{content.total_lines} 行</span>
            </div>
            <div className="wiki-code-content">
              {content.lines.map((line, i) => {
                const lineNum = (content.start_line || 1) + i
                return (
                  <div key={i} className="wiki-code-line">
                    <span className="wiki-code-linenum">{lineNum}</span>
                    <span className="wiki-code-text">{line}</span>
                  </div>
                )
              })}
            </div>
          </div>
        </>
      )}

      <div style={{ clear: 'both' }} />
    </article>
  )
}
