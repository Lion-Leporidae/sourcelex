import { useEffect, useRef } from 'react'
import * as d3 from 'd3'
import type { GraphNode } from '../api/types'

interface StarGraphProps {
  centerId: string
  centerNode: GraphNode
  callers: GraphNode[]
  callees: GraphNode[]
  onNodeClick?: (id: string) => void
}

const colorMap: Record<string, string> = { function: '#1565c0', class: '#2e7d32', method: '#7b1fa2' }

export default function StarGraph({ centerId, centerNode, callers, callees, onNodeClick }: StarGraphProps) {
  const svgRef = useRef<SVGSVGElement>(null)

  useEffect(() => {
    if (!svgRef.current) return
    const svg = d3.select(svgRef.current)
    svg.selectAll('*').remove()

    const width = svgRef.current.clientWidth || 600
    const height = 420
    const cx = width / 2, cy = height / 2

    const g = svg.append('g')

    // Zoom
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.3, 3])
      .on('zoom', e => g.attr('transform', e.transform))
    svg.call(zoom)

    // Build nodes and links
    type SimNode = d3.SimulationNodeDatum & { id: string; name: string; nodeType: string; group: 'center' | 'caller' | 'callee'; file_path: string }

    const nodes: SimNode[] = [
      { id: centerId, name: centerNode.name, nodeType: centerNode.type, group: 'center', file_path: centerNode.file_path }
    ]

    const links: { source: string; target: string }[] = []
    const seen = new Set<string>([centerId])

    callers.forEach(n => {
      if (seen.has(n.id)) return
      seen.add(n.id)
      nodes.push({ id: n.id, name: n.name, nodeType: n.type, group: 'caller', file_path: n.file_path })
      links.push({ source: n.id, target: centerId })
    })

    callees.forEach(n => {
      if (seen.has(n.id)) return
      seen.add(n.id)
      nodes.push({ id: n.id, name: n.name, nodeType: n.type, group: 'callee', file_path: n.file_path })
      links.push({ source: centerId, target: n.id })
    })

    // Fix center node
    const centerSim = nodes[0]
    centerSim.fx = cx
    centerSim.fy = cy

    // Radial force: callers upper half, callees lower half
    const radialForce = () => {
      const strength = 0.08
      nodes.forEach(n => {
        if (n.group === 'center') return
        const angle = n.group === 'caller'
          ? -Math.PI / 2 + (Math.random() - 0.5) * Math.PI
          : Math.PI / 2 + (Math.random() - 0.5) * Math.PI
        const radius = 140 + Math.random() * 60
        const tx = cx + Math.cos(angle) * radius
        const ty = cy + Math.sin(angle) * radius
        n.vx = ((n.vx || 0) + (tx - (n.x || cx)) * strength)
        n.vy = ((n.vy || 0) + (ty - (n.y || cy)) * strength)
      })
    }

    // Simulation
    const simulation = d3.forceSimulation(nodes)
      .force('link', d3.forceLink(links).id((d: any) => d.id).distance(120).strength(0.5))
      .force('charge', d3.forceManyBody().strength(-300))
      .force('collision', d3.forceCollide().radius(30))
      .force('radial', radialForce as any)

    // Defs for glow
    const defs = svg.append('defs')
    const filter = defs.append('filter').attr('id', 'glow')
    filter.append('feGaussianBlur').attr('stdDeviation', 3).attr('result', 'blur')
    filter.append('feMerge').selectAll('feMergeNode')
      .data(['blur', 'SourceGraphic']).join('feMergeNode')
      .attr('in', (d: string) => d)

    // Arrow
    defs.append('marker').attr('id', 'star-arrow')
      .attr('viewBox', '0 -5 10 10').attr('refX', 18).attr('refY', 0)
      .attr('markerWidth', 6).attr('markerHeight', 6).attr('orient', 'auto')
      .append('path').attr('d', 'M0,-3L8,0L0,3').attr('fill', '#999')

    // Links
    const link = g.append('g').selectAll('line').data(links).join('line')
      .attr('stroke', '#bbb').attr('stroke-width', 1)
      .attr('stroke-dasharray', d => {
        const src = nodes.find(n => n.id === (typeof d.source === 'string' ? d.source : (d.source as any).id))
        const tgt = nodes.find(n => n.id === (typeof d.target === 'string' ? d.target : (d.target as any).id))
        return src && tgt && src.file_path !== tgt.file_path ? '5,3' : 'none'
      })
      .attr('marker-end', 'url(#star-arrow)')

    // Nodes
    const node = g.append('g').selectAll('circle').data(nodes).join('circle')
      .attr('r', d => d.group === 'center' ? 16 : 8)
      .attr('fill', d => colorMap[d.nodeType] || '#1565c0')
      .attr('stroke', d => d.group === 'center' ? '#ffd700' : '#fff')
      .attr('stroke-width', d => d.group === 'center' ? 3 : 1.5)
      .attr('cursor', 'pointer')
      .attr('filter', d => d.group === 'center' ? 'url(#glow)' : '')
      .on('click', (_, d) => onNodeClick?.(d.id))
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      .call(d3.drag<any, SimNode>()
        .on('start', (event, d) => { if (!event.active) simulation.alphaTarget(0.3).restart(); d.fx = d.x; d.fy = d.y })
        .on('drag', (event, d) => { d.fx = event.x; d.fy = event.y })
        .on('end', (event, d) => { if (!event.active) simulation.alphaTarget(0); if (d.group !== 'center') { d.fx = null; d.fy = null } })
      )

    // Labels
    const label = g.append('g').selectAll('text').data(nodes).join('text')
      .text(d => d.name)
      .attr('font-size', d => d.group === 'center' ? '12px' : '10px')
      .attr('font-weight', d => d.group === 'center' ? '700' : '400')
      .attr('fill', '#202122')
      .attr('font-family', 'JetBrains Mono, monospace')
      .attr('text-anchor', 'middle')
      .attr('dy', d => d.group === 'center' ? -22 : -12)
      .attr('pointer-events', 'none')

    // Group labels
    g.append('text').attr('x', cx).attr('y', 30).attr('text-anchor', 'middle')
      .attr('fill', '#72777d').attr('font-size', '11px').text(`▲ 调用者 (${callers.length})`)
    g.append('text').attr('x', cx).attr('y', height - 15).attr('text-anchor', 'middle')
      .attr('fill', '#72777d').attr('font-size', '11px').text(`▼ 被调用 (${callees.length})`)

    // Tick
    simulation.on('tick', () => {
      link.attr('x1', (d: any) => d.source.x).attr('y1', (d: any) => d.source.y)
        .attr('x2', (d: any) => d.target.x).attr('y2', (d: any) => d.target.y)
      node.attr('cx', (d: any) => d.x).attr('cy', (d: any) => d.y)
      label.attr('x', (d: any) => d.x).attr('y', (d: any) => d.y)
    })

    return () => { simulation.stop() }
  }, [centerId, centerNode, callers, callees, onNodeClick])

  return (
    <div style={{ border: '1px solid #c8ccd1', borderRadius: 4, background: '#fafafa', overflow: 'hidden' }}>
      <svg ref={svgRef} style={{ width: '100%', height: 420 }} />
    </div>
  )
}
