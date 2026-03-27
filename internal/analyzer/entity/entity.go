// Package entity provides code entity extraction from AST.
// Corresponds to: REPOMIND_ARCHITECTURE_MINDMAP.md - 步骤4: 实体提取 (EntityProcessor)
package entity

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// EntityType represents the type of code entity
type EntityType string

const (
	EntityFunction EntityType = "function"
	EntityClass    EntityType = "class"
	EntityMethod   EntityType = "method"
	EntityVariable EntityType = "variable"
)

// Entity represents a code entity (function, class, method, variable)
type Entity struct {
	Name          string     `json:"name"`
	Type          EntityType `json:"type"`
	QualifiedName string     `json:"qualified_name"`
	FilePath      string     `json:"file_path"`
	StartLine     uint32     `json:"start_line"`
	EndLine       uint32     `json:"end_line"`
	StartCol      uint32     `json:"start_col"`
	EndCol        uint32     `json:"end_col"`
	Signature     string     `json:"signature"`
	DocComment    string     `json:"doc_comment"`
	Language      string     `json:"language"`
	Parent        string     `json:"parent,omitempty"`
}

// Extractor extracts entities from AST
type Extractor struct {
	content  []byte
	filePath string
	language string
}

// NewExtractor creates a new entity extractor
func NewExtractor(content []byte, filePath, language string) *Extractor {
	return &Extractor{
		content:  content,
		filePath: filePath,
		language: language,
	}
}

// Extract extracts all entities from the AST
func (e *Extractor) Extract(tree *sitter.Tree) []Entity {
	var entities []Entity
	root := tree.RootNode()

	switch e.language {
	case "python":
		entities = e.extractPython(root)
	case "go":
		entities = e.extractGo(root)
	case "java":
		entities = e.extractJava(root)
	case "javascript", "typescript":
		entities = e.extractJavaScript(root)
	}

	return entities
}

// extractPython extracts entities from Python AST
func (e *Extractor) extractPython(node *sitter.Node) []Entity {
	var entities []Entity
	e.walkNode(node, "", func(n *sitter.Node, parent string) {
		switch n.Type() {
		case "function_definition":
			entity := e.extractPythonFunction(n, parent)
			if entity != nil {
				entities = append(entities, *entity)
			}
		case "class_definition":
			entity := e.extractPythonClass(n)
			if entity != nil {
				entities = append(entities, *entity)
				// 提取类方法
				className := entity.Name
				for i := 0; i < int(n.ChildCount()); i++ {
					child := n.Child(i)
					if child.Type() == "block" {
						for j := 0; j < int(child.ChildCount()); j++ {
							blockChild := child.Child(j)
							if blockChild.Type() == "function_definition" {
								method := e.extractPythonFunction(blockChild, className)
								if method != nil {
									method.Type = EntityMethod
									entities = append(entities, *method)
								}
							}
						}
					}
				}
			}
		}
	})
	return entities
}

// extractPythonFunction extracts a Python function entity
func (e *Extractor) extractPythonFunction(node *sitter.Node, parent string) *Entity {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := e.nodeContent(nameNode)
	qualifiedName := name
	if parent != "" {
		qualifiedName = parent + "." + name
	}

	// 获取参数
	params := node.ChildByFieldName("parameters")
	signature := name
	if params != nil {
		signature = name + e.nodeContent(params)
	}

	// 获取文档字符串
	docComment := ""
	body := node.ChildByFieldName("body")
	if body != nil && body.ChildCount() > 0 {
		firstStmt := body.Child(0)
		if firstStmt != nil && firstStmt.Type() == "expression_statement" {
			expr := firstStmt.Child(0)
			if expr != nil && expr.Type() == "string" {
				docComment = e.nodeContent(expr)
			}
		}
	}

	return &Entity{
		Name:          name,
		Type:          EntityFunction,
		QualifiedName: qualifiedName,
		FilePath:      e.filePath,
		StartLine:     node.StartPoint().Row + 1,
		EndLine:       node.EndPoint().Row + 1,
		StartCol:      node.StartPoint().Column,
		EndCol:        node.EndPoint().Column,
		Signature:     signature,
		DocComment:    docComment,
		Language:      e.language,
		Parent:        parent,
	}
}

// extractPythonClass extracts a Python class entity
func (e *Extractor) extractPythonClass(node *sitter.Node) *Entity {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := e.nodeContent(nameNode)

	return &Entity{
		Name:          name,
		Type:          EntityClass,
		QualifiedName: name,
		FilePath:      e.filePath,
		StartLine:     node.StartPoint().Row + 1,
		EndLine:       node.EndPoint().Row + 1,
		StartCol:      node.StartPoint().Column,
		EndCol:        node.EndPoint().Column,
		Signature:     "class " + name,
		Language:      e.language,
	}
}

// extractGoPackageName 从 Go AST 根节点提取包名
func (e *Extractor) extractGoPackageName(root *sitter.Node) string {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "package_clause" {
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				// fallback: 第二个子节点通常是包名
				if child.ChildCount() >= 2 {
					return e.nodeContent(child.Child(1))
				}
			} else {
				return e.nodeContent(nameNode)
			}
		}
	}
	return ""
}

