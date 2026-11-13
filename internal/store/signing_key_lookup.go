package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// The lookups below serve a whole page of commits at once: a constant number of queries, never one per commit.

// ListByFingerprints returns the keys with any of the SHA256 fingerprints, in one query.
func (s *SSHSigningKeyStore) ListByFingerprints(ctx context.Context, fingerprints []string) ([]model.SSHSigningKey, error) {
	if len(fingerprints) == 0 {
		return nil, nil
	}
	ph, args := textPlaceholders(1, fingerprints)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sshSigningKeyColumns+`
		 FROM ssh_signing_keys WHERE fingerprint IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("ssh signing keys by fingerprint: %w", err)
	}
	defer rows.Close()
	var keys []model.SSHSigningKey
	for rows.Next() {
		k, err := scanSSHSigningKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// textPlaceholders is inPlaceholders for strings.
func textPlaceholders(start int, vals []string) (string, []any) {
	ph := make([]string, len(vals))
	args := make([]any, len(vals))
	for i, v := range vals {
		ph[i] = fmt.Sprintf("$%d", start+i)
		args[i] = v
	}
	return strings.Join(ph, ","), args
}
