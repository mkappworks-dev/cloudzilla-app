package backup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParsePgVersion(t *testing.T) {
	for _, c := range []struct {
		out       string
		wantMajor int
		wantFull  string
	}{
		{"pg_dump (PostgreSQL) 18.6\n", 18, "18.6"},
		{"pg_dump (PostgreSQL) 16.2 (Debian 16.2-1.pgdg120+2)\n", 16, "16.2"},
		{"pg_restore (PostgreSQL) 18beta1\n", 18, "18beta1"},
		{"pg_dump (PostgreSQL) 18devel\n", 18, "18devel"},
		{"pg_dump (PostgreSQL) 9.6.24\n", 9, "9.6.24"},
	} {
		major, full, err := ParsePgVersion(c.out)
		if err != nil || major != c.wantMajor || full != c.wantFull {
			t.Errorf("ParsePgVersion(%q) = %d, %q, %v; want %d, %q", c.out, major, full, err, c.wantMajor, c.wantFull)
		}
	}
	if _, _, err := ParsePgVersion("not a version"); err == nil {
		t.Error("garbage output parsed")
	}
}

func TestServerMajor(t *testing.T) {
	for num, want := range map[int]int{180006: 18, 170002: 17, 90624: 9, 100001: 10} {
		if got := ServerMajor(num); got != want {
			t.Errorf("ServerMajor(%d) = %d, want %d", num, got, want)
		}
	}
}

func fakePgTool(t *testing.T, name, versionLine string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\necho '" + versionLine + "'\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckClient(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		line    string
		server  int
		wantErr string
	}{
		{"equal", "pg_dump (PostgreSQL) 18.6", 18, ""},
		{"newer", "pg_dump (PostgreSQL) 19.0", 18, ""},
		{"older", "pg_dump (PostgreSQL) 16.2", 18, "older than the server"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			full, err := CheckClient(ctx, fakePgTool(t, "pg_dump", c.line), c.server, "the server")
			if c.wantErr == "" {
				if err != nil || full == "" {
					t.Fatalf("CheckClient = %q, %v", full, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), "postgresql18-client") {
				t.Fatalf("err = %v, want it to contain %q and name the needed version", err, c.wantErr)
			}
		})
	}

	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := CheckClient(ctx, "pg_dump", 18, "the server")
		if err == nil || !strings.Contains(err.Error(), "version 18") {
			t.Fatalf("err = %v, want one naming version 18", err)
		}
	})
}

func TestConnEnv(t *testing.T) {
	for _, c := range []struct {
		name, dsn string
		want      []string
		db        string
	}{
		{
			"url", "postgres://cz:p%40ss@db.internal:5433/cloudzilla?sslmode=require",
			[]string{"PGHOST=db.internal", "PGPORT=5433", "PGUSER=cz", "PGPASSWORD=p@ss", "PGDATABASE=cloudzilla", "PGSSLMODE=require"}, "cloudzilla",
		},
		{
			"keyword", "host=localhost port=5432 user=cz password=secret dbname=app sslmode=disable",
			[]string{"PGHOST=localhost", "PGPORT=5432", "PGUSER=cz", "PGPASSWORD=secret", "PGDATABASE=app", "PGSSLMODE=disable"}, "app",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, db, err := ConnEnv(c.dsn)
			if err != nil {
				t.Fatal(err)
			}
			if db != c.db {
				t.Errorf("database = %q, want %q", db, c.db)
			}
			for _, w := range c.want {
				if !slices.Contains(env, w) {
					t.Errorf("env %v lacks %s", env, w)
				}
			}
		})
	}
	if _, _, err := ConnEnv("postgres://%zz"); err == nil {
		t.Error("malformed DSN accepted")
	}
}
