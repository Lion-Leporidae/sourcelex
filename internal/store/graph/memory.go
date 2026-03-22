// Package graph 提供内存图存储实现
// 对应架构文档: NetworkX (DiGraph)
//
// 内存图存储使用 Go 的 map 结构实现有向图
// 适用于中小型代码库，数据存储在内存中
//
// 数据结构:
// - nodes: map[id]*Node 存储所有节点
// - outEdges: map[srcID][]Edge 存储出边（从 src 出发的边）
// - inEdges: map[dstID][]Edge 存储入边（指向 dst 的边）
//
// 这种结构支持快速的:
// - 节点查找: O(1)
// - 邻居遍历: O(degree)
// - 调用链追踪: O(depth * avgDegree)
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// MemoryStore 内存图存储实现
// 使用 map 存储节点和边，读写锁保证并发安全
type MemoryStore struct {
	// mu 读写锁，保护并发访问
	mu sync.RWMutex

	// nodes 节点存储: id -> Node
	nodes map[string]*Node

	// outEdges 出边存储: srcID -> []Edge
	// 用于快速查找"这个节点调用了谁"
	outEdges map[string][]Edge

	// inEdges 入边存储: dstID -> []Edge
	// 用于快速查找"谁调用了这个节点"
	inEdges map[string][]Edge
}

// NewMemoryStore 创建内存图存储
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nodes:    make(map[string]*Node),
		outEdges: make(map[string][]Edge),
		inEdges:  make(map[string][]Edge),
	}
}

// AddNode 添加节点
// 如果节点已存在，将被覆盖
func (s *MemoryStore) AddNode(ctx context.Context, node Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 存储节点（复制一份以避免外部修改）
	nodeCopy := node
	s.nodes[node.ID] = &nodeCopy

	return nil
}

// AddNodes 批量添加节点
func (s *MemoryStore) AddNodes(ctx context.Context, nodes []Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, node := range nodes {
		nodeCopy := node
		s.nodes[node.ID] = &nodeCopy
	}

	return nil
}

// AddEdge 添加边
// 边会同时添加到 outEdges 和 inEdges
func (s *MemoryStore) AddEdge(ctx context.Context, edge Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 添加到出边列表
	s.outEdges[edge.Source] = append(s.outEdges[edge.Source], edge)

	// 添加到入边列表
	s.inEdges[edge.Target] = append(s.inEdges[edge.Target], edge)

	return nil
}

// AddEdges 批量添加边
func (s *MemoryStore) AddEdges(ctx context.Context, edges []Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, edge := range edges {
		s.outEdges[edge.Source] = append(s.outEdges[edge.Source], edge)
		s.inEdges[edge.Target] = append(s.inEdges[edge.Target], edge)
	}

	return nil
}

// GetNode 获取节点
func (s *MemoryStore) GetNode(ctx context.Context, id string) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, ok := s.nodes[id]
	if !ok {
		return nil, fmt.Errorf("节点不存在: %s", id)
	}

	// 返回副本
	nodeCopy := *node
	return &nodeCopy, nil
}

// GetNeighbors 获取邻居节点
// direction: "in", "out", "both"
func (s *MemoryStore) GetNeighbors(ctx context.Context, id string, direction string, edgeTypes []EdgeType) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	neighborIDs := make(map[string]bool)

	// 收集邻居 ID
	if direction == "out" || direction == "both" {
		for _, edge := range s.outEdges[id] {
			if len(edgeTypes) == 0 || containsEdgeType(edgeTypes, edge.Type) {
				neighborIDs[edge.Target] = true
			}
		}
	}

	if direction == "in" || direction == "both" {
		for _, edge := range s.inEdges[id] {
			if len(edgeTypes) == 0 || containsEdgeType(edgeTypes, edge.Type) {
				neighborIDs[edge.Source] = true
			}
		}
	}

	// 收集节点
	neighbors := make([]Node, 0, len(neighborIDs))
	for nid := range neighborIDs {
		if node, ok := s.nodes[nid]; ok {
			neighbors = append(neighbors, *node)
		}
	}

	return neighbors, nil
}

// GetCallersOf 获取调用者（向上追溯）
// 使用 BFS 遍历入边，找到所有调用此函数的函数
func (s *MemoryStore) GetCallersOf(ctx context.Context, id string, depth int) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// BFS 遍历
	visited := make(map[string]bool)
	visited[id] = true

	currentLevel := []string{id}
	var callers []Node

	for d := 0; d < depth && len(currentLevel) > 0; d++ {
		nextLevel := []string{}

		for _, nodeID := range currentLevel {
			// 遍历入边（谁调用了这个节点）
			for _, edge := range s.inEdges[nodeID] {
				if edge.Type == EdgeTypeCalls && !visited[edge.Source] {
					visited[edge.Source] = true
					nextLevel = append(nextLevel, edge.Source)

					if node, ok := s.nodes[edge.Source]; ok {
						callers = append(callers, *node)
					}
				}
			}
		}

		currentLevel = nextLevel
	}

	return callers, nil
}

