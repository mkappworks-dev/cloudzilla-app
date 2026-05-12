package service

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestValidateGistFiles_MaxFiles(t *testing.T) {
	files := make([]model.GistFile, maxGistFiles+1)
	for i := range files {
		files[i] = model.GistFile{Filename: strings.Repeat("a", i+1) + ".txt", Content: "x"}
	}
	if err := validateGistFiles(files); err == nil {
		t.Fatal("expected error for too many files, got nil")
	}
}

func TestValidateGistFiles_MaxSize(t *testing.T) {
	files := []model.GistFile{
		{Filename: "big.txt", Content: strings.Repeat("x", maxGistFileSize+1)},
	}
	if err := validateGistFiles(files); err == nil {
		t.Fatal("expected error for file exceeding 1 MB, got nil")
	}
}

func TestValidateGistFiles_EmptyFilename(t *testing.T) {
	files := []model.GistFile{
		{Filename: "   ", Content: "hello"},
	}
	if err := validateGistFiles(files); err == nil {
		t.Fatal("expected error for empty filename, got nil")
	}
}

func TestValidateGistFiles_DuplicateFilename(t *testing.T) {
	files := []model.GistFile{
		{Filename: "foo.go", Content: "a"},
		{Filename: "foo.go", Content: "b"},
	}
	if err := validateGistFiles(files); err == nil {
		t.Fatal("expected error for duplicate filenames, got nil")
	}
}

func TestValidateGistFiles_ValidInput(t *testing.T) {
	files := []model.GistFile{
		{Filename: "main.go", Content: "package main"},
		{Filename: "README.md", Content: "# hello"},
	}
	if err := validateGistFiles(files); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateGistID_Length(t *testing.T) {
	id, err := generateGistID()
	if err != nil {
		t.Fatalf("generateGistID error: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("expected 32-char hex id, got %d chars: %s", len(id), id)
	}
}

func TestGenerateGistID_Unique(t *testing.T) {
	id1, _ := generateGistID()
	id2, _ := generateGistID()
	if id1 == id2 {
		t.Fatal("expected unique IDs, got identical")
	}
}
