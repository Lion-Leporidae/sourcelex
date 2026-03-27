import { useEffect, useState, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { getGraphData, getFileTree, searchSemantic } from '../api/client'
import type { GraphData, GraphNode, GraphEdge, FileTreeNode as FTN, SearchResult } from '../api/types'
import * as d3 from 'd3'

// ==================== 类型 ====================
interface FilterState {
  mode: 'all' | 'file' | 'node'
  filePath?: string
  nodeId?: string
}

// ==================== 主组件 ====================
export default function Explorer() {
  const [graphData, setGraphData] = useState<GraphData | null>(null)
  const [fileTree, setFileTree] = useState<FTN | null>(null)
  const [searchResults, setSearchResults] = useState<SearchResult[]>([])
  const [searchQuery, setSearchQuery] = useState('')
  const [minScore, setMinScore] = useState(0.3)
  const [filter, setFilter] = useState<FilterState>({ mode: 'all' })
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null)
  const svgRef = useRef<SVGSVGElement>(null)

  useEffect(() => {
    getGraphData().then(setGraphData).catch(() => {})
    getFileTree().then(setFileTree).catch(() => {})
  }, [])

  // 计算过滤后的图数据
  const filteredData = useCallback((): { nodes: GraphNode[]; edges: GraphEdge[] } | null => {
    if (!graphData?.nodes || !graphData?.edges) return null

    // 先按置信度过滤边
    const confEdges = graphData.edges.filter(e => (e.confidence || 0) >= minScore)

    if (filter.mode === 'all') {
      // 全局模式：只过滤边，保留边涉及的节点 + 孤立节点
      const edgeNodeIds = new Set<string>()
      confEdges.forEach(e => { edgeNodeIds.add(e.source); edgeNodeIds.add(e.target) })
      return {
        nodes: graphData.nodes.filter(n => edgeNodeIds.has(n.id)),
        edges: confEdges,
      }
    }

    if (filter.mode === 'file' && filter.filePath) {
      const fileNodeIds = new Set(graphData.nodes.filter(n => n.file_path === filter.filePath).map(n => n.id))
      const relatedEdges = confEdges.filter(e => fileNodeIds.has(e.source) || fileNodeIds.has(e.target))
      const allNodeIds = new Set(fileNodeIds)
      relatedEdges.forEach(e => { allNodeIds.add(e.source); allNodeIds.add(e.target) })
      return {
        nodes: graphData.nodes.filter(n => allNodeIds.has(n.id)),
        edges: relatedEdges,
      }
    }

    if (filter.mode === 'node' && filter.nodeId) {
      const nodeId = filter.nodeId
      const relatedEdges = confEdges.filter(e => e.source === nodeId || e.target === nodeId)
      const allNodeIds = new Set([nodeId])
      relatedEdges.forEach(e => { allNodeIds.add(e.source); allNodeIds.add(e.target) })
      return {
        nodes: graphData.nodes.filter(n => allNodeIds.has(n.id)),
        edges: relatedEdges,
      }
    }

    return { nodes: graphData.nodes, edges: confEdges }
  }, [graphData, filter, minScore])

  // 渲染图谱
  const renderGraph = useCallback(() => {
    const data = filteredData()
    if (!data || !data.nodes.length || !svgRef.current) return
    const svg = d3.select(svgRef.current)
    svg.selectAll('*').remove()

    const container = svgRef.current.parentElement!
    const width = container.clientWidth
    const height = container.clientHeight || 600

    const colorMap: Record<string, string> = { function: '#3366cc', class: '#14866d', method: '#7b1fa2' }
    const sizeMap: Record<string, number> = { function: 6, class: 10, method: 5 }

    const g = svg.append('g')
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 8])
      .on('zoom', e => g.attr('transform', e.transform))
    svg.call(zoom)

    // Defs
    const defs = svg.append('defs')
    const radGrad = defs.append('radialGradient').attr('id', 'bg-glow')
    radGrad.append('stop').attr('offset', '0%').attr('stop-color', '#e8edf2')
    radGrad.append('stop').attr('offset', '100%').attr('stop-color', '#f8f9fa')
    g.append('rect').attr('x', -4000).attr('y', -4000).attr('width', 8000).attr('height', 8000)
      .attr('fill', 'url(#bg-glow)')

    defs.append('marker').attr('id', 'arrow-conf')
      .attr('viewBox', '0 -4 8 8').attr('refX', 18).attr('refY', 0)
      .attr('markerWidth', 5).attr('markerHeight', 5).attr('orient', 'auto')
      .append('path').attr('d', 'M0,-3L7,0L0,3').attr('fill', '#999')

    // Clone data for d3 mutation
    const simNodes = data.nodes.map(n => ({ ...n }))
    const nodeIds = new Set(simNodes.map(n => n.id))
    const simEdges = data.edges
      .filter(e => nodeIds.has(e.source) && nodeIds.has(e.target))
      .map(e => ({ ...e }))

    // File map for cross-file detection
    const nodeFileMap: Record<string, string> = {}
    simNodes.forEach(n => { nodeFileMap[n.id] = n.file_path })

    // 中心节点高亮
    const focusNodeId = filter.mode === 'node' ? filter.nodeId : undefined
    const focusFilePath = filter.mode === 'file' ? filter.filePath : undefined

    // Simulation
    const simulation = d3.forceSimulation(simNodes as d3.SimulationNodeDatum[])
      .force('link', d3.forceLink(simEdges).id((d: any) => d.id).distance(80).strength(0.4))
      .force('charge', d3.forceManyBody().strength(-200))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(16))
      .force('x', d3.forceX(width / 2).strength(0.04))
      .force('y', d3.forceY(height / 2).strength(0.04))

    // Edges
    const link = g.append('g').selectAll('line').data(simEdges).join('line')
      .attr('stroke', d => {
        const conf = d.confidence || 0.5
        return conf > 0.7 ? '#666' : conf > 0.4 ? '#999' : '#ccc'
      })
      .attr('stroke-width', d => (d.confidence || 0.5) > 0.7 ? 1.5 : 0.8)
      .attr('stroke-opacity', d => Math.max(0.2, d.confidence || 0.5))
      .attr('stroke-dasharray', d => {
        const src = typeof d.source === 'string' ? d.source : (d.source as any).id
        const tgt = typeof d.target === 'string' ? d.target : (d.target as any).id
        return nodeFileMap[src] !== nodeFileMap[tgt] ? '5,3' : 'none'
      })
      .attr('marker-end', 'url(#arrow-conf)')

    // Nodes
    const node = g.append('g').selectAll('circle').data(simNodes).join('circle')
      .attr('r', d => {
        if (d.id === focusNodeId) return (sizeMap[d.type] || 6) + 4
        return sizeMap[d.type] || 6
      })
      .attr('fill', d => {
        if (d.id === focusNodeId) return '#e65100'
        if (focusFilePath && d.file_path === focusFilePath) return colorMap[d.type] || '#3366cc'
        if (focusFilePath && d.file_path !== focusFilePath) return '#aab' // 跨文件节点淡色
        return colorMap[d.type] || '#3366cc'
      })
      .attr('stroke', d => d.id === focusNodeId ? '#ff6d00' : '#fff')
      .attr('stroke-width', d => d.id === focusNodeId ? 2.5 : 1)
      .attr('cursor', 'pointer')
      .call(d3.drag<any, any>()
        .on('start', (event, d) => { if (!event.active) simulation.alphaTarget(0.3).restart(); d.fx = d.x; d.fy = d.y })
        .on('drag', (event, d) => { d.fx = event.x; d.fy = event.y })
        .on('end', (event, d) => { if (!event.active) simulation.alphaTarget(0); d.fx = null; d.fy = null })
      )

    // Labels
    const label = g.append('g').selectAll('text').data(simNodes).join('text')
      .text(d => d.name)
      .attr('font-size', d => d.id === focusNodeId ? '11px' : '9px')
      .attr('font-weight', d => d.id === focusNodeId ? '700' : '400')
      .attr('fill', d => d.id === focusNodeId ? '#e65100' : '#555')
      .attr('font-family', 'JetBrains Mono, monospace')
      .attr('text-anchor', 'middle')
      .attr('dy', d => -(sizeMap[d.type] || 6) - 5)
      .attr('pointer-events', 'none')
      .attr('opacity', d => {
        if (d.id === focusNodeId) return 1
        if (d.type === 'class') return 0.9
        if (simNodes.length < 40) return 0.7
        return 0.35
      })

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
        .style('box-shadow', '0 2px 8px rgba(0,0,0,0.12)')
        .style('z-index', '50')
        .style('pointer-events', 'none')
        .style('max-width', '320px') as any
    }

    // Hover
    node.on('mouseover', (event, d) => {
      tooltip.style('display', 'block').html(
        `<strong>${d.name}</strong><br/>` +
        `<span style="color:#72777d">${d.type} · ${d.file_path}:${d.start_line}</span>` +
        (d.signature ? `<br/><code style="font-size:11px;color:#3366cc">${d.signature}</code>` : '') +
        `<br/><span style="color:#aaa;font-size:11px">点击查看调用链</span>`
      )
      // Highlight connected
      const connected = new Set<string>()
      simEdges.forEach(e => {
        const src = typeof e.source === 'string' ? e.source : (e.source as any).id
        const tgt = typeof e.target === 'string' ? e.target : (e.target as any).id
        if (src === d.id) connected.add(tgt)
        if (tgt === d.id) connected.add(src)
      })
      node.attr('opacity', (n: any) => n.id === d.id || connected.has(n.id) ? 1 : 0.15)
      link.attr('stroke-opacity', (e: any) => {
        const src = typeof e.source === 'string' ? e.source : e.source.id
        const tgt = typeof e.target === 'string' ? e.target : e.target.id
        return src === d.id || tgt === d.id ? 0.85 : 0.03
      })
      label.attr('opacity', (n: any) => n.id === d.id || connected.has(n.id) ? 1 : 0.1)
    }).on('mousemove', event => {
      tooltip.style('left', (event.pageX + 12) + 'px').style('top', (event.pageY - 8) + 'px')
    }).on('mouseout', () => {
      tooltip.style('display', 'none')
      node.attr('opacity', 1)
      link.attr('stroke-opacity', (d: any) => Math.max(0.2, d.confidence || 0.5))
      label.attr('opacity', (d: any) => {
        if (d.id === focusNodeId) return 1
        return d.type === 'class' ? 0.9 : simNodes.length < 40 ? 0.7 : 0.35
      })
    })

    // Click: focus node
    node.on('click', (_, d) => {
      const found = graphData?.nodes?.find(n => n.id === d.id)
      if (found) {
        setSelectedNode(found)
        setFilter({ mode: 'node', nodeId: d.id })
      }
    })

    // Tick
    simulation.on('tick', () => {
      link
        .attr('x1', (d: any) => d.source.x).attr('y1', (d: any) => d.source.y)
        .attr('x2', (d: any) => d.target.x).attr('y2', (d: any) => d.target.y)
      node.attr('cx', (d: any) => d.x).attr('cy', (d: any) => d.y)
      label.attr('x', (d: any) => d.x).attr('y', (d: any) => d.y)
    })
  }, [filteredData, graphData, filter])

  useEffect(() => { renderGraph() }, [renderGraph])

  // 选中节点的调用者/被调用者
  const nodeEdgeInfo = useCallback(() => {
    if (!selectedNode || !graphData?.edges || !graphData?.nodes) return { callers: [] as GraphNode[], callees: [] as GraphNode[] }
    const nodeMap = new Map(graphData.nodes.map(n => [n.id, n]))
    const callers: GraphNode[] = []
    const callees: GraphNode[] = []
    for (const e of graphData.edges) {
      if (e.source === selectedNode.id) {
        const t = nodeMap.get(e.target)
        if (t) callees.push(t)
      }
      if (e.target === selectedNode.id) {
        const s = nodeMap.get(e.source)
        if (s) callers.push(s)
      }
    }
    return { callers, callees }
  }, [selectedNode, graphData])

  const handleSearch = async () => {
    if (!searchQuery.trim()) return
    const results = await searchSemantic(searchQuery, 10, minScore)
    setSearchResults(results)
  }

  const handleFileClick = (filePath: string) => {
    setSelectedNode(null)
    setFilter({ mode: 'file', filePath })
  }

  const handleResetFilter = () => {
    setSelectedNode(null)
    setFilter({ mode: 'all' })
  }

  const { callers, callees } = nodeEdgeInfo()
  const currentData = filteredData()
  const nodeCount = currentData?.nodes?.length ?? 0
  const edgeCount = currentData?.edges?.length ?? 0

  return (
    <div>
      <h1 className="wiki-h1">调用图谱浏览器</h1>

      {/* 搜索栏 */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 12, alignItems: 'center', flexWrap: 'wrap' }}>
        <input
          type="text"
          placeholder="搜索实体..."
          value={searchQuery}
          onChange={e => setSearchQuery(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSearch()}
          style={{ flex: 1, maxWidth: 300, padding: '6px 10px', border: '1px solid #c8ccd1', borderRadius: 4, fontFamily: 'var(--font-code)', fontSize: 13 }}
        />
        <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--wiki-text-secondary)' }}>
          置信度 ≥ {(minScore * 100).toFixed(0)}%
          <input type="range" min="0" max="0.95" step="0.05" value={minScore}
            onChange={e => setMinScore(parseFloat(e.target.value))} style={{ width: 80 }} />
        </label>
        <button onClick={handleSearch} style={{ padding: '6px 14px', background: 'var(--wiki-accent)', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer', fontSize: 13 }}>搜索</button>
      </div>

      {searchResults.length > 0 && (
        <div style={{ marginBottom: 12 }}>
          <h3 className="wiki-h3">搜索结果</h3>
          <table className="wiki-table">
            <thead><tr><th>实体</th><th>类型</th><th>文件</th><th>相似度</th></tr></thead>
            <tbody>
              {searchResults.map(r => (
                <tr key={r.entity_id} style={{ cursor: 'pointer' }} onClick={() => {
                  const n = graphData?.nodes?.find(nd => nd.id === r.entity_id)
                  if (n) { setSelectedNode(n); setFilter({ mode: 'node', nodeId: n.id }) }
                }}>
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

      {/* 过滤状态指示 + 图例 */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 8, alignItems: 'center', flexWrap: 'wrap', fontSize: 12 }}>
        {filter.mode !== 'all' && (
          <button onClick={handleResetFilter} style={{
            padding: '3px 10px', background: '#fff', border: '1px solid #c8ccd1', borderRadius: 12,
            cursor: 'pointer', fontSize: 12, color: '#333', display: 'flex', alignItems: 'center', gap: 4
          }}>
            ✕ 显示全部
          </button>
        )}
        {filter.mode === 'file' && (
          <span style={{ color: 'var(--wiki-accent)', fontFamily: 'var(--font-code)' }}>
            📄 {filter.filePath}
          </span>
        )}
        {filter.mode === 'node' && selectedNode && (
          <span style={{ color: '#e65100', fontFamily: 'var(--font-code)' }}>
            🔍 {selectedNode.name}
          </span>
        )}
        <span style={{ marginLeft: 'auto', display: 'flex', gap: 12, color: '#72777d', alignItems: 'center' }}>
          <span className="mono" style={{ fontSize: 11 }}>{nodeCount} 节点 · {edgeCount} 边</span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
            <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#3366cc', display: 'inline-block' }} />函数
          </span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
            <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#14866d', display: 'inline-block' }} />类
          </span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
            <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#7b1fa2', display: 'inline-block' }} />方法
          </span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
            <span style={{ width: 14, height: 0, borderTop: '2px dashed #999', display: 'inline-block' }} />跨文件
          </span>
        </span>
      </div>

      {/* 主布局：左图谱 + 右面板 */}
      <div style={{ display: 'flex', gap: 0, border: '1px solid #c8ccd1', borderRadius: 4, overflow: 'hidden', height: 600 }}>
        {/* 左：图谱 */}
        <div style={{ flex: 1, background: '#fafafa', position: 'relative', minWidth: 0 }}>
          <svg ref={svgRef} style={{ width: '100%', height: '100%' }} />
          {(!graphData?.nodes || graphData.nodes.length === 0) && (
            <div style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#999' }}>
              加载中...
            </div>
          )}
        </div>

        {/* 右：文件目录 / 节点详情 */}
        <div style={{
          width: 300, borderLeft: '1px solid #c8ccd1', background: '#fff',
          display: 'flex', flexDirection: 'column', flexShrink: 0
        }}>
          {/* Tab 切换 */}
          <div style={{ display: 'flex', borderBottom: '1px solid #c8ccd1' }}>
            <PanelTab
              active={!selectedNode}
              onClick={() => { setSelectedNode(null); if (filter.mode === 'node') setFilter({ mode: 'all' }) }}
              label="📁 文件目录"
            />
            {selectedNode && (
              <PanelTab active={true} onClick={() => {}} label={`🔍 ${selectedNode.name}`} />
            )}
          </div>

          <div style={{ flex: 1, overflow: 'auto', padding: '8px 0' }}>
            {selectedNode ? (
              <NodeDetailPanel
                node={selectedNode}
                callers={callers}
                callees={callees}
                graphData={graphData}
                onNodeClick={(id) => {
                  const n = graphData?.nodes?.find(nd => nd.id === id)
                  if (n) { setSelectedNode(n); setFilter({ mode: 'node', nodeId: id }) }
                }}
                onBackToFile={() => {
                  setFilter({ mode: 'file', filePath: selectedNode.file_path })
                  setSelectedNode(null)
                }}
              />
            ) : (
              <div style={{ padding: '0 8px' }}>
                {fileTree ? (
                  <FileTreeView
                    node={fileTree}
                    depth={0}
                    selectedFile={filter.mode === 'file' ? filter.filePath : undefined}
                    onFileClick={handleFileClick}
                  />
                ) : (
                  <div style={{ color: '#999', padding: 12 }}>加载中...</div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

// ==================== 面板 Tab ====================
function PanelTab({ active, onClick, label }: { active: boolean; onClick: () => void; label: string }) {
  return (
    <button
      onClick={onClick}
      style={{
        flex: 1, padding: '8px 4px', border: 'none', borderBottom: active ? '2px solid var(--wiki-accent)' : '2px solid transparent',
        background: active ? '#fff' : '#f8f9fa', cursor: 'pointer',
        fontSize: 12, fontWeight: active ? 600 : 400, color: active ? 'var(--wiki-accent)' : '#666',
        whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis'
      }}
    >
      {label}
    </button>
  )
}

// ==================== 节点详情面板 ====================
function NodeDetailPanel({ node, callers, callees, graphData, onNodeClick, onBackToFile }: {
  node: GraphNode
  callers: GraphNode[]
  callees: GraphNode[]
  graphData: GraphData | null
  onNodeClick: (id: string) => void
  onBackToFile: () => void
}) {
  // 计算该节点的边置信度
  const edgeConfMap: Record<string, number> = {}
  if (graphData?.edges) {
    for (const e of graphData.edges) {
      if (e.source === node.id) edgeConfMap[e.target] = e.confidence || 0
      if (e.target === node.id) edgeConfMap[e.source] = e.confidence || 0
    }
  }

  return (
    <div style={{ padding: '0 12px', fontSize: 13 }}>
      {/* 返回按钮 */}
      <div style={{ margin: '4px 0 8px', display: 'flex', gap: 8, fontSize: 12 }}>
        <button onClick={onBackToFile} style={{
          padding: '2px 8px', background: '#f0f0f0', border: '1px solid #ddd', borderRadius: 3,
          cursor: 'pointer', fontSize: 11, color: '#555'
        }}>
          ← 该文件全部
        </button>
      </div>

      {/* 节点信息卡 */}
      <div style={{ background: '#f8f9fa', border: '1px solid #e0e0e0', borderRadius: 4, padding: '10px 12px', marginBottom: 12 }}>
        <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 4, wordBreak: 'break-all' }}>{node.name}</div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 6 }}>
          <span className={`badge badge-${node.type}`}>{node.type}</span>
          <span className="mono text-sm muted">{node.file_path}:{node.start_line}</span>
        </div>
        <div style={{ fontSize: 11, color: '#666', marginBottom: 4 }}>
          <strong>限定名：</strong><code style={{ fontSize: 11 }}>{node.id}</code>
        </div>
        {node.signature && (
          <div style={{ marginTop: 6, padding: '6px 8px', background: '#fff', border: '1px solid #e8e8e8', borderRadius: 3, fontFamily: 'var(--font-code)', fontSize: 11, lineHeight: 1.5, wordBreak: 'break-all' }}>
            {node.signature}
          </div>
        )}
        <div style={{ marginTop: 8 }}>
          <Link to={`/entity/${encodeURIComponent(node.id)}`} style={{ fontSize: 12 }}>
            查看完整文档 →
          </Link>
        </div>
      </div>

      {/* 调用者 */}
      <div style={{ marginBottom: 10 }}>
        <div style={{ fontWeight: 600, fontSize: 12, color: '#333', marginBottom: 4, borderBottom: '1px solid #eee', paddingBottom: 2 }}>
          ← 调用者 ({callers.length})
        </div>
        {callers.length === 0 ? (
          <div className="muted text-sm" style={{ padding: '4px 0' }}>无</div>
        ) : (
          callers.map(n => (
            <NodeListItem
              key={n.id}
              node={n}
              confidence={edgeConfMap[n.id]}
              isCrossFile={n.file_path !== node.file_path}
              onClick={() => onNodeClick(n.id)}
            />
          ))
        )}
      </div>

      {/* 被调用 */}
      <div>
        <div style={{ fontWeight: 600, fontSize: 12, color: '#333', marginBottom: 4, borderBottom: '1px solid #eee', paddingBottom: 2 }}>
          → 被调用 ({callees.length})
        </div>
        {callees.length === 0 ? (
          <div className="muted text-sm" style={{ padding: '4px 0' }}>无</div>
        ) : (
          callees.map(n => (
            <NodeListItem
              key={n.id}
              node={n}
              confidence={edgeConfMap[n.id]}
              isCrossFile={n.file_path !== node.file_path}
              onClick={() => onNodeClick(n.id)}
            />
          ))
        )}
      </div>
    </div>
  )
}

// ==================== 节点列表项 ====================
function NodeListItem({ node, confidence, isCrossFile, onClick }: {
  node: GraphNode; confidence?: number; isCrossFile: boolean; onClick: () => void
}) {
  return (
    <div
      onClick={onClick}
      style={{
        padding: '4px 6px', margin: '2px 0', borderRadius: 3, cursor: 'pointer',
        display: 'flex', alignItems: 'center', gap: 6, fontSize: 12,
        background: isCrossFile ? '#fff8e1' : '#fff',
        border: '1px solid ' + (isCrossFile ? '#ffe0b2' : '#f0f0f0'),
      }}
      onMouseEnter={e => (e.currentTarget.style.background = '#e3f2fd')}
      onMouseLeave={e => (e.currentTarget.style.background = isCrossFile ? '#fff8e1' : '#fff')}
    >
      <span className={`badge badge-${node.type}`} style={{ fontSize: 10, padding: '0 4px' }}>{node.type[0]}</span>
      <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontFamily: 'var(--font-code)', fontSize: 11 }}>
        {node.name}
      </span>
      {confidence !== undefined && confidence > 0 && (
        <span style={{ fontSize: 10, color: confidence > 0.7 ? '#2e7d32' : confidence > 0.4 ? '#e65100' : '#999' }}>
          {(confidence * 100).toFixed(0)}%
        </span>
      )}
      {isCrossFile && (
        <span style={{ fontSize: 9, color: '#e65100', fontWeight: 600 }}>跨文件</span>
      )}
    </div>
  )
}

// ==================== 文件树 ====================
function FileTreeView({ node, depth, selectedFile, onFileClick }: {
  node: FTN; depth: number; selectedFile?: string; onFileClick: (path: string) => void
}) {
  const [open, setOpen] = useState(depth < 1)

  if (!node.is_dir && !node.children) {
    const isSelected = selectedFile === node.path
    return (
      <div
        style={{
          paddingLeft: depth * 14, cursor: 'pointer', lineHeight: 1.7,
          background: isSelected ? '#e3f2fd' : 'transparent',
          borderRadius: 2, padding: '1px 4px 1px ' + (depth * 14) + 'px',
          fontFamily: 'var(--font-code)', fontSize: 12,
        }}
        onClick={() => onFileClick(node.path)}
        onMouseEnter={e => { if (!isSelected) e.currentTarget.style.background = '#f0f0f0' }}
        onMouseLeave={e => { if (!isSelected) e.currentTarget.style.background = 'transparent' }}
      >
        <span style={{ opacity: 0.5, marginRight: 4 }}>📄</span>
        {node.name}
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
        style={{
          paddingLeft: depth * 14, cursor: 'pointer', color: '#333',
          fontFamily: 'var(--font-code)', fontSize: 12, lineHeight: 1.7,
          fontWeight: 600
        }}
        onClick={() => setOpen(!open)}
      >
        <span style={{ opacity: 0.6, marginRight: 4 }}>{open ? '📂' : '📁'}</span>
        {node.name}/
      </div>
      {open && sorted.map(child => (
        <FileTreeView
          key={child.path || child.name}
          node={child}
          depth={depth + 1}
          selectedFile={selectedFile}
          onFileClick={onFileClick}
        />
      ))}
    </div>
  )
}
