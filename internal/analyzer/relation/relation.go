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

	"github.com/Lion-Leporidae/sourcelex/internal/analyzer/entity"
	sitter "github.com/smacker/go-tree-sitter"
)

// CallRelation 调用关系
type CallRelation struct {
	CallerID   string  `json:"caller_id"`
	CalleeID   string  `json:"callee_id"`
	CallerFile string  `json:"caller_file"`
	Line       int     `json:"line"`
	Column     int     `json:"column"`
	Confidence float64 `json:"confidence"` // 0-1 置信度
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

	// pkgName Go 包名（用于生成与实体一致的限定名）
	pkgName string
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

// ExtractImports 从 AST 提取 import 语句并填充符号表
// 支持 Go/Python/Java/JavaScript 的 import 语法
func (e *Extractor) ExtractImports(tree *sitter.Tree) {
	root := tree.RootNode()
	switch e.language {
	case "go":
		e.extractGoImports(root)
	case "python":
		e.extractPythonImports(root)
	case "java":
		e.extractJavaImports(root)
	case "javascript", "typescript":
		e.extractJSImports(root)
	}
}

// extractGoImports 提取 Go import 语句
// import "fmt"                → alias="fmt", module="fmt"
// import alias "path/to/pkg" → alias="alias", module="path/to/pkg"
// import ( "pkg1"; "pkg2" )  → 多条
func (e *Extractor) extractGoImports(node *sitter.Node) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "import_declaration" {
			e.extractGoImportDecl(child)
		}
	}
}

func (e *Extractor) extractGoImportDecl(node *sitter.Node) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "import_spec":
			e.extractGoImportSpec(child)
		case "import_spec_list":
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() == "import_spec" {
					e.extractGoImportSpec(spec)
				}
			}
		}
	}
}

func (e *Extractor) extractGoImportSpec(spec *sitter.Node) {
	nameNode := spec.ChildByFieldName("name")
	pathNode := spec.ChildByFieldName("path")
	if pathNode == nil {
		return
	}

	importPath := strings.Trim(e.nodeContent(pathNode), "\"")
	// 包别名
	var alias string
	if nameNode != nil {
		alias = e.nodeContent(nameNode)
		if alias == "." || alias == "_" {
			return
		}
	} else {
		// 默认别名是路径最后一段
		parts := strings.Split(importPath, "/")
		alias = parts[len(parts)-1]
	}

	e.symbolTable.AddImport(e.filePath, alias, importPath)
}

// extractPythonImports 提取 Python import 语句
// import os          → alias="os"
// from os import path → alias="path", module="os.path"
// import os as myos  → alias="myos", module="os"
func (e *Extractor) extractPythonImports(node *sitter.Node) {
	e.walkImportNodes(node, func(n *sitter.Node) {
		switch n.Type() {
		case "import_statement":
			// import x, import x as y
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				name := e.nodeContent(nameNode)
				parts := strings.Split(name, ".")
				alias := parts[len(parts)-1]
				e.symbolTable.AddImport(e.filePath, alias, name)
			}
		case "import_from_statement":
			// from x import y
			moduleNode := n.ChildByFieldName("module_name")
			if moduleNode == nil {
				return
			}
			moduleName := e.nodeContent(moduleNode)
			// 提取导入的名称
			for j := 0; j < int(n.ChildCount()); j++ {
				child := n.Child(j)
				if child.Type() == "dotted_name" && child != moduleNode {
					importedName := e.nodeContent(child)
					e.symbolTable.AddImport(e.filePath, importedName, moduleName+"."+importedName)
				} else if child.Type() == "aliased_import" {
					nameChild := child.ChildByFieldName("name")
					aliasChild := child.ChildByFieldName("alias")
					if nameChild != nil {
						importedName := e.nodeContent(nameChild)
						alias := importedName
						if aliasChild != nil {
							alias = e.nodeContent(aliasChild)
						}
						e.symbolTable.AddImport(e.filePath, alias, moduleName+"."+importedName)
					}
				}
			}
		}
	})
}

// extractJavaImports 提取 Java import 语句
func (e *Extractor) extractJavaImports(node *sitter.Node) {
	e.walkImportNodes(node, func(n *sitter.Node) {
		if n.Type() == "import_declaration" {
			// 获取完整导入路径
			for j := 0; j < int(n.ChildCount()); j++ {
				child := n.Child(j)
				if child.Type() == "scoped_identifier" || child.Type() == "identifier" {
					fullPath := e.nodeContent(child)
					parts := strings.Split(fullPath, ".")
					shortName := parts[len(parts)-1]
					if shortName != "*" {
						e.symbolTable.AddImport(e.filePath, shortName, fullPath)
					}
				}
			}
		}
	})
}

