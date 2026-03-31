// Package relation 提供 API 端点提取功能
// 通过 AST 结构匹配识别 HTTP 路由注册（服务端暴露的 API）
// 和 HTTP 客户端调用（对外发起的请求），用于跨仓库调用关系分析
package relation

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// APIEndpoint 一个 API 端点（服务端暴露的路由）
type APIEndpoint struct {
	Method    string `json:"method"`     // GET/POST/PUT/DELETE/ANY
	Path      string `json:"path"`       // "/api/v1/users/:id"
	HandlerID string `json:"handler_id"` // 处理函数的限定名
	Framework string `json:"framework"`  // gin/echo/chi/flask/express/spring
	File      string `json:"file"`
	Line      int    `json:"line"`
}

// 已知 Web 框架的 import 路径
var webFrameworkModules = map[string]string{
	// Go
	"github.com/gin-gonic/gin":     "gin",
	"github.com/labstack/echo":     "echo",
	"github.com/labstack/echo/v4":  "echo",
	"github.com/go-chi/chi":        "chi",
	"github.com/go-chi/chi/v5":     "chi",
	"github.com/gorilla/mux":       "mux",
	"net/http":                      "net/http",
	// Python
	"flask":   "flask",
	"fastapi": "fastapi",
	"django":  "django",
	// JS/TS
	"express": "express",
	"koa":     "koa",
	"hono":    "hono",
	// Java Spring（通过注解识别，不依赖 import 匹配）
}

// HTTP 方法名到标准化方法的映射
var httpMethodNames = map[string]string{
	"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "DELETE",
	"PATCH": "PATCH", "HEAD": "HEAD", "OPTIONS": "OPTIONS", "ANY": "ANY",
	// 小写变体（Python/JS 框架常用）
	"get": "GET", "post": "POST", "put": "PUT", "delete": "DELETE",
	"patch": "PATCH", "head": "HEAD", "options": "OPTIONS",
	// 特殊方法
	"HandleFunc": "ANY", "Handle": "ANY",
	"route": "ANY", "api_route": "ANY", "add_api_route": "ANY",
}

// normalizeHTTPMethod 将方法名标准化为 HTTP 方法
func normalizeHTTPMethod(name string) string {
	if m, ok := httpMethodNames[name]; ok {
		return m
	}
	return ""
}

// ExtractAPIEndpoints 从 AST 提取 API 端点（服务端路由注册）
func (e *Extractor) ExtractAPIEndpoints(tree *sitter.Tree) []APIEndpoint {
	var endpoints []APIEndpoint
	root := tree.RootNode()

	switch e.language {
	case "go":
		endpoints = e.extractGoAPIEndpoints(root)
	case "python":
		endpoints = e.extractPythonAPIEndpoints(root)
	case "javascript", "typescript":
		endpoints = e.extractJSAPIEndpoints(root)
	case "java":
		endpoints = e.extractJavaAPIEndpoints(root)
	}

	return endpoints
}

// ==================== Go API 端点提取 ====================

// extractGoAPIEndpoints 提取 Go HTTP 路由注册
func (e *Extractor) extractGoAPIEndpoints(root *sitter.Node) []APIEndpoint {
	// 先检查文件是否导入了 Web 框架
	framework := e.detectGoWebFramework()
	if framework == "" {
		return nil
	}

	var endpoints []APIEndpoint
	e.walkGoAPINodes(root, framework, &endpoints)
	return endpoints
}

// detectGoWebFramework 通过 import 信息检测使用的 Go Web 框架
func (e *Extractor) detectGoWebFramework() string {
	imports := e.symbolTable.GetImports(e.filePath)
	for _, module := range imports {
		if fw, ok := webFrameworkModules[module]; ok {
			return fw
		}
	}
	return ""
}

