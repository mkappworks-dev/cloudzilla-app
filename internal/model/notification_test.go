package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

// Go can't enumerate a type's constants, so read them from the source to catch
// a new NotificationType that AllNotificationTypes (and the DB test) would miss.
func TestAllNotificationTypes_ListsEveryConstant(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "notification.go", nil, 0)
	if err != nil {
		t.Fatalf("parse notification.go: %v", err)
	}
	var declared []NotificationType
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "NotificationType" {
				continue
			}
			for _, v := range vs.Values {
				s, err := strconv.Unquote(v.(*ast.BasicLit).Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", v.(*ast.BasicLit).Value, err)
				}
				declared = append(declared, NotificationType(s))
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no NotificationType constants in notification.go")
	}
	for _, typ := range declared {
		if !slices.Contains(AllNotificationTypes, typ) {
			t.Errorf("AllNotificationTypes is missing %q", typ)
		}
	}
	if len(AllNotificationTypes) != len(declared) {
		t.Errorf("AllNotificationTypes has %d entries, notification.go declares %d constants", len(AllNotificationTypes), len(declared))
	}
}
