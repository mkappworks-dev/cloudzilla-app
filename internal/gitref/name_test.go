package gitref

import (
	"errors"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"main", "feature/x", "v1.2.3", "fix_bug-2", "a/b/c", "release-1.0", "a.b", "x@y", "ünï/çode",
		"a-", "foo.lockx", "lock", "a/b.lockfile", "@x", "x@", "a{b}", "#1", "100%", "it's", "a./b",
	}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"", "@", "a..b", "..", "/a", "a/", "a//b", "/",
		".hidden", "a/.hidden", "bad.lock", "a/bad.lock", "a/bad.lock/b", "trail.",
		"a@{b", "@{", "x y", " x", "x ", "a\tb", "a\nb", "a\x00b", "a\x1fb", "a\x7fb",
		"a~b", "a^b", "a:b", "a?b", "a*b", "a[b", `a\b`,
	}
	for _, name := range invalid {
		err := ValidateName(name)
		if !errors.Is(err, ErrInvalidName) {
			t.Errorf("ValidateName(%q) = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestValidateBranchName(t *testing.T) {
	for _, name := range []string{"main", "feature/x", "a-b"} {
		if err := ValidateBranchName(name); err != nil {
			t.Errorf("ValidateBranchName(%q) = %v, want nil", name, err)
		}
	}
	// A leading dash would read as an option to the git CLI; HEAD collides with the symbolic ref.
	for _, name := range []string{"-x", "-", "a..b", "HEAD"} {
		if err := ValidateBranchName(name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("ValidateBranchName(%q) = %v, want ErrInvalidName", name, err)
		}
	}
	// Tags may start with a dash as far as git is concerned.
	if err := ValidateName("-x"); err != nil {
		t.Errorf("ValidateName(-x) = %v, want nil", err)
	}
}
