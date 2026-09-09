package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintDetectsGormModelLeak(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.com/project\n\ngo 1.26.5\n")
	writeFixture(t, root, "services/blindpulse/v1/db/blindpulse/users/entities.go", `package users
type UserModel struct { ID string `+"`gorm:\"column:id\"`"+` }
func (UserModel) TableName() string { return "users" }
`)
	writeFixture(t, root, "services/blindpulse/v1/domain/user/service.go", `package user
import usersdb "example.com/project/services/blindpulse/v1/db/blindpulse/users"
type Service struct { persisted usersdb.UserModel }
`)

	violations, err := lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsViolation(violations, "L-6") {
		t.Fatalf("expected L-6 violation, got %v", violations)
	}
}

func TestLintAllowsModelInsideOwningPackage(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.com/project\n\ngo 1.26.5\n")
	writeFixture(t, root, "services/blindpulse/v1/db/blindpulse/users/entities.go", `package users
type UserModel struct { ID string `+"`gorm:\"column:id\"`"+` }
func mapUser(model UserModel) string { return model.ID }
`)

	violations, err := lint(root)
	if err != nil {
		t.Fatal(err)
	}
	if containsViolation(violations, "L-6") {
		t.Fatalf("unexpected L-6 violation: %v", violations)
	}
}

func writeFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func containsViolation(violations []string, rule string) bool {
	for _, violation := range violations {
		if strings.HasPrefix(violation, rule+" ") {
			return true
		}
	}
	return false
}
