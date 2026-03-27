import { useEffect, useState, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { getGraphData, getFileTree, searchSemantic } from '../api/client'
import type { GraphData, GraphNode, FileTreeNode as FTN, SearchResult } from '../api/types'
import * as d3 from 'd3'

export default function Explorer() {
  const [graphData, setGraphData] = useState<GraphData | null>(null)
  const [fileTree, setFileTree] = useState<FTN | null>(null)
  const [searchResults, setSearchResults] = useState<SearchResult[]>([])
  const [searchQuery, setSearchQuery] = useState('')
  const svgRef = useRef<SVGSVGElement>(null)

  useEffect(() => {
    getGraphData().then(setGraphData).catch(() => {})
    getFileTree().then(setFileTree).catch(() => {})
  }, [])

  const renderGraph = useCallback(() => {
    if (!graphData || !svgRef.current) return
    const svg = d3.select(svgRef.current)
    svg.selectAll('*').remove()

    const container = svgRef.current.parentElement!
    const width = container.clientWidth
    const height = container.clientHeight

    const g = svg.append('g')
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.05, 4])
      .on('zoom', event => g.attr('transform', event.transform))
    svg.call(zoom)

    // Group by file
    const fileGroups: Record<string, GraphNode[]> = {}
    graphData.nodes.forEach(n => {
      const f = n.file_path || '(unknown)'
      if (!fileGroups[f]) fileGroups[f] = []
      fileGroups[f].push(n)
    })

    const fileNames = Object.keys(fileGroups).sort()
    const nodeIdToFile: Record<string, string> = {}
    graphData.nodes.forEach(n => { nodeIdToFile[n.id] = n.file_path || '' })

    const colorMap: Record<string, string> = { function: '#1565c0', class: '#2e7d32', method: '#7b1fa2' }
    const sizeMap: Record<string, number> = { function: 5, class: 8, method: 4 }
    const boxPadX = 14, boxPadY = 26, nodeH = 20, nodeW = 170
    const gapX = 50, gapY = 35
    const cols = Math.max(1, Math.ceil(Math.sqrt(fileNames.length)))

    const nodePos: Record<string, { x: number; y: number }> = {}
    let col = 0, cx = 20, cy = 20, maxRowH = 0

    // Layout file boxes
    const boxGroup = g.append('g')
    const linkGroup = g.append('g')
    const nodeGroup = g.append('g')

    fileNames.forEach(fn => {
      const nodes = fileGroups[fn]
      const boxW = nodeW + boxPadX * 2
      const boxH = boxPadY + nodes.length * nodeH + 8

      if (col >= cols) { col = 0; cx = 20; cy += maxRowH + gapY; maxRowH = 0 }

      // File box
      boxGroup.append('rect')
        .attr('x', cx).attr('y', cy)
        .attr('width', boxW).attr('height', boxH)
        .attr('rx', 3)
        .attr('fill', '#fff').attr('stroke', '#c8ccd1').attr('stroke-width', 1)

      boxGroup.append('text')
        .attr('x', cx + 8).attr('y', cy + 16)
        .attr('fill', '#0645ad').attr('font-size', '10px').attr('font-weight', '600')
        .attr('font-family', 'Inter, sans-serif')
        .text(fn.split('/').pop()!)
        .attr('cursor', 'pointer')

      nodes.forEach((n, i) => {
        const nx = cx + boxPadX + nodeW / 2
        const ny = cy + boxPadY + i * nodeH + nodeH / 2
        nodePos[n.id] = { x: nx, y: ny }

        nodeGroup.append('circle')
          .attr('cx', nx).attr('cy', ny)
          .attr('r', sizeMap[n.type] || 5)
          .attr('fill', colorMap[n.type] || '#1565c0')
          .attr('stroke', '#fff').attr('stroke-width', 1)
          .attr('cursor', 'pointer')
          .on('click', () => { window.location.href = `/entity/${encodeURIComponent(n.id)}` })

        nodeGroup.append('text')
          .attr('x', nx + (sizeMap[n.type] || 5) + 4).attr('y', ny + 3)
          .attr('fill', '#202122').attr('font-size', '10px')
          .attr('font-family', 'JetBrains Mono, monospace')
          .text(n.name)
          .attr('cursor', 'pointer')
          .on('click', () => { window.location.href = `/entity/${encodeURIComponent(n.id)}` })
      })

      cx += boxW + gapX
      maxRowH = Math.max(maxRowH, boxH)
      col++
    })

    // Draw edges
    const nodeIds = new Set(graphData.nodes.map(n => n.id))
    graphData.edges.forEach(e => {
      const srcId = typeof e.source === 'string' ? e.source : (e.source as any).id
      const tgtId = typeof e.target === 'string' ? e.target : (e.target as any).id
      if (!nodeIds.has(srcId) || !nodeIds.has(tgtId)) return
      const src = nodePos[srcId], tgt = nodePos[tgtId]
      if (!src || !tgt) return

      const isCross = nodeIdToFile[srcId] !== nodeIdToFile[tgtId]

      if (isCross) {
        const mx = (src.x + tgt.x) / 2, my = (src.y + tgt.y) / 2
        const dx = tgt.x - src.x, dy = tgt.y - src.y
        const dist = Math.sqrt(dx * dx + dy * dy) || 1
        const off = Math.min(dist * 0.25, 60)
        linkGroup.append('path')
          .attr('d', `M${src.x},${src.y} Q${mx - dy / dist * off},${my + dx / dist * off} ${tgt.x},${tgt.y}`)
          .attr('fill', 'none').attr('stroke', '#d33').attr('stroke-width', 1.2)
          .attr('stroke-dasharray', '5,3').attr('opacity', 0.6)
      } else {
        linkGroup.append('line')
          .attr('x1', src.x).attr('y1', src.y).attr('x2', tgt.x).attr('y2', tgt.y)
          .attr('stroke', '#c8ccd1').attr('stroke-width', 0.6).attr('opacity', 0.4)
      }
    })

    // Fit view
    const totalW = cx + 200, totalH = cy + maxRowH + 100
    const scaleX = width / totalW, scaleY = height / totalH
    const initScale = Math.min(scaleX, scaleY, 1) * 0.85
    svg.call(zoom.transform, d3.zoomIdentity.translate(10, 10).scale(initScale))
  }, [graphData])

  useEffect(() => { renderGraph() }, [renderGraph])

  const handleSearch = async () => {
    if (!searchQuery.trim()) return
    const results = await searchSemantic(searchQuery, 10)
    setSearchResults(results)
  }

  return (
    <div>
      <h1 className="wiki-h1">调用图谱浏览器</h1>

      <div style={{ display: 'flex', gap: 12, marginBottom: 16, alignItems: 'center' }}>
        <input
          type="text"
          placeholder="搜索实体..."
          value={searchQuery}
          onChange={e => setSearchQuery(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSearch()}
          style={{ flex: 1, maxWidth: 320, padding: '6px 10px', border: '1px solid #c8ccd1', borderRadius: 4, fontFamily: 'var(--font-code)', fontSize: 13 }}
        />
        <button onClick={handleSearch} style={{ padding: '6px 16px', background: 'var(--wiki-accent)', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer', fontSize: 13 }}>搜索</button>
      </div>

      {searchResults.length > 0 && (
        <div style={{ marginBottom: 16 }}>
          <h3 className="wiki-h3">搜索结果</h3>
          <table className="wiki-table">
            <thead><tr><th>实体</th><th>类型</th><th>文件</th><th>相似度</th></tr></thead>
            <tbody>
              {searchResults.map(r => (
                <tr key={r.entity_id}>
                  <td><Link to={`/entity/${encodeURIComponent(r.entity_id)}`}>{r.entity_id}</Link></td>
                  <td><span className={`badge badge-${r.type}`}>{r.type}</span></td>
                  <td className="mono text-sm">{r.file_path}</td>
                  <td className="mono">{(r.score * 100).toFixed(0)}%</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Legend */}
      <div style={{ display: 'flex', gap: 16, fontSize: 12, color: '#72777d', marginBottom: 8, alignItems: 'center' }}>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#1565c0', display: 'inline-block' }} />函数
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#2e7d32', display: 'inline-block' }} />类
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#7b1fa2', display: 'inline-block' }} />方法
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 16, height: 0, borderTop: '2px dashed #d33', display: 'inline-block' }} />跨文件调用
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 16, height: 2, background: '#c8ccd1', display: 'inline-block' }} />同文件调用
        </span>
      </div>

      {/* Graph */}
      <div style={{ border: '1px solid #c8ccd1', borderRadius: 4, background: '#fafafa', height: 500, overflow: 'hidden' }}>
        <svg ref={svgRef} style={{ width: '100%', height: '100%' }} />
      </div>

      {/* File Tree */}
      {fileTree && (
        <>
          <h2 className="wiki-h2" style={{ marginTop: 24 }}>文件目录</h2>
          <div style={{ fontFamily: 'var(--font-code)', fontSize: 13, lineHeight: 1.8 }}>
            <FileTreeView node={fileTree} depth={0} />
          </div>
        </>
      )}
    </div>
  )
}

function FileTreeView({ node, depth }: { node: FTN; depth: number }) {
  const [open, setOpen] = useState(depth < 1)

  if (!node.is_dir && !node.children) {
    return (
      <div style={{ paddingLeft: depth * 16 }}>
        📄 <Link to={`/file/${node.path}`}>{node.name}</Link>
      </div>
    )
  }

  const sorted = (node.children || []).slice().sort((a, b) => {
    if (a.is_dir && !b.is_dir) return -1
    if (!a.is_dir && b.is_dir) return 1
    return a.name.localeCompare(b.name)
  })

  return (
    <div>
      <div
        style={{ paddingLeft: depth * 16, cursor: 'pointer', color: '#0645ad' }}
        onClick={() => setOpen(!open)}
      >
        {open ? '📂' : '📁'} {node.name}/
      </div>
      {open && sorted.map(child => (
        <FileTreeView key={child.path || child.name} node={child} depth={depth + 1} />
      ))}
    </div>
  )
}
