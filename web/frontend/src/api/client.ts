import type { APIResponse, GraphData, SearchResult, FileTreeNode, FileContent, StatsData, GraphNode, GrepMatch } from './types'

const BASE = ''

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + url, init)
  const json = await res.json()
  return json as T
}

export async function getStats(): Promise<StatsData> {
  return fetchJSON<StatsData>('/agent/stats')
}

export async function getGraphData(): Promise<GraphData> {
  return fetchJSON<GraphData>('/agent/graph/data')
}

export async function searchSemantic(query: string, topK = 8, minScore = 0): Promise<SearchResult[]> {
  const body: Record<string, unknown> = { query, top_k: topK }
  if (minScore > 0) body.min_score = minScore
  const resp = await fetchJSON<APIResponse<SearchResult[]>>('/api/v1/search/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return resp.data || []
}

export async function getFileTree(): Promise<FileTreeNode> {
  const resp = await fetchJSON<APIResponse<FileTreeNode>>('/api/v1/file/tree')
  return resp.data!
}

export async function getFileLines(path: string, start?: number, end?: number): Promise<FileContent> {
  let url = `/api/v1/file/lines?path=${encodeURIComponent(path)}`
  if (start) url += `&start=${start}`
  if (end) url += `&end=${end}`
  const resp = await fetchJSON<APIResponse<FileContent>>(url)
  return resp.data!
}

export async function getCallers(entityId: string, depth = 2): Promise<GraphNode[]> {
  const resp = await fetchJSON<APIResponse<GraphNode[]>>(
    `/api/v1/callers/${encodeURIComponent(entityId)}?depth=${depth}`
  )
  return resp.data || []
}

export async function getCallees(entityId: string, depth = 2): Promise<GraphNode[]> {
  const resp = await fetchJSON<APIResponse<GraphNode[]>>(
    `/api/v1/callees/${encodeURIComponent(entityId)}?depth=${depth}`
  )
  return resp.data || []
}

export async function grepCode(pattern: string, filePattern?: string): Promise<GrepMatch[]> {
  const resp = await fetchJSON<APIResponse<{ matches: GrepMatch[] }>>('/api/v1/grep', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pattern, file_pattern: filePattern, max_results: 50 }),
  })
  return resp.data?.matches || []
}