// extractGo extracts entities from Go AST
func (e *Extractor) extractGo(node *sitter.Node) []Entity {
	var entities []Entity
	pkgName := e.extractGoPackageName(node)

	e.walkNode(node, "", func(n *sitter.Node, parent string) {
		switch n.Type() {
		case "function_declaration":
			entity := e.extractGoFunction(n)
			if entity != nil {
				// 加上包名前缀：pkgName.funcName
				if pkgName != "" && pkgName != "main" {
					entity.QualifiedName = pkgName + "." + entity.QualifiedName
				}
				entities = append(entities, *entity)
			}
		case "method_declaration":
			entity := e.extractGoMethod(n)
			if entity != nil {
				// 方法已经是 Type.Method 格式，加包名: pkg.Type.Method
				if pkgName != "" && pkgName != "main" {
					entity.QualifiedName = pkgName + "." + entity.QualifiedName
				}
				entities = append(entities, *entity)
			}
		case "type_declaration":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type() == "type_spec" {
					entity := e.extractGoType(child)
					if entity != nil {
						if pkgName != "" && pkgName != "main" {
							entity.QualifiedName = pkgName + "." + entity.QualifiedName
						}
						entities = append(entities, *entity)
					}
				}
			}
		}
	})
	return entities
}

// extractGoFunction extracts a Go function entity
func (e *Extractor) extractGoFunction(node *sitter.Node) *Entity {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := e.nodeContent(nameNode)

	// 构建签名
	params := node.ChildByFieldName("parameters")
	result := node.ChildByFieldName("result")
	signature := "func " + name
	if params != nil {
		signature += e.nodeContent(params)
	}
	if result != nil {
		signature += " " + e.nodeContent(result)
	}

	return &Entity{
		Name:          name,
		Type:          EntityFunction,
		QualifiedName: name,
		FilePath:      e.filePath,
		StartLine:     node.StartPoint().Row + 1,
		EndLine:       node.EndPoint().Row + 1,
		Signature:     signature,
		Language:      e.language,
	}
}

// extractGoMethod extracts a Go method entity
func (e *Extractor) extractGoMethod(node *sitter.Node) *Entity {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := e.nodeContent(nameNode)

	// 获取接收者类型
	receiver := node.ChildByFieldName("receiver")
	parent := ""
	if receiver != nil {
		receiverText := e.nodeContent(receiver)
		parts := strings.Fields(receiverText)
		if len(parts) >= 2 {
			parent = strings.Trim(parts[len(parts)-1], "*)")
		}
	}

	qualifiedName := name
	if parent != "" {
		qualifiedName = parent + "." + name
	}

	return &Entity{
		Name:          name,
		Type:          EntityMethod,
		QualifiedName: qualifiedName,
		FilePath:      e.filePath,
		StartLine:     node.StartPoint().Row + 1,
		EndLine:       node.EndPoint().Row + 1,
		Language:      e.language,
		Parent:        parent,
	}
}

// extractGoType extracts a Go type entity (struct/interface)
func (e *Extractor) extractGoType(node *sitter.Node) *Entity {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := e.nodeContent(nameNode)

	return &Entity{
		Name:          name,
		Type:          EntityClass,
		QualifiedName: name,
		FilePath:      e.filePath,
		StartLine:     node.StartPoint().Row + 1,
		EndLine:       node.EndPoint().Row + 1,
		Signature:     "type " + name,
		Language:      e.language,
	}
}

// extractJava extracts entities from Java AST
func (e *Extractor) extractJava(node *sitter.Node) []Entity {
	var entities []Entity
	e.walkNode(node, "", func(n *sitter.Node, parent string) {
		switch n.Type() {
		case "class_declaration":
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				name := e.nodeContent(nameNode)
				entities = append(entities, Entity{
					Name:          name,
					Type:          EntityClass,
					QualifiedName: name,
					FilePath:      e.filePath,
					StartLine:     n.StartPoint().Row + 1,
					EndLine:       n.EndPoint().Row + 1,
					Language:      e.language,
				})
			}
		case "method_declaration":
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				name := e.nodeContent(nameNode)
				entities = append(entities, Entity{
					Name:          name,
					Type:          EntityMethod,
					QualifiedName: parent + "." + name,
					FilePath:      e.filePath,
					StartLine:     n.StartPoint().Row + 1,
					EndLine:       n.EndPoint().Row + 1,
					Language:      e.language,
					Parent:        parent,
				})
			}
		}
	})
	return entities
}

// extractJavaScript extracts entities from JavaScript/TypeScript AST
func (e *Extractor) extractJavaScript(node *sitter.Node) []Entity {
	var entities []Entity
	e.walkNode(node, "", func(n *sitter.Node, parent string) {
		switch n.Type() {
		case "function_declaration":
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				name := e.nodeContent(nameNode)
				entities = append(entities, Entity{
					Name:          name,
					Type:          EntityFunction,
					QualifiedName: name,
					FilePath:      e.filePath,
					StartLine:     n.StartPoint().Row + 1,
					EndLine:       n.EndPoint().Row + 1,
					Language:      e.language,
				})
			}
		case "class_declaration":
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				name := e.nodeContent(nameNode)
				entities = append(entities, Entity{
					Name:          name,
					Type:          EntityClass,
					QualifiedName: name,
					FilePath:      e.filePath,
					StartLine:     n.StartPoint().Row + 1,
					EndLine:       n.EndPoint().Row + 1,
					Language:      e.language,
				})
			}
		}
	})
	return entities
}

// walkNode traverses the AST and calls the callback for each node
func (e *Extractor) walkNode(node *sitter.Node, parent string, callback func(*sitter.Node, string)) {
	callback(node, parent)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		e.walkNode(child, parent, callback)
	}
}

// nodeContent returns the content of a node
func (e *Extractor) nodeContent(node *sitter.Node) string {
	return string(e.content[node.StartByte():node.EndByte()])
}
