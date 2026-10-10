package main

// Tests for the seed command. They need TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/seed"
)

var smallWorld = []string{"--users", "3", "--orgs", "1", "--repos", "2", "--seed", "7"}

func TestSeedCmd_FillsAFreshInstanceAndSaysHowToSignIn(t *testing.T) {
	c := newInstanceConfig(t, scratchDatabase(t, true))

	stdout, stderr, err := runAdmin(t, seedCmd(), c.file(t), nil, smallWorld...)
	if err != nil {
		t.Fatalf("seed: %v\n%s", err, stderr)
	}
	if !regexp.MustCompile(`\d+ users, 1 orgs, 2 repos \(\d+ commits\)`).MatchString(stdout) {
		t.Errorf("stdout = %q; want the counts of what was created", stdout)
	}
	assertOutput(t, "stdout", stdout, "people", "repositories", "Sign in as "+seed.AdminEmail+" (username "+seed.AdminUsername+")")

	owners, err := os.ReadDir(c.reposRoot)
	if err != nil || len(owners) == 0 {
		t.Errorf("repos root holds %d entries, %v; want the seeded repositories", len(owners), err)
	}
}

func TestSeedCmd_UsesThePasswordItIsGiven(t *testing.T) {
	dsn := scratchDatabase(t, true)
	c := newInstanceConfig(t, dsn)

	if _, stderr, err := runAdmin(t, seedCmd(), c.file(t), nil, append([]string{"--password", "a-seeded-password"}, smallWorld...)...); err != nil {
		t.Fatalf("seed: %v\n%s", err, stderr)
	}
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var hash string
	if err := d.QueryRow(`SELECT password_hash FROM users WHERE email = $1`, seed.AdminEmail).Scan(&hash); err != nil {
		t.Fatalf("admin row: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("a-seeded-password")); err != nil {
		t.Errorf("the admin does not sign in with --password: %v", err)
	}
}

func TestSeedCmd_RefusesAnInstanceThatIsNotFresh(t *testing.T) {
	c := newInstanceConfig(t, scratchDatabase(t, true))
	cfg := c.file(t)
	if _, stderr, err := runAdmin(t, seedCmd(), cfg, nil, smallWorld...); err != nil {
		t.Fatalf("first seed: %v\n%s", err, stderr)
	}

	_, _, err := runAdmin(t, seedCmd(), cfg, nil, smallWorld...)
	if !errors.Is(err, seed.ErrNotFresh) {
		t.Errorf("second seed err = %v; want %v", err, seed.ErrNotFresh)
	}
}

func TestSeedCmd_RefusesANonEmptyReposRoot(t *testing.T) {
	c := newInstanceConfig(t, scratchDatabase(t, true))
	writeFile(t, filepath.Join(c.reposRoot, "stray"), "x")

	_, _, err := runAdmin(t, seedCmd(), c.file(t), nil, smallWorld...)
	if !errors.Is(err, seed.ErrNotFresh) {
		t.Errorf("err = %v; want %v", err, seed.ErrNotFresh)
	}
}

func TestSeedCmd_Failures(t *testing.T) {
	t.Run("a config that does not exist", func(t *testing.T) {
		if _, _, err := runAdmin(t, seedCmd(), filepath.Join(t.TempDir(), "missing.yaml"), nil); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("a database that is down", func(t *testing.T) {
		c := newInstanceConfig(t, "postgres://nobody:x@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
		if _, _, err := runAdmin(t, seedCmd(), c.file(t), nil, smallWorld...); err == nil {
			t.Error("want an error")
		}
	})
}
