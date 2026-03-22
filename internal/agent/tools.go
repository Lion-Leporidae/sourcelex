package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Lion-Leporidae/sourcelex/internal/agent/llm"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
	"github.com/Lion-Leporidae/sourcelex/internal/store/graph"
)

// AllTools returns all tool definitions available to the agent
func AllTools() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Name:        "semantic_search",
			Description: "根据自然语言描述，在代码知识库中搜索语义相关的代码实体（函数、类、方法等）",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "搜索查询，描述你要找的代码功能或概念",
					},
					"top_k": map[string]interface{}{
						"type":        "integer",
						"description": "返回结果数量，默认 5",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_entity",
			Description: "根据实体的 QualifiedName（如 ClassName.MethodName 或 FunctionName）获取详细信息",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{
						"type":        "string",
						"description": "实体的 QualifiedName 标识符",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "get_callers",
			Description: "查找调用了指定函数/方法的所有调用者",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{
						"type":        "string",
						"description": "要查找调用者的实体 ID",
					},
					"depth": map[string]interface{}{
						"type":        "integer",
						"description": "遍历深度，默认 1",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "get_callees",
			Description: "查找指定函数/方法调用的所有下游函数",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{
						"type":        "string",
						"description": "要查找被调用者的实体 ID",
					},
					"depth": map[string]interface{}{
						"type":        "integer",
						"description": "遍历深度，默认 1",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "get_subgraph",
			Description: "获取以指定实体为中心的代码关系子图，包含周围相关的实体和调用关系",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{
						"type":        "string",
						"description": "中心实体 ID",
					},
					"depth": map[string]interface{}{
						"type":        "integer",
						"description": "子图扩展深度，默认 2",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "find_path",
			Description: "查找两个代码实体之间的调用路径",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source_id": map[string]interface{}{
						"type":        "string",
						"description": "起始实体 ID",
					},
					"target_id": map[string]interface{}{
						"type":        "string",
						"description": "目标实体 ID",
					},
				},
				"required": []string{"source_id", "target_id"},
			},
		},
		{
			Name:        "get_workspace_stats",
			Description: "获取当前代码知识库的统计概览：实体数量、关系数量、向量数量等",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "detect_cycles",
			Description: "检测代码中的循环调用依赖",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// ExecuteTool runs a tool by name with the given JSON arguments against the KnowledgeStore
func ExecuteTool(ctx context.Context, ks *store.KnowledgeStore, name string, argsJSON string) (string, error) {
	var args map[string]interface{}
	if argsJSON != "" && argsJSON != "{}" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("解析工具参数失败: %w", err)
		}
	}

	switch name {
	case "semantic_search":
		return execSemanticSearch(ctx, ks, args)
	case "get_entity":
		return execGetEntity(ctx, ks, args)
	case "get_callers":
		return execGetCallers(ctx, ks, args)
	case "get_callees":
		return execGetCallees(ctx, ks, args)
	case "get_subgraph":
		return execGetSubgraph(ctx, ks, args)
	case "find_path":
		return execFindPath(ctx, ks, args)
	case "get_workspace_stats":
		return execGetWorkspaceStats(ctx, ks)
	case "detect_cycles":
		return execDetectCycles(ctx, ks)
	default:
		return "", fmt.Errorf("未知工具: %s", name)
	}
}

