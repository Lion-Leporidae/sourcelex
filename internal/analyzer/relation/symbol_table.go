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

	// globalSymbols 全局符号映射: name -> qualified_name
	globalSymbols map[string]string

	// fileSymbols 文件级符号: file -> {name -> qualified_name}
	fileSymbols map[string]map[string]string

	// imports 导入映射: file -> {alias -> module}
	imports map[string]map[string]string
}

// NewSymbolTable 创建空符号表
func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		globalSymbols: make(map[string]string),
		fileSymbols:   make(map[string]map[string]string),
		imports:       make(map[string]map[string]string),
	}
}

// AddSymbol 添加符号到表中
// 参数:
//   - name: 符号名称
//   - qualifiedName: 完全限定名
//   - filePath: 所在文件
func (st *SymbolTable) AddSymbol(name, qualifiedName, filePath string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// 添加到全局符号（使用完全限定名）
	st.globalSymbols[qualifiedName] = qualifiedName

	// 添加到全局符号（使用简单名，可能会被覆盖）
	st.globalSymbols[name] = qualifiedName

	// 添加到文件级符号
	if st.fileSymbols[filePath] == nil {
		st.fileSymbols[filePath] = make(map[string]string)
	}
	st.fileSymbols[filePath][name] = qualifiedName
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
// 参数:
//   - name: 符号名（可能是简单名或带点的名称）
//   - fromFile: 调用发生的文件
//
// 返回:
//   - 完全限定名，如果无法解析则返回原名
func (st *SymbolTable) Resolve(name, fromFile string) string {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// 1. 先查文件局部符号
	if fileMap, ok := st.fileSymbols[fromFile]; ok {
		if qn, ok := fileMap[name]; ok {
			return qn
		}
	}

	// 2. 再查全局符号（尝试完全匹配）
	if qn, ok := st.globalSymbols[name]; ok {
		return qn
	}

	// 3. 处理带点的名称 (pkg.Func 或 Type.Method)
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		prefix := parts[0]
		suffix := parts[1]

		// 先检查是否是导入的模块
		if imports, ok := st.imports[fromFile]; ok {
			if module, ok := imports[prefix]; ok {
				return module + "." + suffix
			}
		}

		// 再检查是否是已知的类型
		if qn, ok := st.globalSymbols[prefix]; ok {
			return qn + "." + suffix
		}
	}

	// 4. 未解析的外部调用，返回原名
	return name
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
