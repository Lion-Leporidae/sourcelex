// Package graph 提供图存储接口和实现
// 对应架构文档: GraphStore (图存储)
//
// 图存储用于存储代码实体之间的关系，如:
// - 函数调用关系（A 调用 B）
// - 类继承关系（A 继承 B）
// - 模块导入关系（A 导入 B）
//
// 图的基本概念:
// - 节点（Node）: 代码实体（函数、类、方法）
// - 边（Edge）: 实体之间的关系
// - 有向图: 边有方向（调用者 → 被调用者）
package graph

import (
	"context"
)

// NodeType 节点类型
type NodeType string

const (
	NodeTypeFunction NodeType = "function"
	NodeTypeClass    NodeType = "class"
	NodeTypeMethod   NodeType = "method"
	NodeTypeModule   NodeType = "module"
)

// EdgeType 边类型（关系类型）
type EdgeType string

const (
	// EdgeTypeCalls 调用关系: A calls B
	EdgeTypeCalls EdgeType = "calls"

	// EdgeTypeCalledBy 被调用关系: A is called by B
	EdgeTypeCalledBy EdgeType = "called_by"

	// EdgeTypeInherits 继承关系: A inherits from B
	EdgeTypeInherits EdgeType = "inherits"

	// EdgeTypeImports 导入关系: A imports B
	EdgeTypeImports EdgeType = "imports"

	// EdgeTypeContains 包含关系: A contains B (如类包含方法)
	EdgeTypeContains EdgeType = "contains"
)

// Node 图中的节点（代码实体）
// 对应架构文档中的 NodeInfo
type Node struct {
	// ID 节点唯一标识符（通常是完全限定名）
	ID string `json:"id"`

	// Name 节点名称
	Name string `json:"name"`

	// Type 节点类型
	Type NodeType `json:"type"`

	// FilePath 所在文件路径
	FilePath string `json:"file_path"`

	// StartLine 起始行号
	StartLine int `json:"start_line"`

	// EndLine 结束行号
	EndLine int `json:"end_line"`

	// Signature 签名（函数签名等）
	Signature string `json:"signature,omitempty"`

	// Metadata 额外元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Edge 图中的边（关系）
// 对应架构文档中的 EdgeInfo
type Edge struct {
	// Source 源节点 ID
	Source string `json:"source"`

	// Target 目标节点 ID
	Target string `json:"target"`

	// Type 关系类型
	Type EdgeType `json:"type"`

	// Metadata 额外元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PathResult 路径查询结果
type PathResult struct {
	// Path 路径上的节点 ID 列表
	Path []string `json:"path"`

	// Edges 路径上的边
	Edges []Edge `json:"edges"`
}

// Store 图存储接口
// 定义了图数据库的基本操作
// 实现可以是内存存储、NetworkX、Cayley、DGraph 等
type Store interface {
	// AddNode 添加节点
	AddNode(ctx context.Context, node Node) error

	// AddNodes 批量添加节点
	AddNodes(ctx context.Context, nodes []Node) error

	// AddEdge 添加边（关系）
	AddEdge(ctx context.Context, edge Edge) error

	// AddEdges 批量添加边
	AddEdges(ctx context.Context, edges []Edge) error

	// GetNode 获取节点
	GetNode(ctx context.Context, id string) (*Node, error)

	// GetNeighbors 获取邻居节点
	// direction: "in"（入边）, "out"（出边）, "both"（双向）
	GetNeighbors(ctx context.Context, id string, direction string, edgeTypes []EdgeType) ([]Node, error)

	// GetCallersOf 获取调用者（谁调用了这个函数）
	// 即获取所有指向此节点的 "calls" 边的源节点
	GetCallersOf(ctx context.Context, id string, depth int) ([]Node, error)

	// GetCalleesOf 获取被调用者（这个函数调用了谁）
	// 即获取所有从此节点出发的 "calls" 边的目标节点
	GetCalleesOf(ctx context.Context, id string, depth int) ([]Node, error)

	// FindPath 查找两个节点之间的路径
	FindPath(ctx context.Context, sourceID, targetID string) (*PathResult, error)

	// DeleteNode 删除节点及其所有边
	DeleteNode(ctx context.Context, id string) error

	// NodeCount 返回节点数量
	NodeCount(ctx context.Context) (int64, error)

	// EdgeCount 返回边数量
	EdgeCount(ctx context.Context) (int64, error)

	// Clear 清空所有数据
	Clear(ctx context.Context) error

	// Close 关闭连接
	Close() error
}
