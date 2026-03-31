// Package relation 提供符号表功能
// 用于解析调用目标到完全限定名
package relation

import (
	"strings"
	"sync"
)

// SymbolTable 符号表
// 存储文件级和全局符号，用于解析调用目标
type SymbolTable struct {
	mu sync.RWMutex

	// globalSymbols 全局符号映射: name -> qualified_name（精确匹配）
	globalSymbols map[string]string

	// nameToQualified 简单名到限定名的多值映射: simpleName -> []qualifiedName
	// 解决同名函数覆盖问题
	nameToQualified map[string][]string

	// fileSymbols 文件级符号: file -> {name -> qualified_name}
	fileSymbols map[string]map[string]string

	// imports 导入映射: file -> {alias -> module}
	imports map[string]map[string]string
}

// NewSymbolTable 创建空符号表
func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		globalSymbols:   make(map[string]string),
		nameToQualified: make(map[string][]string),
		fileSymbols:     make(map[string]map[string]string),
		imports:         make(map[string]map[string]string),
	}
}

// AddSymbol 添加符号到表中
// 参数:
//   - name: 符号简单名称
//   - qualifiedName: 完全限定名
//   - filePath: 所在文件
func (st *SymbolTable) AddSymbol(name, qualifiedName, filePath string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// 精确匹配（限定名 → 自身）
	st.globalSymbols[qualifiedName] = qualifiedName

	// 多值映射（简单名 → 可能的限定名列表）
	existing := st.nameToQualified[name]
	found := false
	for _, qn := range existing {
		if qn == qualifiedName {
			found = true
			break
		}
	}
	if !found {
		st.nameToQualified[name] = append(existing, qualifiedName)
	}

	// 文件级符号
	if st.fileSymbols[filePath] == nil {
		st.fileSymbols[filePath] = make(map[string]string)
	}
	st.fileSymbols[filePath][name] = qualifiedName
	st.fileSymbols[filePath][qualifiedName] = qualifiedName
}

// AddImport 添加导入映射
// 参数:
//   - filePath: 文件路径
//   - alias: 导入别名（或包名）
//   - module: 模块路径
func (st *SymbolTable) AddImport(filePath, alias, module string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.imports[filePath] == nil {
		st.imports[filePath] = make(map[string]string)
	}
	st.imports[filePath][alias] = module
}

// Resolve 解析符号名到完全限定名
// 解析优先级：文件局部 → 精确全局匹配 → import 解析 → 类型.方法 → 简单名多值
func (st *SymbolTable) Resolve(name, fromFile string) string {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// 1. 先查文件局部符号（同文件定义优先）
	if fileMap, ok := st.fileSymbols[fromFile]; ok {
		if qn, ok := fileMap[name]; ok {
			return qn
		}
	}

	// 2. 精确匹配全局符号（限定名完全一致）
	if qn, ok := st.globalSymbols[name]; ok {
		return qn
	}

	// 3. 处理带点的名称 (pkg.Func 或 Type.Method)
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		prefix := parts[0]
		suffix := parts[1]

		// 3a. 通过 import 映射解析（pkg.Func → 用 import 信息得到完整路径）
		if imports, ok := st.imports[fromFile]; ok {
			if module, ok := imports[prefix]; ok {
				// 尝试 module.suffix 作为限定名
				candidate := module + "." + suffix
				if _, ok := st.globalSymbols[candidate]; ok {
					return candidate
				}
				// 回退：可能 import 的是包路径，而限定名用包名
				moduleParts := strings.Split(module, "/")
				pkgName := moduleParts[len(moduleParts)-1]
				candidate = pkgName + "." + suffix
				if _, ok := st.globalSymbols[candidate]; ok {
					return candidate
				}
				return candidate // 即使找不到，也用推断的限定名
			}
		}

		// 3b. 前缀是已知类型/类名（Type.Method）
		if candidates, ok := st.nameToQualified[prefix]; ok && len(candidates) > 0 {
			// 用第一个匹配的类型作为前缀
			return candidates[0] + "." + suffix
		}
		// 尝试精确匹配
		if qn, ok := st.globalSymbols[prefix]; ok {
			return qn + "." + suffix
		}
	}

	// 4. 简单名多值查找
	if candidates, ok := st.nameToQualified[name]; ok && len(candidates) > 0 {
		// 优先选择同目录/同包下的定义
		fromDir := fileDir(fromFile)
		for _, qn := range candidates {
			// 查找定义该符号的文件
			for file, fileMap := range st.fileSymbols {
				for _, fqn := range fileMap {
					if fqn == qn && fileDir(file) == fromDir {
						return qn
					}
				}
			}
		}
		// 没有同目录的，返回第一个
		return candidates[0]
	}

	// 5. 未解析的外部调用，返回原名
	return name
}

