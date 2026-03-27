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
  const [minScore, setMinScore] = useState(0.3)
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
    const height = container.clientHeight || 500

    // Color & size
    const colorMap: Record<string, string> = { function: '#3366cc', class: '#14866d', method: '#7b1fa2' }
    const sizeMap: Record<string, number> = { function: 5, class: 9, method: 4 }

    // Build file color palette (each file gets a soft hue)
    const files = [...new Set(graphData.nodes.map(n => n.file_path))]
    const fileColor: Record<string, string> = {}
    files.forEach((f, i) => {
      const hue = (i * 137.5) % 360 // golden angle distribution
      fileColor[f] = `hsla(${hue}, 40%, 85%, 0.3)`
    })

    const g = svg.append('g')
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 6])
      .on('zoom', e => g.attr('transform', e.transform))
    svg.call(zoom)

    // Background gradient
    const defs = svg.append('defs')
    const radGrad = defs.append('radialGradient').attr('id', 'bg-glow')
    radGrad.append('stop').attr('offset', '0%').attr('stop-color', '#e8edf2')
    radGrad.append('stop').attr('offset', '100%').attr('stop-color', '#f8f9fa')
    g.append('rect').attr('x', -2000).attr('y', -2000).attr('width', 4000).attr('height', 4000)
      .attr('fill', 'url(#bg-glow)')

    // Arrow
    defs.append('marker').attr('id', 'arrow-conf')
      .attr('viewBox', '0 -4 8 8').attr('refX', 16).attr('refY', 0)
      .attr('markerWidth', 5).attr('markerHeight', 5).attr('orient', 'auto')
      .append('path').attr('d', 'M0,-3L7,0L0,3').attr('fill', '#999')

    // Prepare data
    const nodeIds = new Set(graphData.nodes.map(n => n.id))
    const validEdges = graphData.edges.filter(e => nodeIds.has(e.source) && nodeIds.has(e.target))

    // Force simulation — galaxy style
    const simulation = d3.forceSimulation(graphData.nodes as d3.SimulationNodeDatum[])
      .force('link', d3.forceLink(validEdges).id((d: any) => d.id).distance(60).strength(0.3))
      .force('charge', d3.forceManyBody().strength(-120))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(12))
      .force('x', d3.forceX(width / 2).strength(0.03))
      .force('y', d3.forceY(height / 2).strength(0.03))

    // Edges — opacity by confidence, dashed if cross-file
    const nodeFileMap: Record<string, string> = {}
    graphData.nodes.forEach(n => { nodeFileMap[n.id] = n.file_path })

    const link = g.append('g').selectAll('line').data(validEdges).join('line')
      .attr('stroke', d => {
        const conf = d.confidence || 0.5
        return conf > 0.7 ? '#666' : conf > 0.4 ? '#999' : '#ccc'
      })
      .attr('stroke-width', d => {
        const conf = d.confidence || 0.5
        return conf > 0.7 ? 1.2 : 0.7
      })
      .attr('stroke-opacity', d => Math.max(0.15, (d.confidence || 0.5)))
      .attr('stroke-dasharray', d => {
        const src = typeof d.source === 'string' ? d.source : (d.source as any).id
        const tgt = typeof d.target === 'string' ? d.target : (d.target as any).id
        return nodeFileMap[src] !== nodeFileMap[tgt] ? '4,3' : 'none'
      })
      .attr('marker-end', 'url(#arrow-conf)')

    // File background clusters (soft colored regions)
    // We'll update these on tick

    // Nodes
    const node = g.append('g').selectAll('circle').data(graphData.nodes).join('circle')
      .attr('r', d => sizeMap[d.type] || 5)
      .attr('fill', d => colorMap[d.type] || '#3366cc')
      .attr('stroke', d => fileColor[d.file_path] ? '#fff' : '#eee')
      .attr('stroke-width', 1)
      .attr('cursor', 'pointer')
      .call(d3.drag<any, any>()
        .on('start', (event, d) => { if (!event.active) simulation.alphaTarget(0.3).restart(); d.fx = d.x; d.fy = d.y })
        .on('drag', (event, d) => { d.fx = event.x; d.fy = event.y })
        .on('end', (event, d) => { if (!event.active) simulation.alphaTarget(0); d.fx = null; d.fy = null })
      )

    // Labels (only show for larger nodes or on hover)
    const label = g.append('g').selectAll('text').data(graphData.nodes).join('text')
      .text(d => d.name)
      .attr('font-size', '9px')
      .attr('fill', '#555')
      .attr('font-family', 'JetBrains Mono, monospace')
      .attr('text-anchor', 'middle')
      .attr('dy', d => -(sizeMap[d.type] || 5) - 4)
      .attr('pointer-events', 'none')
      .attr('opacity', d => d.type === 'class' ? 0.9 : 0.4)

    // Tooltip
    let tooltip = d3.select('.graph-tooltip') as any
    if (tooltip.empty()) {
      tooltip = d3.select('body').append('div')
        .attr('class', 'graph-tooltip')
        .style('display', 'none')
        .style('position', 'absolute')
        .style('background', '#fff')
        .style('border', '1px solid #c8ccd1')
        .style('border-radius', '4px')
        .style('padding', '8px 12px')
        .style('font-size', '12px')
        .style('box-shadow', '0 2px 8px rgba(0,0,0,0.1)')
        .style('z-index', '50')
        .style('pointer-events', 'none')
        .style('max-width', '300px') as any
    }

    node.on('mouseover', (event, d) => {
      tooltip.style('display', 'block').html(
        `<strong>${d.name}</strong><br/>` +
        `<span style="color:#72777d">${d.type} · ${d.file_path}:${d.start_line}</span>` +
        (d.signature ? `<br/><code style="font-size:11px;color:#3366cc">${d.signature}</code>` : '')
      )
      // Highlight connected
      d3.selectAll('circle').attr('opacity', 0.15)
      d3.select(event.target).attr('opacity', 1).attr('r', (sizeMap[d.type] || 5) + 3)
      const connected = new Set<string>()
      validEdges.forEach(e => {
        const src = typeof e.source === 'string' ? e.source : (e.source as any).id
        const tgt = typeof e.target === 'string' ? e.target : (e.target as any).id
        if (src === d.id) connected.add(tgt)
        if (tgt === d.id) connected.add(src)
      })
      d3.selectAll('circle').filter((n: any) => connected.has(n.id)).attr('opacity', 1)
      link.attr('stroke-opacity', (e: any) => {
        const src = typeof e.source === 'string' ? e.source : e.source.id
        const tgt = typeof e.target === 'string' ? e.target : e.target.id
        return src === d.id || tgt === d.id ? 0.8 : 0.03
      })
      label.attr('opacity', (n: any) => n.id === d.id || connected.has(n.id) ? 1 : 0.1)
    }).on('mousemove', event => {
      tooltip.style('left', (event.pageX + 12) + 'px').style('top', (event.pageY - 8) + 'px')
    }).on('mouseout', () => {
      tooltip.style('display', 'none')
      d3.selectAll('circle').attr('opacity', 1).attr('r', (d: any) => sizeMap[d.type] || 5)
      link.attr('stroke-opacity', (d: any) => Math.max(0.15, (d.confidence || 0.5)))
      label.attr('opacity', (d: any) => d.type === 'class' ? 0.9 : 0.4)
    })

    node.on('click', (_, d) => {
      window.location.href = `/entity/${encodeURIComponent(d.id)}`
    })

    // Tick
    simulation.on('tick', () => {
      link
        .attr('x1', (d: any) => d.source.x).attr('y1', (d: any) => d.source.y)
        .attr('x2', (d: any) => d.target.x).attr('y2', (d: any) => d.target.y)
      node.attr('cx', (d: any) => d.x).attr('cy', (d: any) => d.y)
      label.attr('x', (d: any) => d.x).attr('y', (d: any) => d.y)
    })
  }, [graphData])

  useEffect(() => { renderGraph() }, [renderGraph])

  const handleSearch = async () => {
    if (!searchQuery.trim()) return
    const results = await searchSemantic(searchQuery, 10, minScore)
    setSearchResults(results)
  }

  return (
    <div>
      <h1 className="wiki-h1">调用图谱浏览器</h1>

      <div style={{ display: 'flex', gap: 12, marginBottom: 16, alignItems: 'center', flexWrap: 'wrap' }}>
        <input
          type="text"
          placeholder="搜索实体..."
          value={searchQuery}
          onChange={e => setSearchQuery(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSearch()}
          style={{ flex: 1, maxWidth: 320, padding: '6px 10px', border: '1px solid #c8ccd1', borderRadius: 4, fontFamily: 'var(--font-code)', fontSize: 13 }}
        />
        <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--wiki-text-secondary)' }}>
          置信度 ≥ {(minScore * 100).toFixed(0)}%
          <input
            type="range"
            min="0" max="0.95" step="0.05"
            value={minScore}
            onChange={e => setMinScore(parseFloat(e.target.value))}
            style={{ width: 100 }}
          />
        </label>
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
      <div style={{ display: 'flex', gap: 16, fontSize: 12, color: '#72777d', marginBottom: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#3366cc', display: 'inline-block' }} />函数
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#14866d', display: 'inline-block' }} />类
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#7b1fa2', display: 'inline-block' }} />方法
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 16, height: 0, borderTop: '2px dashed #999', display: 'inline-block' }} />跨文件调用
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 16, height: 2, background: '#666', display: 'inline-block' }} />高置信度
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ width: 16, height: 1, background: '#ccc', display: 'inline-block' }} />低置信度
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
