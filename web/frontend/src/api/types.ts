// API response types for Sourcelex backend

export interface APIResponse<T = unknown> {
  success: boolean
  data?: T
  error?: string
}

export interface GraphNode {
  id: string
  name: string
  type: 'function' | 'class' | 'method'
  file_path: string
  start_line: number
  end_line: number
  signature?: string
}

export interface GraphEdge {
  source: string
  target: string
  type: string
  source_file?: string
  line?: number
  confidence?: number
}

export interface GraphData {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export interface SearchResult {
  entity_id: string
  type: string
  file_path: string
  score: number
  content?: string
  start_line?: number
  end_line?: number
}

export interface FileTreeNode {
  name: string
  path: string
  is_dir: boolean
  children?: FileTreeNode[]
}

export interface FileContent {
  path: string
  start_line: number
  end_line: number
  total_lines: number
  content: string
  lines: string[]
}

export interface StatsData {
  node_count: number
  edge_count: number
  vector_count?: number
}

export interface GrepMatch {
  file_path: string
  line_number: number
  content: string
}
