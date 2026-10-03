package service

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

var (
	ErrImportURL         = errors.New("enter the http:// or https:// URL of a Git repository")
	ErrImportURLUserinfo = errors.New("put credentials in the username and token fields, not in the URL")
	ErrImportCredentials = errors.New("enter both a username and a token, or neither")
	ErrImportEmptySource = errors.New("the source repository is empty")
)

// The URL is kept in the job and its audit row for an hour or more.
const maxImportURLBytes = 2048

// ParseImportURL accepts http(s) only: go-git reads a bare path or file://
// URL as a repository on this server's disk. It refuses a query string:
// go-git appends /info/refs after it, so the clone could never work, and a
// ?token= secret would reach the audit log and the status page.
func ParseImportURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxImportURLBytes {
		return "", ErrImportURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.RawQuery != "" || u.ForceQuery {
		return "", ErrImportURL
	}
	if u.User != nil {
		return "", ErrImportURLUserinfo
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String(), nil
}

// importAuth returns a nil interface, not a nil *BasicAuth, which go-git
// would call methods on.
func importAuth(username, token string) transport.AuthMethod {
	if token == "" {
		return nil
	}
	return &githttp.BasicAuth{Username: username, Password: token}
}

const maxProgressLine = 200

// importProgress keeps the last line of the source's progress output, which
// git redraws in place with \r.
type importProgress struct {
	mu      sync.Mutex
	pending []byte
	last    string
}

func (p *importProgress) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending = append(p.pending, b...)
	for {
		i := bytes.IndexAny(p.pending, "\r\n")
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(p.pending[:i])); line != "" {
			p.last = truncateRunes(line, maxProgressLine)
		}
		p.pending = p.pending[i+1:]
	}
	if len(p.pending) > 4*maxProgressLine {
		p.pending = p.pending[len(p.pending)-4*maxProgressLine:]
	}
	return len(b), nil
}

func (p *importProgress) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
