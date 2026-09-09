package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type parsedFile struct {
	path string
	dir  string
	file *ast.File
}

type modelPackage struct {
	dir        string
	importPath string
	types      map[string]struct{}
}

func main() {
	violations, err := lint(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		os.Exit(1)
	}
	fmt.Println("architecture boundaries OK")
}

func lint(root string) ([]string, error) {
	module, err := modulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	files, err := parseServiceFiles(root)
	if err != nil {
		return nil, err
	}
	models := discoverModels(root, module, files)
	violations := make([]string, 0)
	for _, parsed := range files {
		violations = append(violations, lintImports(parsed)...)
		violations = append(violations, lintMoneyTypes(parsed)...)
		violations = append(violations, lintModelLeaks(parsed, models)...)
	}
	return violations, nil
}

func modulePath(goModPath string) (module string, err error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return "", fmt.Errorf("open go.mod: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close go.mod: %w", closeErr)
		}
	}()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("go.mod has no module directive")
}

func parseServiceFiles(root string) ([]parsedFile, error) {
	files := make([]parsedFile, 0)
	servicesRoot := filepath.Join(root, "services")
	err := filepath.Walk(servicesRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		files = append(files, parsedFile{path: path, dir: filepath.Dir(path), file: file})
		return nil
	})
	if os.IsNotExist(err) {
		return files, nil
	}
	return files, err
}

func discoverModels(root, module string, files []parsedFile) map[string]modelPackage {
	packages := make(map[string]modelPackage)
	for _, parsed := range files {
		if !strings.Contains(filepath.ToSlash(parsed.path), "/db/") {
			continue
		}
		modelNames := gormModelNames(parsed.file)
		if len(modelNames) == 0 {
			continue
		}
		relative, _ := filepath.Rel(root, parsed.dir)
		importPath := module + "/" + filepath.ToSlash(relative)
		pkg := packages[importPath]
		if pkg.types == nil {
			pkg = modelPackage{dir: parsed.dir, importPath: importPath, types: make(map[string]struct{})}
		}
		for name := range modelNames {
			pkg.types[name] = struct{}{}
		}
		packages[importPath] = pkg
	}
	return packages
}

func gormModelNames(file *ast.File) map[string]struct{} {
	models := make(map[string]struct{})
	for _, declaration := range file.Decls {
		switch node := declaration.(type) {
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if ok && hasGormTag(structure) {
					models[typeSpec.Name.Name] = struct{}{}
				}
			}
		case *ast.FuncDecl:
			if node.Name.Name == "TableName" && node.Recv != nil && len(node.Recv.List) == 1 {
				if name := receiverName(node.Recv.List[0].Type); name != "" {
					models[name] = struct{}{}
				}
			}
		}
	}
	return models
}

func hasGormTag(structure *ast.StructType) bool {
	for _, field := range structure.Fields.List {
		if field.Tag != nil && strings.Contains(field.Tag.Value, "gorm:") {
			return true
		}
	}
	return false
}

func receiverName(expression ast.Expr) string {
	switch node := expression.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return receiverName(node.X)
	default:
		return ""
	}
}

func lintImports(parsed parsedFile) []string {
	violations := make([]string, 0)
	slashPath := filepath.ToSlash(parsed.path)
	for _, imported := range parsed.file.Imports {
		name, _ := strconv.Unquote(imported.Path.Value)
		if strings.Contains(slashPath, "/domain/") && (name == "github.com/gin-gonic/gin" || name == "gorm.io/gorm" || name == "net/http") {
			violations = append(violations, fmt.Sprintf("L-2 %s imports %s", parsed.path, name))
		}
		if strings.Contains(slashPath, "/controllers/") && filepath.Base(parsed.path) != "service.go" && strings.Contains(name, "/db/") {
			violations = append(violations, fmt.Sprintf("L-3 %s imports %s", parsed.path, name))
		}
	}
	return violations
}

func lintMoneyTypes(parsed parsedFile) []string {
	slashPath := filepath.ToSlash(parsed.path)
	if !strings.Contains(slashPath, "/domain/") && !strings.Contains(slashPath, "/entities/domain/") {
		return nil
	}
	violations := make([]string, 0)
	ast.Inspect(parsed.file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && (ident.Name == "float32" || ident.Name == "float64") {
			violations = append(violations, fmt.Sprintf("M-1 %s declares %s", parsed.path, ident.Name))
		}
		return true
	})
	return violations
}

func lintModelLeaks(parsed parsedFile, models map[string]modelPackage) []string {
	aliases := make(map[string]modelPackage)
	violations := make([]string, 0)
	for _, imported := range parsed.file.Imports {
		importPath, _ := strconv.Unquote(imported.Path.Value)
		pkg, ok := models[importPath]
		if !ok || sameDirectory(parsed.dir, pkg.dir) {
			continue
		}
		if imported.Name != nil && imported.Name.Name == "." {
			violations = append(violations, fmt.Sprintf("L-6 %s dot-imports GORM model package %s", parsed.path, importPath))
			continue
		}
		alias := filepath.Base(importPath)
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		aliases[alias] = pkg
	}
	ast.Inspect(parsed.file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		alias, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		pkg, ok := aliases[alias.Name]
		if !ok {
			return true
		}
		if _, model := pkg.types[selector.Sel.Name]; model {
			violations = append(violations, fmt.Sprintf("L-6 %s references GORM model %s.%s from %s", parsed.path, alias.Name, selector.Sel.Name, pkg.importPath))
		}
		return true
	})
	return violations
}

func sameDirectory(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return leftAbs == rightAbs
}
