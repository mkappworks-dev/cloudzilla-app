package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// Limits are the per-container resource caps, set by runner flags and never by a workflow.
type Limits struct {
	CPUs   string
	Memory string
	PIDs   int
	// User is the uid:gid every container runs as; it must not be root.
	User string
}

func DefaultLimits() Limits { return Limits{CPUs: "2", Memory: "4g", PIDs: 512, User: "1000:1000"} }

// Validate refuses limits that would weaken the container: a root user, or a missing cap.
func (l Limits) Validate() error {
	uid, _, _ := strings.Cut(l.User, ":")
	if uid == "" || uid == "0" || uid == "root" {
		return errors.New("user must be a non-root uid[:gid]")
	}
	if l.CPUs == "" || l.Memory == "" || l.PIDs < 1 {
		return errors.New("cpus, memory and pids-limit must all be set")
	}
	return nil
}

// hardening is the flag set of every container the runner starts: the Job's network and workspace volume are the
// only things it gets. Nothing a workflow says reaches this function, so none of it can be loosened.
func (l Limits) hardening(network, volume string) []string {
	return []string{
		"--privileged=false",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--user=" + l.User,
		"--cpus=" + l.CPUs,
		"--memory=" + l.Memory,
		"--memory-swap=" + l.Memory,
		"--pids-limit=" + strconv.Itoa(l.PIDs),
		"--network=" + network,
		"--volume=" + volume + ":" + workspaceDir,
	}
}

// The volume's root is root-owned and docker cp cannot change that, so a non-root Step can write only to directories
// created below it with a world-writable mode: the checkout and a home for tool caches.
const (
	workspaceDir = "/workspace"
	checkoutDir  = workspaceDir + "/src"
	homeDir      = workspaceDir + "/home"
)

// docker runs the docker CLI. Its binary comes from PATH unless Bin says otherwise.
type docker struct{ bin string }

func (d docker) cmd(ctx context.Context, args ...string) *exec.Cmd {
	bin := d.bin
	if bin == "" {
		bin = "docker"
	}
	return exec.CommandContext(ctx, bin, args...)
}

// run runs one docker command to completion, feeding stdin and returning its combined output on failure.
func (d docker) run(ctx context.Context, stdin io.Reader, args ...string) error {
	c := d.cmd(ctx, args...)
	c.Stdin = stdin
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w: %s", args[0], err, trimOut(out))
	}
	return nil
}

// output runs one docker command and returns its stdout.
func (d docker) output(ctx context.Context, args ...string) (string, error) {
	out, err := d.cmd(ctx, args...).Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w", args[0], err)
	}
	return string(out), nil
}

func trimOut(b []byte) string {
	const max = 512
	if len(b) > max {
		b = b[len(b)-max:]
	}
	return string(b)
}
