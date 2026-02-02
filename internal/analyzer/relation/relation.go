// Package relation 提供调用关系提取功能
// 对应架构文档: 步骤7 - 调用关系提取 (RelationProcessor)
//
// 核心功能:
// - 基于 Tree-sitter 识别 call_expression 节点
// - 解析调用目标并匹配到已知符号
// - 支持多语言: Go, Python, Java, JavaScript
package relation

import (
	"strings"

	"github.com/repomind/repomind-go/internal/analyzer/entity"
	sitter "github.com/smacker/go-tree-sitter"
)

// CallRelation 调用关系
type CallRelation struct {
	// CallerID 调用者 (qualified_name)
	CallerID string `json:"caller_id"`

	// CalleeID 被调用者 (可能是完全限定名或简单名)
	CalleeID string `json:"callee_id"`

	// CallerFile 调用者所在文件
	CallerFile string `json:"caller_file"`

	// Line 调用发生的行号
	Line int `json:"line"`

	// Column 调用发生的列号
	Column int `json:"column"`
}

// Extractor 调用关系提取器
type Extractor struct {
	content  []byte
	filePath string
	language string

	// symbolTable 符号表（用于解析调用目标）
	symbolTable *SymbolTable

	// currentScope 当前作用域（当前所在的函数/方法）
	currentScope string

	// scopeStack 作用域栈
	scopeStack []string
}

// NewExtractor 创建调用关系提取器
// 参数:
//   - content: 文件内容
//   - filePath: 文件路径
//   - language: 编程语言
//   - symbolTable: 符号表（可为 nil）
func NewExtractor(content []byte, filePath, language string, symbolTable *SymbolTable) *Extractor {
	if symbolTable == nil {
		symbolTable = NewSymbolTable()
	}
	return &Extractor{
		content:     content,
		filePath:    filePath,
		language:    language,
		symbolTable: symbolTable,
		scopeStack:  make([]string, 0),
	}
}

// Extract 从 AST 提取调用关系
func (e *Extractor) Extract(tree *sitter.Tree) []CallRelation {
	var relations []CallRelation
	root := tree.RootNode()

	switch e.language {
	case "go":
		relations = e.extractGo(root)
	case "python":
		relations = e.extractPython(root)
	case "java":
		relations = e.extractJava(root)
	case "javascript", "typescript":
		relations = e.extractJavaScript(root)
	}

	return relations
}

// extractGo 提取 Go 调用关系
func (e *Extractor) extractGo(root *sitter.Node) []CallRelation {
	var relations []CallRelation
	e.extractGoRecursive(root, &relations)
	return relations
}

// extractGoRecursive 递归提取 Go 调用关系
func (e *Extractor) extractGoRecursive(node *sitter.Node, relations *[]CallRelation) {
	if node == nil {
		return
	}

	// 检查是否需要进入新作用域
	shouldPop := false
	switch node.Type() {
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			e.pushScope(e.nodeContent(nameNode))
			shouldPop = true
		}
	case "method_declaration":
		nameNode := node.ChildByFieldName("name")
		receiver := node.ChildByFieldName("receiver")
		if nameNode != nil {
			methodName := e.nodeContent(nameNode)
			if receiver != nil {
				receiverType := e.extractGoReceiverType(receiver)
				if receiverType != "" {
					methodName = receiverType + "." + methodName
				}
			}
			e.pushScope(methodName)
			shouldPop = true
		}
	case "call_expression":
		// 在当前作用域内发现调用
		if e.currentScope != "" {
			rel := e.extractGoCall(node)
			if rel != nil {
				*relations = append(*relations, *rel)
			}
		}
	}

	// 递归处理子节点
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		e.extractGoRecursive(child, relations)
	}

	// 离开作用域
	if shouldPop {
		e.popScope()
	}
}

// extractGoCall 提取 Go 调用
func (e *Extractor) extractGoCall(node *sitter.Node) *CallRelation {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return nil
	}

	var calleeName string
	switch funcNode.Type() {
	case "identifier":
		// 简单调用: foo()
		calleeName = e.nodeContent(funcNode)
	case "selector_expression":
		// 方法调用: obj.Method() 或 pkg.Func()
		calleeName = e.nodeContent(funcNode)
	}

	if calleeName == "" {
		return nil
	}

	// 尝试解析完全限定名
	calleeID := e.symbolTable.Resolve(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   calleeID,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
	}
}

// extractGoReceiverType 提取 Go 方法的接收者类型
func (e *Extractor) extractGoReceiverType(receiver *sitter.Node) string {
	// receiver 格式: (p *Parser) 或 (p Parser)
	text := e.nodeContent(receiver)
	text = strings.Trim(text, "()")
	parts := strings.Fields(text)
	if len(parts) >= 2 {
		typePart := parts[len(parts)-1]
		return strings.TrimLeft(typePart, "*")
	}
	return ""
}

