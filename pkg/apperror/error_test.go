package apperror

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestCatalogComplete(t *testing.T) {
	t.Parallel()
	for code, item := range catalog {
		if code == "" || item.HTTP == 0 || item.Message == "" {
			t.Errorf("incomplete catalog entry %q: %+v", code, item)
		}
	}
	for code, locations := range referencedCodes(t) {
		if _, exists := catalog[code]; !exists {
			t.Errorf("code %q is referenced at %s but missing from catalog", code, strings.Join(locations, ", "))
		}
	}
}

func TestWrapAndIs(t *testing.T) {
	t.Parallel()
	cause := errors.New("database detail")
	err := Wrap(cause, "USER_NOT_FOUND")
	if !errors.Is(err, cause) || !Is(err, "USER_NOT_FOUND") {
		t.Fatalf("error chain was not preserved: %v", err)
	}
}

func referencedCodes(t *testing.T) map[string][]string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test location")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	references := make(map[string][]string)
	err := filepath.Walk(repositoryRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		aliases := apperrorAliases(file)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			function, codeIndex, ok := catalogCall(call.Fun, aliases)
			if !ok || codeIndex >= len(call.Args) {
				return true
			}
			literal, ok := call.Args[codeIndex].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			code, err := strconv.Unquote(literal.Value)
			if err == nil {
				position := fileSet.Position(call.Pos())
				relative, _ := filepath.Rel(repositoryRoot, position.Filename)
				references[code] = append(references[code], fmt.Sprintf("%s:%d (%s)", filepath.ToSlash(relative), position.Line, function))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return references
}

func apperrorAliases(file *ast.File) map[string]struct{} {
	aliases := make(map[string]struct{})
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || path != "github.com/jblabs/blindpulse-be/pkg/apperror" {
			continue
		}
		alias := "apperror"
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		aliases[alias] = struct{}{}
	}
	return aliases
}

func catalogCall(expression ast.Expr, aliases map[string]struct{}) (function string, codeIndex int, ok bool) {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return "", 0, false
	}
	alias, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", 0, false
	}
	if _, imported := aliases[alias.Name]; !imported {
		return "", 0, false
	}
	switch selector.Sel.Name {
	case "New", "Newf", "WithFields":
		return selector.Sel.Name, 0, true
	case "Wrap":
		return selector.Sel.Name, 1, true
	default:
		return "", 0, false
	}
}

func TestUnknownCodeBecomesInternalError(t *testing.T) {
	t.Parallel()
	err := New("DOES_NOT_EXIST")
	if err.Code != "INTERNAL_ERROR" || err.HTTP != 500 {
		t.Fatalf("unexpected fallback: %+v", err)
	}
}
