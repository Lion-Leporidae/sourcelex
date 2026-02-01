// Package parser provides Tree-sitter based code parsing.
// Corresponds to: REPOMIND_ARCHITECTURE_MINDMAP.md - 步骤3: Tree-sitter AST解析
//
// REQUIREMENTS:
// - CGO_ENABLED=1
// - C compiler (gcc from MinGW-w64 or MSYS2)
// - Set CC=gcc environment variable
package parser

import (
	"context"
	"fmt"
	"os"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
)

// Parser wraps tree-sitter parser with multi-language support
// Note: Tree-sitter parsers are NOT thread-safe, so we create new instances per parse
type Parser struct {
	mu sync.Mutex
}

// New creates a new multi-language parser
func New() *Parser {
	return &Parser{}
}

// getLanguage returns the tree-sitter language for a given language name
func getLanguage(lang string) *sitter.Language {
	switch lang {
	case "python":
		return python.GetLanguage()
	case "go":
		return golang.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "javascript", "typescript":
		return javascript.GetLanguage()
	default:
		return nil
	}
}

// ParseResult contains the parsed AST and metadata
type ParseResult struct {
	Tree     *sitter.Tree
	Language string
	FilePath string
	Content  []byte
}

// ParseFile parses a source file and returns the AST
// Creates a new parser instance for thread safety
func (p *Parser) ParseFile(ctx context.Context, filePath, language string) (*ParseResult, error) {
	lang := getLanguage(language)
	if lang == nil {
		return nil, fmt.Errorf("不支持的语言: %s", language)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 为每次解析创建新的 parser 实例（Tree-sitter 非线程安全）
	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	defer parser.Close()

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("解析失败: %w", err)
	}

	return &ParseResult{
		Tree:     tree,
		Language: language,
		FilePath: filePath,
		Content:  content,
	}, nil
}

// ParseContent parses source code content directly
func (p *Parser) ParseContent(ctx context.Context, content []byte, language string) (*sitter.Tree, error) {
	lang := getLanguage(language)
	if lang == nil {
		return nil, fmt.Errorf("不支持的语言: %s", language)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	defer parser.Close()

	return parser.ParseCtx(ctx, nil, content)
}

// SupportedLanguages returns list of supported languages
func (p *Parser) SupportedLanguages() []string {
	return []string{"python", "go", "java", "javascript", "typescript"}
}