// GetCalleesOf 获取被调用者（向下追踪）
// 使用 BFS 遍历出边，找到所有被此函数调用的函数
func (s *MemoryStore) GetCalleesOf(ctx context.Context, id string, depth int) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// BFS 遍历
	visited := make(map[string]bool)
	visited[id] = true

	currentLevel := []string{id}
	var callees []Node

	for d := 0; d < depth && len(currentLevel) > 0; d++ {
		nextLevel := []string{}

		for _, nodeID := range currentLevel {
			// 遍历出边（这个节点调用了谁）
			for _, edge := range s.outEdges[nodeID] {
				if edge.Type == EdgeTypeCalls && !visited[edge.Target] {
					visited[edge.Target] = true
					nextLevel = append(nextLevel, edge.Target)

					if node, ok := s.nodes[edge.Target]; ok {
						callees = append(callees, *node)
					}
				}
			}
		}

		currentLevel = nextLevel
	}

	return callees, nil
}

// FindPath 查找两个节点之间的最短路径
// 使用 BFS 算法
func (s *MemoryStore) FindPath(ctx context.Context, sourceID, targetID string) (*PathResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.nodes[sourceID]; !ok {
		return nil, fmt.Errorf("源节点不存在: %s", sourceID)
	}
	if _, ok := s.nodes[targetID]; !ok {
		return nil, fmt.Errorf("目标节点不存在: %s", targetID)
	}

	if sourceID == targetID {
		return &PathResult{Path: []string{sourceID}}, nil
	}

	// BFS 查找最短路径
	visited := make(map[string]bool)
	parent := make(map[string]string)    // 记录父节点
	parentEdge := make(map[string]*Edge) // 记录到达此节点的边

	queue := []string{sourceID}
	visited[sourceID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// 遍历出边
		for _, edge := range s.outEdges[current] {
			if !visited[edge.Target] {
				visited[edge.Target] = true
				parent[edge.Target] = current
				edgeCopy := edge
				parentEdge[edge.Target] = &edgeCopy
				queue = append(queue, edge.Target)

				if edge.Target == targetID {
					// 找到目标，重建路径
					return s.reconstructPath(sourceID, targetID, parent, parentEdge), nil
				}
			}
		}
	}

	return nil, fmt.Errorf("没有找到从 %s 到 %s 的路径", sourceID, targetID)
}

// reconstructPath 重建路径
func (s *MemoryStore) reconstructPath(sourceID, targetID string, parent map[string]string, parentEdge map[string]*Edge) *PathResult {
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

	// 反转路径和边
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

// DeleteNode 删除节点及其所有边
func (s *MemoryStore) DeleteNode(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 删除节点
	delete(s.nodes, id)

	// 删除出边
	delete(s.outEdges, id)

	// 从其他节点的入边中删除指向此节点的边
	for srcID, edges := range s.outEdges {
		filtered := edges[:0]
		for _, edge := range edges {
			if edge.Target != id {
				filtered = append(filtered, edge)
			}
		}
		s.outEdges[srcID] = filtered
	}

	// 删除入边
	delete(s.inEdges, id)

	// 从其他节点的出边中删除来自此节点的边
	for dstID, edges := range s.inEdges {
		filtered := edges[:0]
		for _, edge := range edges {
			if edge.Source != id {
				filtered = append(filtered, edge)
			}
		}
		s.inEdges[dstID] = filtered
	}

	return nil
}

// NodeCount 返回节点数量
func (s *MemoryStore) NodeCount(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.nodes)), nil
}

// EdgeCount 返回边数量
func (s *MemoryStore) EdgeCount(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, edges := range s.outEdges {
		count += len(edges)
	}
	return int64(count), nil
}

// Clear 清空所有数据
func (s *MemoryStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nodes = make(map[string]*Node)
	s.outEdges = make(map[string][]Edge)
	s.inEdges = make(map[string][]Edge)

	return nil
}

// Close 关闭（内存存储无需关闭）
func (s *MemoryStore) Close() error {
	return nil
}

