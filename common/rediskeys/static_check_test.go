package keys

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRedisKeysFileOnlyDefinesConstants(t *testing.T) {
	fset := token.NewFileSet()
	file := parseRedisKeysFile(t, fset)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			pos := fset.Position(decl.Pos())
			t.Fatalf("redis_keys.go 只允许定义 Redis key 常量，line=%d", pos.Line)
		}
		if gen.Tok != token.CONST {
			pos := fset.Position(gen.Pos())
			t.Fatalf("redis_keys.go 不允许出现 %s 声明，line=%d", gen.Tok, pos.Line)
		}
	}
}

func TestRedisKeyCommentsIncludeTypeAndTTL(t *testing.T) {
	fset := token.NewFileSet()
	file := parseRedisKeysFile(t, fset)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			comment := valueSpec.Doc.Text()
			for _, name := range valueSpec.Names {
				if !strings.Contains(comment, "Redis 类型：") || !strings.Contains(comment, "TTL 过期规则：") {
					pos := fset.Position(valueSpec.Pos())
					t.Fatalf("%s 注释必须包含 Redis 类型和 TTL 过期规则，line=%d", name.Name, pos.Line)
				}
			}
		}
	}
}

func TestRedisKeyConstantsStayInRedisKeysFile(t *testing.T) {
	dir := redisKeysPackageDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == "redis_keys.go" || name == "cache_marker.go" {
			continue
		}
		fset := token.NewFileSet()
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			pos := fset.Position(gen.Pos())
			t.Fatalf("%s 不应定义 Redis key 常量，请放到 redis_keys.go，line=%d", name, pos.Line)
		}
	}
}

func TestNoReferenceOnlyMoneyBalanceTemplateName(t *testing.T) {
	dir := redisKeysPackageDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(data), "RedisHash"+"MoneyBalanceTemplate") {
			t.Fatalf("%s 包含仅用于示例的余额模板名", name)
		}
	}
}

func parseRedisKeysFile(t *testing.T, fset *token.FileSet) *ast.File {
	t.Helper()
	path := filepath.Join(redisKeysPackageDir(t), "redis_keys.go")
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse redis_keys.go: %v", err)
	}
	return file
}

func redisKeysPackageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Dir(file)
}