// extractPython 提取 Python 调用关系
func (e *Extractor) extractPython(root *sitter.Node) []CallRelation {
	var relations []CallRelation

	e.walkNodeWithScope(root, func(node *sitter.Node, scope string) {
		switch node.Type() {
		case "function_definition":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				e.pushScope(e.nodeContent(nameNode))
			}
		case "class_definition":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				e.pushScope(e.nodeContent(nameNode))
			}
		case "call":
			if e.currentScope != "" {
				rel := e.extractPythonCall(node)
				if rel != nil {
					relations = append(relations, *rel)
				}
			}
		}
	})

	return relations
}

// extractPythonCall 提取 Python 调用
func (e *Extractor) extractPythonCall(node *sitter.Node) *CallRelation {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return nil
	}

	var calleeName string
	switch funcNode.Type() {
	case "identifier":
		calleeName = e.nodeContent(funcNode)
	case "attribute":
		// obj.method()
		calleeName = e.nodeContent(funcNode)
	}

	if calleeName == "" {
		return nil
	}

	calleeID := e.symbolTable.Resolve(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   calleeID,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
	}
}

// extractJava 提取 Java 调用关系
func (e *Extractor) extractJava(root *sitter.Node) []CallRelation {
	var relations []CallRelation

	e.walkNodeWithScope(root, func(node *sitter.Node, scope string) {
		switch node.Type() {
		case "class_declaration":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				e.pushScope(e.nodeContent(nameNode))
			}
		case "method_declaration":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				e.pushScope(e.nodeContent(nameNode))
			}
		case "method_invocation":
			if e.currentScope != "" {
				rel := e.extractJavaCall(node)
				if rel != nil {
					relations = append(relations, *rel)
				}
			}
		}
	})

	return relations
}

// extractJavaCall 提取 Java 调用
func (e *Extractor) extractJavaCall(node *sitter.Node) *CallRelation {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	calleeName := e.nodeContent(nameNode)

	// 检查是否有对象调用
	objectNode := node.ChildByFieldName("object")
	if objectNode != nil {
		calleeName = e.nodeContent(objectNode) + "." + calleeName
	}

	calleeID := e.symbolTable.Resolve(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   calleeID,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
	}
}

// extractJavaScript 提取 JavaScript/TypeScript 调用关系
func (e *Extractor) extractJavaScript(root *sitter.Node) []CallRelation {
	var relations []CallRelation

	e.walkNodeWithScope(root, func(node *sitter.Node, scope string) {
		switch node.Type() {
		case "function_declaration", "arrow_function", "function":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				e.pushScope(e.nodeContent(nameNode))
			}
		case "class_declaration":
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				e.pushScope(e.nodeContent(nameNode))
			}
		case "call_expression":
			if e.currentScope != "" {
				rel := e.extractJSCall(node)
				if rel != nil {
					relations = append(relations, *rel)
				}
			}
		}
	})

	return relations
}

// extractJSCall 提取 JavaScript 调用
func (e *Extractor) extractJSCall(node *sitter.Node) *CallRelation {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return nil
	}

	var calleeName string
	switch funcNode.Type() {
	case "identifier":
		calleeName = e.nodeContent(funcNode)
	case "member_expression":
		calleeName = e.nodeContent(funcNode)
	}

	if calleeName == "" {
		return nil
	}

	calleeID := e.symbolTable.Resolve(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   calleeID,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
	}
}

// walkNodeWithScope 遍历 AST 并维护作用域
func (e *Extractor) walkNodeWithScope(node *sitter.Node, callback func(*sitter.Node, string)) {
	callback(node, e.currentScope)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		e.walkNodeWithScope(child, callback)
	}
}

// pushScope 进入新作用域
func (e *Extractor) pushScope(name string) {
	if e.currentScope != "" {
		name = e.currentScope + "." + name
	}
	e.scopeStack = append(e.scopeStack, e.currentScope)
	e.currentScope = name
}

// popScope 退出当前作用域
func (e *Extractor) popScope() {
	if len(e.scopeStack) > 0 {
		e.currentScope = e.scopeStack[len(e.scopeStack)-1]
		e.scopeStack = e.scopeStack[:len(e.scopeStack)-1]
	} else {
		e.currentScope = ""
	}
}

// nodeContent 获取节点内容
func (e *Extractor) nodeContent(node *sitter.Node) string {
	return string(e.content[node.StartByte():node.EndByte()])
}

// BuildSymbolTableFromEntities 从实体列表构建符号表
func BuildSymbolTableFromEntities(entities []entity.Entity) *SymbolTable {
	st := NewSymbolTable()
	for _, e := range entities {
		st.AddSymbol(e.Name, e.QualifiedName, e.FilePath)
	}
	return st
}
