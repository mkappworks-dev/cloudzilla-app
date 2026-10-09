package backup

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

var pgVersionRE = regexp.MustCompile(`\(PostgreSQL\)\s+((\d+)(?:\.\d+)*(?:[a-z]+\d*)?)`)

// ParsePgVersion reads `pg_dump --version` output. Releases before 10 have a
// two-part major (9.6); the first number is enough to compare against any
// server this code supports.
func ParsePgVersion(out string) (major int, full string, err error) {
	m := pgVersionRE.FindStringSubmatch(out)
	if m == nil {
		return 0, "", fmt.Errorf("unrecognised version output %q", strings.TrimSpace(out))
	}
	major, _ = strconv.Atoi(m[2])
	return major, m[1], nil
}

// ServerMajor turns SHOW server_version_num (180006) into a major (18).
func ServerMajor(versionNum int) int { return versionNum / 10000 }

// CheckClient resolves tool (a name on PATH or a path) and fails unless its
// major version is at least needMajor: pg_dump refuses an older client
// against a newer server, and pg_restore can't read a newer archive. against
// names what needMajor came from ("the server", "the backup").
func CheckClient(ctx context.Context, tool string, needMajor int, against string) (full string, err error) {
	bin, err := exec.LookPath(tool)
	if err != nil {
		return "", fmt.Errorf("%s not found: install postgresql%d-client (version %d or newer) or point --%s at it", tool, needMajor, needMajor, strings.ReplaceAll(filepath.Base(tool), "_", "-"))
	}
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("run %s --version: %w", bin, err)
	}
	major, full, err := ParsePgVersion(string(out))
	if err != nil {
		return "", fmt.Errorf("%s: %w", bin, err)
	}
	if major < needMajor {
		return "", fmt.Errorf("%s %s is older than %s (PostgreSQL %d): install postgresql%d-client or newer", tool, full, against, needMajor, needMajor)
	}
	return full, nil
}

// ConnEnv splits a DSN into the libpq environment pg_dump and pg_restore
// read, so the password never appears in the process list. The returned name
// is the database, which pg_restore needs as an argument to connect at all.
func ConnEnv(dsn string) (env []string, dbname string, err error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return nil, "", fmt.Errorf("parse database DSN: %w", err)
	}
	env = []string{
		"PGHOST=" + cfg.Host,
		"PGPORT=" + strconv.Itoa(int(cfg.Port)),
		"PGUSER=" + cfg.User,
		"PGPASSWORD=" + cfg.Password,
		"PGDATABASE=" + cfg.Database,
	}
	if mode := dsnParam(dsn, "sslmode"); mode != "" {
		env = append(env, "PGSSLMODE="+mode)
	}
	return env, cfg.Database, nil
}

// pgconn exposes the TLS config but not the sslmode string libpq wants.
func dsnParam(dsn, key string) string {
	if strings.Contains(dsn, "://") {
		if u, err := url.Parse(dsn); err == nil {
			return u.Query().Get(key)
		}
		return ""
	}
	for _, f := range strings.Fields(dsn) {
		if v, ok := strings.CutPrefix(f, key+"="); ok {
			return strings.Trim(v, "'")
		}
	}
	return ""
}
