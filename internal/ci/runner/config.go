// Package runner is cz-runner's engine: it claims Jobs over the runner protocol and runs each Step in a hardened
// container by shelling out to the docker CLI.
package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is what `cz-runner register` writes and `cz-runner run` reads. It holds the runner token, so the file is 0600.
type Config struct {
	Server string `yaml:"server"`
	Token  string `yaml:"token"`
	Name   string `yaml:"name"`
	// RunnerID is the server's id for this runner; it labels the Docker resources the runner makes.
	RunnerID int64    `yaml:"runner_id,omitempty"`
	Labels   []string `yaml:"labels"`
	// Images maps a runs-on label to the image a Job with that label runs in when it names none.
	Images map[string]string `yaml:"images"`
	// AllowedImages limits the `container:` a workflow may ask for; a trailing * matches a prefix. Empty allows any
	// image. Images in Images are always allowed.
	AllowedImages []string `yaml:"allowed_images"`
}

// Owner is the value of the cz.runner label. A config written before runner_id existed falls back to a digest of the
// token, which also identifies one runner and is not the token.
func (c *Config) Owner() string {
	if c.RunnerID > 0 {
		return strconv.FormatInt(c.RunnerID, 10)
	}
	sum := sha256.Sum256([]byte(c.Token))
	return "t" + hex.EncodeToString(sum[:6])
}

// DefaultConfigPath is where the config lives when --config is not given.
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config dir: %w", err)
	}
	return filepath.Join(dir, "cz-runner", "config.yaml"), nil
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Server == "" || c.Token == "" || len(c.Labels) == 0 {
		return nil, fmt.Errorf("%s needs server, token and labels; run `cz-runner register`", path)
	}
	return &c, nil
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// ResolveImage picks the image for a Job: the one its workflow asks for if allowed, else the first runs-on label's
// default. The error is the reason the Job fails with.
func (c *Config) ResolveImage(container string, runsOn []string) (string, error) {
	if container != "" {
		if err := validImage(container); err != nil {
			return "", fmt.Errorf("image %q: %w", container, err)
		}
		if !c.imageAllowed(container) {
			return "", fmt.Errorf("image %q is not in this runner's allowed_images", container)
		}
		return container, nil
	}
	for _, l := range runsOn {
		if img := c.Images[l]; img != "" {
			return img, nil
		}
	}
	return "", fmt.Errorf("this runner has no image for labels %s and the job names no container", strings.Join(runsOn, ", "))
}

func (c *Config) imageAllowed(img string) bool {
	if len(c.AllowedImages) == 0 {
		return true
	}
	for _, def := range c.Images {
		if def == img {
			return true
		}
	}
	for _, a := range c.AllowedImages {
		if a == img || (strings.HasSuffix(a, "*") && strings.HasPrefix(img, strings.TrimSuffix(a, "*"))) {
			return true
		}
	}
	return false
}

// validImage keeps a workflow's image from being read by docker as a flag or from carrying shell-ish junk.
func validImage(img string) error {
	if img == "" || img[0] == '-' || len(img) > 255 {
		return errors.New("not a valid image reference")
	}
	for _, r := range img {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._-/:@", r):
		default:
			return errors.New("not a valid image reference")
		}
	}
	return nil
}
