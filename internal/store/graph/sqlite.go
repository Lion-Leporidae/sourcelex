// Package graph 提供 SQLite 图存储实现
// 使用 SQLite 存储代码实体和调用关系，便于人工检查和调试
//
// 表结构:
//   - entities: 存储代码实体（函数、类、方法等）
//   - relationships: 存储调用关系和继承关系
//
// 使用 modernc.org/sqlite（纯 Go 实现，无 CGO 依赖）
package graph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // SQLite 驱动（纯 Go 实现）
)

// SQLiteStore 是 SQLite 图存储的实现
// 将代码实体和关系存储到 SQLite 数据库中
type SQLiteStore struct {
	// db 数据库连接
	db *sql.DB

	// dbPath 数据库文件路径
	dbPath string
}

// SQLiteConfig SQLite 存储配置
type SQLiteConfig struct {
	// DBPath 数据库文件路径
	DBPath string
}

// NewSQLiteStore 创建 SQLite 图存储实例
// 参数:
//   - cfg: SQLite 配置
//
// 返回:
//   - *SQLiteStore: 存储实例
//   - error: 初始化错误
//
// 使用示例:
//
//	store, err := NewSQLiteStore(SQLiteConfig{
//	    DBPath: "./data/graph.db",
//	})
func NewSQLiteStore(cfg SQLiteConfig) (*SQLiteStore, error) {
	// 确保目录存在
	dir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	// 打开数据库连接
	// modernc.org/sqlite 使用 "sqlite" 作为驱动名
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		dbPath: cfg.DBPath,
	}

	// 初始化表结构
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

// initSchema 初始化数据库表结构
// 创建 entities 和 relationships 表
func (s *SQLiteStore) initSchema() error {
	// 实体表
	// 存储所有解析出的代码实体（函数、类、方法等）
	createEntitiesSQL := `
	CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		file_path TEXT NOT NULL,
		start_line INTEGER,
		end_line INTEGER,
		signature TEXT,
		language TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
	CREATE INDEX IF NOT EXISTS idx_entities_file ON entities(file_path);
	`

	// 关系表
	// 存储调用关系、继承关系、导入关系等
	createRelationshipsSQL := `
	CREATE TABLE IF NOT EXISTS relationships (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_id TEXT NOT NULL,
		target_id TEXT NOT NULL,
		type TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_relationships_source ON relationships(source_id);
	CREATE INDEX IF NOT EXISTS idx_relationships_target ON relationships(target_id);
	CREATE INDEX IF NOT EXISTS idx_relationships_type ON relationships(type);
	`

	// 执行建表语句
	if _, err := s.db.Exec(createEntitiesSQL); err != nil {
		return fmt.Errorf("创建 entities 表失败: %w", err)
	}

	if _, err := s.db.Exec(createRelationshipsSQL); err != nil {
		return fmt.Errorf("创建 relationships 表失败: %w", err)
	}

	return nil
}