// walkGoAPINodes 递归遍历 Go AST 寻找路由注册
func (e *Extractor) walkGoAPINodes(node *sitter.Node, framework string, endpoints *[]APIEndpoint) {
	if node == nil {
		return
	}

	// 检查是否为 call_expression
	if node.Type() == "call_expression" {
		if ep := e.tryExtractGoRoute(node, framework); ep != nil {
			*endpoints = append(*endpoints, *ep)
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		e.walkGoAPINodes(node.Child(i), framework, endpoints)
	}
}

// tryExtractGoRoute 尝试从 call_expression 提取 Go 路由
func (e *Extractor) tryExtractGoRoute(callNode *sitter.Node, framework string) *APIEndpoint {
	funcNode := callNode.ChildByFieldName("function")
	if funcNode == nil || funcNode.Type() != "selector_expression" {
		return nil
	}

	// 获取 selector 的方法名（field）
	fieldNode := funcNode.ChildByFieldName("field")
	if fieldNode == nil {
		return nil
	}
	methodName := e.nodeContent(fieldNode)

	// 检查是否为 HTTP 方法名
	httpMethod := normalizeHTTPMethod(methodName)
	if httpMethod == "" {
		return nil
	}

	// 获取参数列表
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}

	// 第一个参数必须是字符串字面量（路由路径）
	pathNode := e.findFirstStringArg(args)
	if pathNode == nil {
		return nil
	}
	routePath := strings.Trim(e.nodeContent(pathNode), "\"`")

	// 过滤明显不是路由路径的字符串
	if !looksLikeRoutePath(routePath) {
		return nil
	}

	// 提取 handler（第二个参数）
	handlerID := e.findHandlerArg(args)
	if handlerID != "" {
		// 通过符号表解析为限定名
		result := e.symbolTable.ResolveWithConfidence(handlerID, e.filePath)
		if result.Confidence > 0.1 {
			handlerID = result.QualifiedName
		}
	}

	return &APIEndpoint{
		Method:    httpMethod,
		Path:      routePath,
		HandlerID: handlerID,
		Framework: framework,
		File:      e.filePath,
		Line:      int(callNode.StartPoint().Row) + 1,
	}
}

// ==================== Python API 端点提取 ====================

// extractPythonAPIEndpoints 提取 Python HTTP 路由注册（Flask/FastAPI 装饰器模式）
func (e *Extractor) extractPythonAPIEndpoints(root *sitter.Node) []APIEndpoint {
	framework := e.detectPythonWebFramework()
	if framework == "" {
		return nil
	}

	var endpoints []APIEndpoint
	e.walkPythonAPINodes(root, framework, &endpoints)
	return endpoints
}

// detectPythonWebFramework 通过 import 检测 Python Web 框架
func (e *Extractor) detectPythonWebFramework() string {
	imports := e.symbolTable.GetImports(e.filePath)
	for _, module := range imports {
		for fwModule, fwName := range webFrameworkModules {
			if module == fwModule || strings.HasPrefix(module, fwModule+".") {
				return fwName
			}
		}
	}
	return ""
}

// walkPythonAPINodes 递归遍历 Python AST 寻找装饰器路由
func (e *Extractor) walkPythonAPINodes(node *sitter.Node, framework string, endpoints *[]APIEndpoint) {
	if node == nil {
		return
	}

	if node.Type() == "decorated_definition" {
		if ep := e.tryExtractPythonRoute(node, framework); ep != nil {
			*endpoints = append(*endpoints, *ep)
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		e.walkPythonAPINodes(node.Child(i), framework, endpoints)
	}
}

// tryExtractPythonRoute 从 decorated_definition 提取 Python 路由
func (e *Extractor) tryExtractPythonRoute(node *sitter.Node, framework string) *APIEndpoint {
	// 遍历装饰器
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() != "decorator" {
			continue
		}

		// 装饰器内的 call 节点
		callNode := e.findChildOfType(child, "call")
		if callNode == nil {
			continue
		}
		funcNode := callNode.ChildByFieldName("function")
		if funcNode == nil || funcNode.Type() != "attribute" {
			continue
		}

		// 检查 attribute 的方法名
		attrNode := funcNode.ChildByFieldName("attribute")
		if attrNode == nil {
			continue
		}
		methodName := e.nodeContent(attrNode)
		httpMethod := normalizeHTTPMethod(methodName)
		if httpMethod == "" {
			continue
		}

		// 提取路径参数
		args := callNode.ChildByFieldName("arguments")
		if args == nil {
			continue
		}
		pathNode := e.findChildOfType(args, "string")
		if pathNode == nil {
			continue
		}
		routePath := strings.Trim(e.nodeContent(pathNode), "\"'")
		if !looksLikeRoutePath(routePath) {
			continue
		}

		// 提取 handler 函数名
		funcDef := e.findChildOfType(node, "function_definition")
		handlerName := ""
		if funcDef != nil {
			nameNode := funcDef.ChildByFieldName("name")
			if nameNode != nil {
				handlerName = e.nodeContent(nameNode)
			}
		}

		return &APIEndpoint{
			Method:    httpMethod,
			Path:      routePath,
			HandlerID: handlerName,
			Framework: framework,
			File:      e.filePath,
			Line:      int(node.StartPoint().Row) + 1,
		}
	}
	return nil
}

