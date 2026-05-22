package service

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRepoService_ListTemplates(t *testing.T) {
	s := &RepoService{}

	gi := s.ListGitignoreTemplates()
	if len(gi) != 7 {
		t.Fatalf("ListGitignoreTemplates: want 7, got %d (%v)", len(gi), gi)
	}

	lic := s.ListLicenseTemplates()
	wantLicenses := []string{"mit", "apache-2.0", "gpl-3.0", "bsd-3-clause", "unlicense"}
	if len(lic) != len(wantLicenses) {
		t.Fatalf("ListLicenseTemplates: want %d, got %d (%v)", len(wantLicenses), len(lic), lic)
	}
	for i, want := range wantLicenses {
		if lic[i].Key != want {
			t.Errorf("license[%d] key: want %q, got %q", i, want, lic[i].Key)
		}
	}
}

func TestRepoService_TemplateContent(t *testing.T) {
	goContent, ok := gitignoreContent("Go")
	if !ok {
		t.Fatal("gitignoreContent(Go): not found")
	}
	if strings.TrimSpace(goContent) == "" {
		t.Error("gitignoreContent(Go): empty")
	}

	cppContent, ok := gitignoreContent("C++")
	if !ok {
		t.Fatal("gitignoreContent(C++): not found")
	}
	if strings.TrimSpace(cppContent) == "" {
		t.Error("gitignoreContent(C++): empty")
	}

	if _, ok := gitignoreContent("Cobol"); ok {
		t.Error("gitignoreContent(Cobol): want not-found")
	}
	if _, ok := gitignoreContent(""); ok {
		t.Error("gitignoreContent(empty): want not-found")
	}

	mit, ok := licenseContent("mit", "Ada Lovelace")
	if !ok {
		t.Fatal("licenseContent(mit): not found")
	}
	if !strings.Contains(mit, "MIT License") {
		t.Error("licenseContent(mit): missing 'MIT License'")
	}
	year := strconv.Itoa(time.Now().Year())
	if !strings.Contains(mit, year) {
		t.Errorf("licenseContent(mit): missing current year %s", year)
	}
	if !strings.Contains(mit, "Ada Lovelace") {
		t.Error("licenseContent(mit): missing owner name substitution")
	}
	if strings.Contains(mit, "[year]") || strings.Contains(mit, "[fullname]") {
		t.Error("licenseContent(mit): unsubstituted placeholder remains")
	}

	if _, ok := licenseContent("not-a-real-license", "Ada Lovelace"); ok {
		t.Error("licenseContent(not-a-real-license): want not-found")
	}
	if _, ok := licenseContent("", "Ada Lovelace"); ok {
		t.Error("licenseContent(empty): want not-found")
	}
}