// SaveToFile 保存到文件（JSON 格式）
func (s *MemoryStore) SaveToFile(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := struct {
		Nodes []Node `json:"nodes"`
		Edges []Edge `json:"edges"`
	}{
		Nodes: make([]Node, 0, len(s.nodes)),
		Edges: make([]Edge, 0),
	}

	for _, node := range s.nodes {
		data.Nodes = append(data.Nodes, *node)
	}

	for _, edges := range s.outEdges {
		data.Edges = append(data.Edges, edges...)
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	return os.WriteFile(path, jsonData, 0644)
}

// LoadFromFile 从文件加载
func (s *MemoryStore) LoadFromFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jsonData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	var data struct {
		Nodes []Node `json:"nodes"`
		Edges []Edge `json:"edges"`
	}

	if err := json.Unmarshal(jsonData, &data); err != nil {
		return fmt.Errorf("解析失败: %w", err)
	}

	// 清空现有数据
	s.nodes = make(map[string]*Node)
	s.outEdges = make(map[string][]Edge)
	s.inEdges = make(map[string][]Edge)

	// 添加节点
	for _, node := range data.Nodes {
		nodeCopy := node
		s.nodes[node.ID] = &nodeCopy
	}

	// 添加边
	for _, edge := range data.Edges {
		s.outEdges[edge.Source] = append(s.outEdges[edge.Source], edge)
		s.inEdges[edge.Target] = append(s.inEdges[edge.Target], edge)
	}

	return nil
}

// GetAllNodes 获取所有节点
func (s *MemoryStore) GetAllNodes(ctx context.Context) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodes := make([]Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodes = append(nodes, *node)
	}
	return nodes, nil
}

// GetAllEdges 获取所有边
func (s *MemoryStore) GetAllEdges(ctx context.Context) ([]Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var edges []Edge
	for _, edgeList := range s.outEdges {
		edges = append(edges, edgeList...)
	}
	return edges, nil
}

// GetSubgraph 获取以指定节点为中心的子图
func (s *MemoryStore) GetSubgraph(ctx context.Context, id string, depth int) (*SubgraphResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	visited := make(map[string]bool)
	queue := []string{id}
	visited[id] = true

	for d := 0; d < depth && len(queue) > 0; d++ {
		var nextQueue []string
		for _, nodeID := range queue {
			for _, edge := range s.outEdges[nodeID] {
				if !visited[edge.Target] {
					visited[edge.Target] = true
					nextQueue = append(nextQueue, edge.Target)
				}
			}
			for _, edge := range s.inEdges[nodeID] {
				if !visited[edge.Source] {
					visited[edge.Source] = true
					nextQueue = append(nextQueue, edge.Source)
				}
			}
		}
		queue = nextQueue
	}

	var nodes []Node
	for nodeID := range visited {
		if node, ok := s.nodes[nodeID]; ok {
			nodes = append(nodes, *node)
		}
	}

	var edges []Edge
	for _, edgeList := range s.outEdges {
		for _, e := range edgeList {
			if visited[e.Source] && visited[e.Target] {
				edges = append(edges, e)
			}
		}
	}

	return &SubgraphResult{
		Nodes:    nodes,
		Edges:    edges,
		CenterID: id,
		Depth:    depth,
	}, nil
}

// GetNodesByFile 获取指定文件中的所有节点
func (s *MemoryStore) GetNodesByFile(ctx context.Context, filePath string) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var nodes []Node
	for _, node := range s.nodes {
		if node.FilePath == filePath {
			nodes = append(nodes, *node)
		}
	}
	return nodes, nil
}

// GetNodesByType 获取指定类型的所有节点
func (s *MemoryStore) GetNodesByType(ctx context.Context, nodeType NodeType) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var nodes []Node
	for _, node := range s.nodes {
		if node.Type == nodeType {
			nodes = append(nodes, *node)
		}
	}
	return nodes, nil
}

// DetectCycles 检测调用图中的环
func (s *MemoryStore) DetectCycles(ctx context.Context) ([][]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	adjacency := make(map[string][]string)
	for src, edges := range s.outEdges {
		for _, e := range edges {
			if e.Type == EdgeTypeCalls {
				adjacency[src] = append(adjacency[src], e.Target)
			}
		}
	}

	return detectCyclesDFS(adjacency), nil
}

// TopologicalSort 对调用图进行拓扑排序
func (s *MemoryStore) TopologicalSort(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	adjacency := make(map[string][]string)
	allNodes := make(map[string]bool)
	for src, edges := range s.outEdges {
		for _, e := range edges {
			if e.Type == EdgeTypeCalls {
				adjacency[src] = append(adjacency[src], e.Target)
				allNodes[src] = true
				allNodes[e.Target] = true
			}
		}
	}

	return topologicalSortKahn(adjacency, allNodes), nil
}

// containsEdgeType 检查边类型是否在列表中
func containsEdgeType(types []EdgeType, t EdgeType) bool {
	for _, et := range types {
		if et == t {
			return true
		}
	}
	return false
}