func getString(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(args map[string]interface{}, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return defaultVal
}

func toJSON(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func execSemanticSearch(ctx context.Context, ks *store.KnowledgeStore, args map[string]interface{}) (string, error) {
	query := getString(args, "query")
	if query == "" {
		return "", fmt.Errorf("query 参数不能为空")
	}
	topK := getInt(args, "top_k", 5)

	results, err := ks.SemanticSearch(ctx, query, topK)
	if err != nil {
		return "", err
	}

	type searchItem struct {
		EntityID string  `json:"entity_id"`
		Score    float32 `json:"score"`
		Name     string  `json:"name,omitempty"`
		Type     string  `json:"type,omitempty"`
		FilePath string  `json:"file_path,omitempty"`
	}
	items := make([]searchItem, len(results))
	for i, r := range results {
		items[i] = searchItem{EntityID: r.EntityID, Score: r.Score}
		if name, ok := r.Metadata["name"].(string); ok {
			items[i].Name = name
		}
		if t, ok := r.Metadata["type"].(string); ok {
			items[i].Type = t
		}
		if fp, ok := r.Metadata["file_path"].(string); ok {
			items[i].FilePath = fp
		}
	}
	return toJSON(items), nil
}

func execGetEntity(ctx context.Context, ks *store.KnowledgeStore, args map[string]interface{}) (string, error) {
	entityID := getString(args, "entity_id")
	if entityID == "" {
		return "", fmt.Errorf("entity_id 参数不能为空")
	}

	node, err := ks.GetEntity(ctx, entityID)
	if err != nil {
		return "", err
	}
	return toJSON(nodeToMap(node)), nil
}

func execGetCallers(ctx context.Context, ks *store.KnowledgeStore, args map[string]interface{}) (string, error) {
	entityID := getString(args, "entity_id")
	if entityID == "" {
		return "", fmt.Errorf("entity_id 参数不能为空")
	}
	depth := getInt(args, "depth", 1)

	callers, err := ks.GetCallersOf(ctx, entityID, depth)
	if err != nil {
		return "", err
	}
	return toJSON(nodesToMaps(callers)), nil
}

func execGetCallees(ctx context.Context, ks *store.KnowledgeStore, args map[string]interface{}) (string, error) {
	entityID := getString(args, "entity_id")
	if entityID == "" {
		return "", fmt.Errorf("entity_id 参数不能为空")
	}
	depth := getInt(args, "depth", 1)

	callees, err := ks.GetCalleesOf(ctx, entityID, depth)
	if err != nil {
		return "", err
	}
	return toJSON(nodesToMaps(callees)), nil
}

func execGetSubgraph(ctx context.Context, ks *store.KnowledgeStore, args map[string]interface{}) (string, error) {
	entityID := getString(args, "entity_id")
	if entityID == "" {
		return "", fmt.Errorf("entity_id 参数不能为空")
	}
	depth := getInt(args, "depth", 2)

	subgraph, err := ks.GetSubgraph(ctx, entityID, depth)
	if err != nil {
		return "", err
	}

	return toJSON(map[string]interface{}{
		"center_id": entityID,
		"depth":     depth,
		"nodes":     nodesToMaps(subgraph.Nodes),
		"edges":     edgesToMaps(subgraph.Edges),
	}), nil
}

func execFindPath(ctx context.Context, ks *store.KnowledgeStore, args map[string]interface{}) (string, error) {
	sourceID := getString(args, "source_id")
	targetID := getString(args, "target_id")
	if sourceID == "" || targetID == "" {
		return "", fmt.Errorf("source_id 和 target_id 参数不能为空")
	}

	result, err := ks.FindPath(ctx, sourceID, targetID)
	if err != nil {
		return "", err
	}
	if result == nil {
		return toJSON(map[string]interface{}{
			"found":  false,
			"source": sourceID,
			"target": targetID,
		}), nil
	}
	return toJSON(map[string]interface{}{
		"found":  true,
		"source": sourceID,
		"target": targetID,
		"path":   result.Path,
		"edges":  edgesToMaps(result.Edges),
	}), nil
}

func execGetWorkspaceStats(ctx context.Context, ks *store.KnowledgeStore) (string, error) {
	stats, err := ks.Stats(ctx)
	if err != nil {
		return "", err
	}
	return toJSON(map[string]interface{}{
		"vector_count": stats.VectorCount,
		"node_count":   stats.NodeCount,
		"edge_count":   stats.EdgeCount,
	}), nil
}

func execDetectCycles(ctx context.Context, ks *store.KnowledgeStore) (string, error) {
	cycles, err := ks.DetectCycles(ctx)
	if err != nil {
		return "", err
	}
	return toJSON(map[string]interface{}{
		"cycles":      cycles,
		"cycle_count": len(cycles),
	}), nil
}

func nodeToMap(n *graph.Node) map[string]interface{} {
	return map[string]interface{}{
		"id":         n.ID,
		"name":       n.Name,
		"type":       string(n.Type),
		"file_path":  n.FilePath,
		"start_line": n.StartLine,
		"end_line":   n.EndLine,
		"signature":  n.Signature,
	}
}

func nodesToMaps(nodes []graph.Node) []map[string]interface{} {
	result := make([]map[string]interface{}, len(nodes))
	for i := range nodes {
		result[i] = nodeToMap(&nodes[i])
	}
	return result
}

func edgesToMaps(edges []graph.Edge) []map[string]interface{} {
	result := make([]map[string]interface{}, len(edges))
	for i, e := range edges {
		result[i] = map[string]interface{}{
			"source": e.Source,
			"target": e.Target,
			"type":   string(e.Type),
		}
	}
	return result
}