// fileDir 获取文件所在目录
func fileDir(filePath string) string {
	idx := strings.LastIndex(filePath, "/")
	if idx >= 0 {
		return filePath[:idx]
	}
	return ""
}

// GetAllSymbols 获取所有全局符号
func (st *SymbolTable) GetAllSymbols() map[string]string {
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[string]string, len(st.globalSymbols))
	for k, v := range st.globalSymbols {
		result[k] = v
	}
	return result
}

// Size 返回符号数量
func (st *SymbolTable) Size() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.globalSymbols)
}

// GetImports 获取指定文件的所有导入映射 {alias → module}
func (st *SymbolTable) GetImports(filePath string) map[string]string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	result := make(map[string]string)
	if fileImports, ok := st.imports[filePath]; ok {
		for k, v := range fileImports {
			result[k] = v
		}
	}
	return result
}

// ResolveResult 解析结果（含置信度）
type ResolveResult struct {
	QualifiedName string
	Confidence    float64 // 0-1
}

// ResolveWithConfidence 解析符号名并返回置信度
// 置信度评分规则:
//   - 1.0: 文件局部符号精确匹配
//   - 0.9: 全局符号精确匹配（限定名完全一致）
//   - 0.8: 通过 import 映射成功解析
//   - 0.7: 类型前缀匹配（Type.Method）
//   - 0.5: 简单名多值匹配（同目录优先）
//   - 0.3: 简单名多值匹配（跨目录）
//   - 0.1: 完全未解析（返回原名）
func (st *SymbolTable) ResolveWithConfidence(name, fromFile string) ResolveResult {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// 1. 文件局部符号
	if fileMap, ok := st.fileSymbols[fromFile]; ok {
		if qn, ok := fileMap[name]; ok {
			return ResolveResult{qn, 1.0}
		}
	}

	// 2. 精确匹配全局符号
	if qn, ok := st.globalSymbols[name]; ok {
		return ResolveResult{qn, 0.9}
	}

	// 3. 带点名称
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		prefix, suffix := parts[0], parts[1]

		// 3a. import 映射
		if imports, ok := st.imports[fromFile]; ok {
			if module, ok := imports[prefix]; ok {
				candidate := module + "." + suffix
				if _, ok := st.globalSymbols[candidate]; ok {
					return ResolveResult{candidate, 0.85}
				}
				moduleParts := strings.Split(module, "/")
				pkgName := moduleParts[len(moduleParts)-1]
				candidate = pkgName + "." + suffix
				if _, ok := st.globalSymbols[candidate]; ok {
					return ResolveResult{candidate, 0.8}
				}
				return ResolveResult{candidate, 0.6}
			}
		}

		// 3b. 类型前缀
		if candidates, ok := st.nameToQualified[prefix]; ok && len(candidates) > 0 {
			return ResolveResult{candidates[0] + "." + suffix, 0.7}
		}
		if qn, ok := st.globalSymbols[prefix]; ok {
			return ResolveResult{qn + "." + suffix, 0.7}
		}
	}

	// 4. 简单名多值
	if candidates, ok := st.nameToQualified[name]; ok && len(candidates) > 0 {
		fromDir := fileDir(fromFile)
		for _, qn := range candidates {
			for file, fileMap := range st.fileSymbols {
				for _, fqn := range fileMap {
					if fqn == qn && fileDir(file) == fromDir {
						return ResolveResult{qn, 0.5}
					}
				}
			}
		}
		return ResolveResult{candidates[0], 0.3}
	}

	// 5. 未解析
	return ResolveResult{name, 0.1}
}
