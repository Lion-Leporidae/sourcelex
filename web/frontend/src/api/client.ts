import type { APIResponse, GraphData, SearchResult, FileTreeNode, FileContent, StatsData, GraphNode, GrepMatch } from './types'

const BASE = ''

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + url, init)
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(`API error ${res.status}: ${text}`)
  }
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

// ========== 多仓库 API ==========

export interface RepoInfo {
  repo_id: string
  repo_url?: string
  repo_path: string
  branch: string
  indexed_at: string
  key: string // repoID@branch
}

export async function listRepos(): Promise<RepoInfo[]> {
  const resp = await fetchJSON<{ repos: RepoInfo[] }>('/api/v1/repos')
  return resp.repos || []
}

export async function setActiveRepo(repoKey: string): Promise<void> {
  await fetchJSON('/api/v1/repos/active', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_key: repoKey }),
  })
}

export async function getActiveRepo(): Promise<string> {
  const resp = await fetchJSON<{ active_repo: string }>('/api/v1/repos/active')
  return resp.active_repo || ''
}
