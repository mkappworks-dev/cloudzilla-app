package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

type Repo struct {
	Owner string
	Name  string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

var (
	scpRemote = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):([^/].*)$`)
	nameChars = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// RepoFromRemote reads the origin remote of the checkout containing dir.
func RepoFromRemote(dir, host string) (Repo, error) {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := stderr.String()
		switch {
		case errors.Is(err, exec.ErrNotFound):
			return Repo{}, errors.New("git is not installed; pass owner/repo or -R owner/repo")
		case strings.Contains(msg, "not a git repository"):
			return Repo{}, errors.New("not inside a git checkout; pass owner/repo or -R owner/repo")
		case strings.Contains(msg, "No such remote"):
			return Repo{}, errors.New("this checkout has no origin remote; pass owner/repo or -R owner/repo")
		}
		return Repo{}, fmt.Errorf("reading the origin remote: %s", oneLine(msg))
	}
	return ParseRemote(strings.TrimSpace(string(out)), host)
}

// ParseRemote accepts HTTP(S) and SSH remote URLs and the scp-like user@host:owner/repo form.
func ParseRemote(remote, host string) (Repo, error) {
	want, err := url.Parse(NormalizeHost(host))
	if err != nil || want.Host == "" {
		return Repo{}, fmt.Errorf("bad host %q", host)
	}
	var gotHost, path string
	var hostMatches bool
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" {
			return Repo{}, fmt.Errorf("remote %q is not a URL or user@host:path", remote)
		}
		path = u.Path
		if u.Scheme == "http" || u.Scheme == "https" {
			gotHost = hostPort(u)
			hostMatches = gotHost == hostPort(want)
		} else {
			// The SSH port is unrelated to the web port, so only the name can match.
			gotHost = strings.ToLower(u.Hostname())
			hostMatches = gotHost == strings.ToLower(want.Hostname())
		}
	case scpRemote.MatchString(remote):
		m := scpRemote.FindStringSubmatch(remote)
		gotHost, path = strings.ToLower(m[1]), m[2]
		hostMatches = gotHost == strings.ToLower(want.Hostname())
	default:
		return Repo{}, fmt.Errorf("remote %q is not a URL or user@host:path", remote)
	}
	if !hostMatches {
		return Repo{}, fmt.Errorf("origin remote is %s, not %s; pass owner/repo or -R owner/repo", gotHost, hostPort(want))
	}
	return splitRepo(path, remote)
}

// ParseRepo accepts owner/repo, host/owner/repo or any remote form ParseRemote does.
func ParseRepo(s, host string) (Repo, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") || scpRemote.MatchString(s) {
		return ParseRemote(s, host)
	}
	parts := strings.Split(s, "/")
	if len(parts) == 3 && strings.ContainsAny(parts[0], ".:") {
		want, err := url.Parse(NormalizeHost(host))
		if err != nil || want.Host == "" {
			return Repo{}, fmt.Errorf("bad host %q", host)
		}
		if !strings.EqualFold(parts[0], want.Host) && !strings.EqualFold(parts[0], want.Hostname()) {
			return Repo{}, fmt.Errorf("repository is on %s, not %s", parts[0], want.Host)
		}
		return splitRepo(parts[1]+"/"+parts[2], s)
	}
	return splitRepo(s, s)
}

func splitRepo(path, shown string) (Repo, error) {
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || !nameChars.MatchString(owner) || !nameChars.MatchString(name) {
		return Repo{}, fmt.Errorf("%q is not owner/repo", shown)
	}
	return Repo{Owner: owner, Name: name}, nil
}

// hostPort drops the scheme's default port so https://h and https://h:443 compare equal.
func hostPort(u *url.URL) string {
	h, port := strings.ToLower(u.Hostname()), u.Port()
	if port == "" || port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http" {
		return h
	}
	return h + ":" + port
}