// ==================== JavaScript/TypeScript API 端点提取 ====================

// extractJSAPIEndpoints 提取 JS/TS HTTP 路由注册（Express 模式）
func (e *Extractor) extractJSAPIEndpoints(root *sitter.Node) []APIEndpoint {
	framework := e.detectJSWebFramework()
	if framework == "" {
		return nil
	}

	var endpoints []APIEndpoint
	e.walkJSAPINodes(root, framework, &endpoints)
	return endpoints
}

// detectJSWebFramework 通过 import 检测 JS Web 框架
func (e *Extractor) detectJSWebFramework() string {
	imports := e.symbolTable.GetImports(e.filePath)
	for _, module := range imports {
		if fw, ok := webFrameworkModules[module]; ok {
			return fw
		}
	}
	return ""
}

// walkJSAPINodes 递归遍历 JS AST 寻找路由注册
func (e *Extractor) walkJSAPINodes(node *sitter.Node, framework string, endpoints *[]APIEndpoint) {
	if node == nil {
		return
	}

	if node.Type() == "call_expression" {
		if ep := e.tryExtractJSRoute(node, framework); ep != nil {
			*endpoints = append(*endpoints, *ep)
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		e.walkJSAPINodes(node.Child(i), framework, endpoints)
	}
}

// tryExtractJSRoute 从 call_expression 提取 JS 路由
func (e *Extractor) tryExtractJSRoute(callNode *sitter.Node, framework string) *APIEndpoint {
	funcNode := callNode.ChildByFieldName("function")
	if funcNode == nil || funcNode.Type() != "member_expression" {
		return nil
	}

	// 获取方法名（property）
	propNode := funcNode.ChildByFieldName("property")
	if propNode == nil {
		return nil
	}
	methodName := e.nodeContent(propNode)
	httpMethod := normalizeHTTPMethod(methodName)
	if httpMethod == "" {
		return nil
	}

	// 获取参数
	args := callNode.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}

	// 第一个参数为字符串字面量
	pathNode := e.findFirstJSStringArg(args)
	if pathNode == nil {
		return nil
	}
	routePath := strings.Trim(e.nodeContent(pathNode), "\"'`")
	if !looksLikeRoutePath(routePath) {
		return nil
	}

	return &APIEndpoint{
		Method:    httpMethod,
		Path:      routePath,
		HandlerID: "",
		Framework: framework,
		File:      e.filePath,
		Line:      int(callNode.StartPoint().Row) + 1,
	}
}

// ==================== Java API 端点提取 ====================

// extractJavaAPIEndpoints 提取 Java Spring 注解路由
func (e *Extractor) extractJavaAPIEndpoints(root *sitter.Node) []APIEndpoint {
	var endpoints []APIEndpoint
	e.walkJavaAPINodes(root, &endpoints)
	return endpoints
}