// AddNode 添加单个节点
func (s *SQLiteStore) AddNode(ctx context.Context, node Node) error {
	// 使用 REPLACE INTO 实现 upsert
	// 如果 ID 已存在则更新，否则插入
	query := `
	REPLACE INTO entities (id, name, type, file_path, start_line, end_line, signature, language)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		node.ID,
		node.Name,
		string(node.Type),
		node.FilePath,
		node.StartLine,
		node.EndLine,
		node.Signature,
		"", // language 字段暂时留空
	)
	if err != nil {
		return fmt.Errorf("插入节点失败: %w", err)
	}
	return nil
}

// AddNodes 批量添加节点
func (s *SQLiteStore) AddNodes(ctx context.Context, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}

	// 使用事务批量插入，提高性能
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	// 准备语句
	stmt, err := tx.PrepareContext(ctx, `
		REPLACE INTO entities (id, name, type, file_path, start_line, end_line, signature, language)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	// 批量插入
	for _, node := range nodes {
		_, err := stmt.ExecContext(ctx,
			node.ID,
			node.Name,
			string(node.Type),
			node.FilePath,
			node.StartLine,
			node.EndLine,
			node.Signature,
			"",
		)
		if err != nil {
			return fmt.Errorf("插入节点 %s 失败: %w", node.ID, err)
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// AddEdge 添加单条边
func (s *SQLiteStore) AddEdge(ctx context.Context, edge Edge) error {
	query := `
	INSERT INTO relationships (source_id, target_id, type)
	VALUES (?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		edge.Source,
		edge.Target,
		string(edge.Type),
	)
	if err != nil {
		return fmt.Errorf("插入边失败: %w", err)
	}
	return nil
}

// AddEdges 批量添加边
func (s *SQLiteStore) AddEdges(ctx context.Context, edges []Edge) error {
	if len(edges) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO relationships (source_id, target_id, type)
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	for _, edge := range edges {
		_, err := stmt.ExecContext(ctx,
			edge.Source,
			edge.Target,
			string(edge.Type),
		)
		if err != nil {
			return fmt.Errorf("插入边失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetNode 获取节点
func (s *SQLiteStore) GetNode(ctx context.Context, id string) (*Node, error) {
	query := `
	SELECT id, name, type, file_path, start_line, end_line, signature
	FROM entities
	WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)

	var node Node
	var nodeType string
	err := row.Scan(
		&node.ID,
		&node.Name,
		&nodeType,
		&node.FilePath,
		&node.StartLine,
		&node.EndLine,
		&node.Signature,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("节点不存在: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询节点失败: %w", err)
	}

	node.Type = NodeType(nodeType)
	return &node, nil
}

// GetNeighbors 获取邻居节点
func (s *SQLiteStore) GetNeighbors(ctx context.Context, id string, direction string, edgeTypes []EdgeType) ([]Node, error) {
	var query string
	switch direction {
	case "out":
		query = `
		SELECT e.id, e.name, e.type, e.file_path, e.start_line, e.end_line, e.signature
		FROM entities e
		INNER JOIN relationships r ON e.id = r.target_id
		WHERE r.source_id = ?
		`
	case "in":
		query = `
		SELECT e.id, e.name, e.type, e.file_path, e.start_line, e.end_line, e.signature
		FROM entities e
		INNER JOIN relationships r ON e.id = r.source_id
		WHERE r.target_id = ?
		`
	default: // both
		query = `
		SELECT DISTINCT e.id, e.name, e.type, e.file_path, e.start_line, e.end_line, e.signature
		FROM entities e
		INNER JOIN relationships r ON (e.id = r.target_id AND r.source_id = ?) OR (e.id = r.source_id AND r.target_id = ?)
		`
	}

	var rows *sql.Rows
	var err error
	if direction == "both" {
		rows, err = s.db.QueryContext(ctx, query, id, id)
	} else {
		rows, err = s.db.QueryContext(ctx, query, id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询邻居失败: %w", err)
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// GetCallersOf 获取调用者
func (s *SQLiteStore) GetCallersOf(ctx context.Context, id string, depth int) ([]Node, error) {
	// 使用 CTE 进行递归查询
	query := `
	WITH RECURSIVE callers AS (
		SELECT source_id, 1 as depth
		FROM relationships
		WHERE target_id = ? AND type = 'calls'
		
		UNION ALL
		
		SELECT r.source_id, c.depth + 1
		FROM relationships r
		INNER JOIN callers c ON r.target_id = c.source_id
		WHERE r.type = 'calls' AND c.depth < ?
	)
	SELECT DISTINCT e.id, e.name, e.type, e.file_path, e.start_line, e.end_line, e.signature
	FROM entities e
	INNER JOIN callers c ON e.id = c.source_id
	`
	rows, err := s.db.QueryContext(ctx, query, id, depth)
	if err != nil {
		return nil, fmt.Errorf("查询调用者失败: %w", err)
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// GetCalleesOf 获取被调用者
func (s *SQLiteStore) GetCalleesOf(ctx context.Context, id string, depth int) ([]Node, error) {
	query := `
	WITH RECURSIVE callees AS (
		SELECT target_id, 1 as depth
		FROM relationships
		WHERE source_id = ? AND type = 'calls'
		
		UNION ALL
		
		SELECT r.target_id, c.depth + 1
		FROM relationships r
		INNER JOIN callees c ON r.source_id = c.target_id
		WHERE r.type = 'calls' AND c.depth < ?
	)
	SELECT DISTINCT e.id, e.name, e.type, e.file_path, e.start_line, e.end_line, e.signature
	FROM entities e
	INNER JOIN callees c ON e.id = c.target_id
	`
	rows, err := s.db.QueryContext(ctx, query, id, depth)
	if err != nil {
		return nil, fmt.Errorf("查询被调用者失败: %w", err)
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// FindPath 查找两个节点之间的最短路径 (BFS)
func (s *SQLiteStore) FindPath(ctx context.Context, sourceID, targetID string) (*PathResult, error) {
	if sourceID == targetID {
		return &PathResult{Path: []string{sourceID}}, nil
	}

	edges, err := s.GetAllEdges(ctx)
	if err != nil {
		return nil, err
	}

	adjacency := make(map[string][]Edge)
	for _, e := range edges {
		adjacency[e.Source] = append(adjacency[e.Source], e)
	}

	visited := make(map[string]bool)
	parent := make(map[string]string)
	parentEdge := make(map[string]*Edge)

	queue := []string{sourceID}
	visited[sourceID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range adjacency[current] {
			if !visited[edge.Target] {
				visited[edge.Target] = true
				parent[edge.Target] = current
				edgeCopy := edge
				parentEdge[edge.Target] = &edgeCopy
				queue = append(queue, edge.Target)

				if edge.Target == targetID {
					return reconstructPath(sourceID, targetID, parent, parentEdge), nil
				}
			}
		}
	}

	return nil, nil
}

// DeleteNode 删除节点及其关系
func (s *SQLiteStore) DeleteNode(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	// 删除关联的边
	_, err = tx.ExecContext(ctx, "DELETE FROM relationships WHERE source_id = ? OR target_id = ?", id, id)
	if err != nil {
		return fmt.Errorf("删除关系失败: %w", err)
	}

	// 删除节点
	_, err = tx.ExecContext(ctx, "DELETE FROM entities WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除节点失败: %w", err)
	}

	return tx.Commit()
}

// NodeCount 返回节点数量
func (s *SQLiteStore) NodeCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities").Scan(&count)
	return count, err
}

// EdgeCount 返回边数量
func (s *SQLiteStore) EdgeCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM relationships").Scan(&count)
	return count, err
}

// Clear 清空所有数据
func (s *SQLiteStore) Clear(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM relationships")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM entities")
	return err
}

// Close 关闭数据库连接
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// SaveToFile 保存到文件（SQLite 自动持久化，此方法为兼容接口）
func (s *SQLiteStore) SaveToFile(path string) error {
	// SQLite 自动持久化，无需额外操作
	return nil
}

// LoadFromFile 从文件加载（SQLite 在打开时自动加载）
func (s *SQLiteStore) LoadFromFile(path string) error {
	// SQLite 在 NewSQLiteStore 时已加载
	return nil
}

// GetAllNodes 获取所有节点
func (s *SQLiteStore) GetAllNodes(ctx context.Context) ([]Node, error) {
	query := `SELECT id, name, type, file_path, start_line, end_line, signature FROM entities`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询所有节点失败: %w", err)
	}
	defer rows.Close()
	return s.scanNodes(rows)
}

// GetAllEdges 获取所有边
func (s *SQLiteStore) GetAllEdges(ctx context.Context) ([]Edge, error) {
	query := `SELECT source_id, target_id, type FROM relationships`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询所有边失败: %w", err)
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		var edgeType string
		if err := rows.Scan(&e.Source, &e.Target, &edgeType); err != nil {
			return nil, fmt.Errorf("扫描边失败: %w", err)
		}
		e.Type = EdgeType(edgeType)
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// GetSubgraph 获取以指定节点为中心的子图
func (s *SQLiteStore) GetSubgraph(ctx context.Context, id string, depth int) (*SubgraphResult, error) {
	query := `
	WITH RECURSIVE related AS (
		SELECT ? as node_id, 0 as depth
		
		UNION
		
		SELECT r.target_id, rel.depth + 1
		FROM relationships r
		INNER JOIN related rel ON r.source_id = rel.node_id
		WHERE rel.depth < ?
		
		UNION
		
		SELECT r.source_id, rel.depth + 1
		FROM relationships r
		INNER JOIN related rel ON r.target_id = rel.node_id
		WHERE rel.depth < ?
	)
	SELECT DISTINCT e.id, e.name, e.type, e.file_path, e.start_line, e.end_line, e.signature
	FROM entities e
	INNER JOIN related rel ON e.id = rel.node_id
	`
	rows, err := s.db.QueryContext(ctx, query, id, depth, depth)
	if err != nil {
		return nil, fmt.Errorf("查询子图节点失败: %w", err)
	}
	defer rows.Close()

	nodes, err := s.scanNodes(rows)
	if err != nil {
		return nil, err
	}

	nodeIDs := make(map[string]bool)
	for _, n := range nodes {
		nodeIDs[n.ID] = true
	}

	allEdges, err := s.GetAllEdges(ctx)
	if err != nil {
		return nil, err
	}

	var subEdges []Edge
	for _, e := range allEdges {
		if nodeIDs[e.Source] && nodeIDs[e.Target] {
			subEdges = append(subEdges, e)
		}
	}

	return &SubgraphResult{
		Nodes:    nodes,
		Edges:    subEdges,
		CenterID: id,
		Depth:    depth,
	}, nil
}

// GetNodesByFile 获取指定文件中的所有节点
func (s *SQLiteStore) GetNodesByFile(ctx context.Context, filePath string) ([]Node, error) {
	query := `SELECT id, name, type, file_path, start_line, end_line, signature FROM entities WHERE file_path = ?`
	rows, err := s.db.QueryContext(ctx, query, filePath)
	if err != nil {
		return nil, fmt.Errorf("查询文件节点失败: %w", err)
	}
	defer rows.Close()
	return s.scanNodes(rows)
}

// GetNodesByType 获取指定类型的所有节点
func (s *SQLiteStore) GetNodesByType(ctx context.Context, nodeType NodeType) ([]Node, error) {
	query := `SELECT id, name, type, file_path, start_line, end_line, signature FROM entities WHERE type = ?`
	rows, err := s.db.QueryContext(ctx, query, string(nodeType))
	if err != nil {
		return nil, fmt.Errorf("查询类型节点失败: %w", err)
	}
	defer rows.Close()
	return s.scanNodes(rows)
}

// DetectCycles 检测调用图中的环
func (s *SQLiteStore) DetectCycles(ctx context.Context) ([][]string, error) {
	edges, err := s.GetAllEdges(ctx)
	if err != nil {
		return nil, err
	}

	adjacency := make(map[string][]string)
	for _, e := range edges {
		if e.Type == EdgeTypeCalls {
			adjacency[e.Source] = append(adjacency[e.Source], e.Target)
		}
	}

	return detectCyclesDFS(adjacency), nil
}

// TopologicalSort 对调用图进行拓扑排序
func (s *SQLiteStore) TopologicalSort(ctx context.Context) ([]string, error) {
	edges, err := s.GetAllEdges(ctx)
	if err != nil {
		return nil, err
	}

	adjacency := make(map[string][]string)
	allNodes := make(map[string]bool)
	for _, e := range edges {
		if e.Type == EdgeTypeCalls {
			adjacency[e.Source] = append(adjacency[e.Source], e.Target)
			allNodes[e.Source] = true
			allNodes[e.Target] = true
		}
	}

	return topologicalSortKahn(adjacency, allNodes), nil
}

// scanNodes 扫描查询结果为节点列表
func (s *SQLiteStore) scanNodes(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var node Node
		var nodeType string
		err := rows.Scan(
			&node.ID,
			&node.Name,
			&nodeType,
			&node.FilePath,
			&node.StartLine,
			&node.EndLine,
			&node.Signature,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描节点失败: %w", err)
		}
		node.Type = NodeType(nodeType)
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}
