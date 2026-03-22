// Package graph 提供图算法工具函数
package graph

// detectCyclesDFS 使用 DFS 检测有向图中的环
func detectCyclesDFS(adjacency map[string][]string) [][]string {
	const (
		white = 0 // 未访问
		gray  = 1 // 正在访问（在当前 DFS 栈中）
		black = 2 // 已完成
	)

	color := make(map[string]int)
	parent := make(map[string]string)
	var cycles [][]string

	var dfs func(node string)
	dfs = func(node string) {
		color[node] = gray
		for _, neighbor := range adjacency[node] {
			if color[neighbor] == gray {
				// 发现环，回溯路径
				cycle := []string{neighbor, node}
				current := node
				for current != neighbor {
					current = parent[current]
					if current == "" {
						break
					}
					cycle = append(cycle, current)
				}
				if len(cycle) > 2 {
					for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
						cycle[i], cycle[j] = cycle[j], cycle[i]
					}
					cycles = append(cycles, cycle)
				}
			} else if color[neighbor] == white {
				parent[neighbor] = node
				dfs(neighbor)
			}
		}
		color[node] = black
	}

	for node := range adjacency {
		if color[node] == white {
			dfs(node)
		}
	}

	return cycles
}

// topologicalSortKahn 使用 Kahn 算法进行拓扑排序
func topologicalSortKahn(adjacency map[string][]string, allNodes map[string]bool) []string {
	inDegree := make(map[string]int)
	for node := range allNodes {
		inDegree[node] = 0
	}
	for _, neighbors := range adjacency {
		for _, n := range neighbors {
			inDegree[n]++
		}
	}

	var queue []string
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		for _, neighbor := range adjacency[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	return sorted
}

// reconstructPath 从 BFS 结果重建路径
func reconstructPath(sourceID, targetID string, parent map[string]string, parentEdge map[string]*Edge) *PathResult {
	path := []string{targetID}
	var edges []Edge

	current := targetID
	for current != sourceID {
		if edge := parentEdge[current]; edge != nil {
			edges = append(edges, *edge)
		}
		current = parent[current]
		path = append(path, current)
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	for i, j := 0, len(edges)-1; i < j; i, j = i+1, j-1 {
		edges[i], edges[j] = edges[j], edges[i]
	}

	return &PathResult{
		Path:  path,
		Edges: edges,
	}
}