// walkJavaAPINodes 递归遍历 Java AST 寻找 Spring 注解
func (e *Extractor) walkJavaAPINodes(node *sitter.Node, endpoints *[]APIEndpoint) {
	if node == nil {
		return
	}

	// Java 的路由通过注解标注在方法上
	if node.Type() == "method_declaration" {
		if ep := e.tryExtractJavaRoute(node); ep != nil {
			*endpoints = append(*endpoints, *ep)
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		e.walkJavaAPINodes(node.Child(i), endpoints)
	}
}

// tryExtractJavaRoute 从 method_declaration 的注解提取 Spring 路由
func (e *Extractor) tryExtractJavaRoute(methodNode *sitter.Node) *APIEndpoint {
	// 查找方法前面的 annotation（可能有多个）
	// 在 tree-sitter 中，annotation 是 method_declaration 的子节点之一
	for i := 0; i < int(methodNode.ChildCount()); i++ {
		child := methodNode.Child(i)
		if child.Type() != "marker_annotation" && child.Type() != "annotation" {
			continue
		}

		annoText := e.nodeContent(child)

		// 匹配 Spring 的 @XxxMapping 注解
		var httpMethod string
		switch {
		case strings.Contains(annoText, "GetMapping"):
			httpMethod = "GET"
		case strings.Contains(annoText, "PostMapping"):
			httpMethod = "POST"
		case strings.Contains(annoText, "PutMapping"):
			httpMethod = "PUT"
		case strings.Contains(annoText, "DeleteMapping"):
			httpMethod = "DELETE"
		case strings.Contains(annoText, "PatchMapping"):
			httpMethod = "PATCH"
		case strings.Contains(annoText, "RequestMapping"):
			httpMethod = "ANY"
		default:
			continue
		}

		// 提取路径（注解参数中的字符串）
		routePath := e.extractAnnotationStringArg(child)
		if routePath == "" {
			continue
		}
		if !looksLikeRoutePath(routePath) {
			continue
		}

		// 提取方法名
		nameNode := methodNode.ChildByFieldName("name")
		handlerName := ""
		if nameNode != nil {
			handlerName = e.nodeContent(nameNode)
		}

		return &APIEndpoint{
			Method:    httpMethod,
			Path:      routePath,
			HandlerID: handlerName,
			Framework: "spring",
			File:      e.filePath,
			Line:      int(methodNode.StartPoint().Row) + 1,
		}
	}
	return nil
}

// ==================== 辅助函数 ====================

// findFirstStringArg 在 Go 参数列表中找第一个字符串字面量
func (e *Extractor) findFirstStringArg(argsNode *sitter.Node) *sitter.Node {
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		child := argsNode.Child(i)
		switch child.Type() {
		case "interpreted_string_literal", "raw_string_literal":
			return child
		}
	}
	return nil
}

// findFirstJSStringArg 在 JS 参数列表中找第一个字符串字面量
func (e *Extractor) findFirstJSStringArg(argsNode *sitter.Node) *sitter.Node {
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		child := argsNode.Child(i)
		switch child.Type() {
		case "string", "template_string":
			return child
		}
	}
	return nil
}

// findHandlerArg 在参数列表中找到 handler 参数（第二个非标点参数）
func (e *Extractor) findHandlerArg(argsNode *sitter.Node) string {
	argIdx := 0
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		child := argsNode.Child(i)
		// 跳过括号和逗号
		if child.Type() == "(" || child.Type() == ")" || child.Type() == "," {
			continue
		}
		argIdx++
		if argIdx == 2 {
			// 第二个参数就是 handler
			if child.Type() == "identifier" || child.Type() == "selector_expression" {
				return e.nodeContent(child)
			}
			return ""
		}
	}
	return ""
}

// findChildOfType 在节点子树中找指定类型的第一个节点
func (e *Extractor) findChildOfType(node *sitter.Node, nodeType string) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == nodeType {
			return child
		}
		if found := e.findChildOfType(child, nodeType); found != nil {
			return found
		}
	}
	return nil
}

// extractAnnotationStringArg 从 Java 注解中提取字符串参数
func (e *Extractor) extractAnnotationStringArg(annoNode *sitter.Node) string {
	// 找到注解中的字符串字面量
	strNode := e.findChildOfType(annoNode, "string_literal")
	if strNode == nil {
		return ""
	}
	return strings.Trim(e.nodeContent(strNode), "\"")
}

// looksLikeRoutePath 判断字符串是否看起来像路由路径
func looksLikeRoutePath(s string) bool {
	if s == "" {
		return false
	}
	// 必须以 / 开头
	if !strings.HasPrefix(s, "/") {
		return false
	}
	// 排除明显不是路径的（如文件系统路径）
	if strings.Contains(s, "\\") {
		return false
	}
	// 排除太长的字符串（路由路径通常不超过 200 字符）
	if len(s) > 200 {
		return false
	}
	return true
}