// extractJSImports 提取 JavaScript/TypeScript import 语句
func (e *Extractor) extractJSImports(node *sitter.Node) {
	e.walkImportNodes(node, func(n *sitter.Node) {
		if n.Type() == "import_statement" {
			// import { x } from 'y'
			// import x from 'y'
			sourceNode := n.ChildByFieldName("source")
			if sourceNode == nil {
				return
			}
			modulePath := strings.Trim(e.nodeContent(sourceNode), "\"'`")
			parts := strings.Split(modulePath, "/")
			moduleName := parts[len(parts)-1]
			// 简化：用模块文件名作为别名
			e.symbolTable.AddImport(e.filePath, moduleName, modulePath)

			// 提取具名导入
			for j := 0; j < int(n.ChildCount()); j++ {
				child := n.Child(j)
				if child.Type() == "import_clause" {
					for k := 0; k < int(child.ChildCount()); k++ {
						clauseChild := child.Child(k)
						if clauseChild.Type() == "identifier" {
							name := e.nodeContent(clauseChild)
							e.symbolTable.AddImport(e.filePath, name, modulePath+"."+name)
						} else if clauseChild.Type() == "named_imports" {
							for m := 0; m < int(clauseChild.ChildCount()); m++ {
								specifier := clauseChild.Child(m)
								if specifier.Type() == "import_specifier" {
									nameChild := specifier.ChildByFieldName("name")
									if nameChild != nil {
										name := e.nodeContent(nameChild)
										e.symbolTable.AddImport(e.filePath, name, modulePath+"."+name)
									}
								}
							}
						}
					}
				}
			}
		}
	})
}

// walkImportNodes 遍历顶层节点查找 import
func (e *Extractor) walkImportNodes(node *sitter.Node, callback func(*sitter.Node)) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		callback(child)
	}
}

// extractGo 提取 Go 调用关系
func (e *Extractor) extractGo(root *sitter.Node) []CallRelation {
	var relations []CallRelation
	// 提取包名（与 entity.go 一致）
	e.pkgName = e.extractGoPackageName(root)
	e.extractGoRecursive(root, &relations)
	return relations
}

// extractGoPackageName 从 Go AST 根节点提取包名
func (e *Extractor) extractGoPackageName(root *sitter.Node) string {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "package_clause" {
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
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
			funcName := e.nodeContent(nameNode)
			// 与 entity.go 一致：非 main 包加包名前缀
			if e.pkgName != "" && e.pkgName != "main" {
				funcName = e.pkgName + "." + funcName
			}
			e.pushScope(funcName)
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
			// 与 entity.go 一致：非 main 包加包名前缀
			if e.pkgName != "" && e.pkgName != "main" {
				methodName = e.pkgName + "." + methodName
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

	// 尝试解析完全限定名（带置信度）
	result := e.symbolTable.ResolveWithConfidence(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   result.QualifiedName,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
		Confidence: result.Confidence,
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

	calleeResult := e.symbolTable.ResolveWithConfidence(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   calleeResult.QualifiedName,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
		Confidence: calleeResult.Confidence,
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

	javaResult := e.symbolTable.ResolveWithConfidence(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   javaResult.QualifiedName,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
		Confidence: javaResult.Confidence,
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

	jsResult := e.symbolTable.ResolveWithConfidence(calleeName, e.filePath)

	return &CallRelation{
		CallerID:   e.currentScope,
		CalleeID:   jsResult.QualifiedName,
		CallerFile: e.filePath,
		Line:       int(node.StartPoint().Row) + 1,
		Column:     int(node.StartPoint().Column),
		Confidence: jsResult.Confidence,
	}
}

// walkNodeWithScope 遍历 AST 并维护作用域
func (e *Extractor) walkNodeWithScope(node *sitter.Node, callback func(*sitter.Node, string)) {
	scopeBefore := e.currentScope
	callback(node, e.currentScope)
	scopeChanged := e.currentScope != scopeBefore

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		e.walkNodeWithScope(child, callback)
	}

	if scopeChanged {
		e.popScope()
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
