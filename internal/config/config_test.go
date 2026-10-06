package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

func TestLoad_TrustedProxiesFromEnv(t *testing.T) {
	t.Setenv("CZ_SERVER_TRUSTED_PROXIES", "10.0.0.0/8,192.168.1.7")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"10.0.0.0/8", "192.168.1.7"}; !slices.Equal(cfg.Server.TrustedProxies, want) {
		t.Errorf("want %v, got %v", want, cfg.Server.TrustedProxies)
	}
}

func TestLoad_ImportDefaultsAndEnv(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Import.AllowLocalNetworks || cfg.Import.Timeout != 30*time.Minute {
		t.Errorf("defaults: want allow_local_networks=false timeout=30m, got %+v", cfg.Import)
	}

	t.Setenv("CZ_IMPORT_ALLOW_LOCAL_NETWORKS", "true")
	t.Setenv("CZ_IMPORT_TIMEOUT", "5m")
	cfg, err = config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Import.AllowLocalNetworks || cfg.Import.Timeout != 5*time.Minute {
		t.Errorf("env: want allow_local_networks=true timeout=5m, got %+v", cfg.Import)
	}
}

func TestLoad_RateLimitDefaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.RateLimitConfig{
		Enabled: true,
		Window:  time.Hour,
		Core:    config.RateBudget{Authenticated: 5000, Anonymous: 1000},
		Git:     config.RateBudget{Authenticated: 1000, Anonymous: 200},
		Archive: config.RateBudget{Authenticated: 100, Anonymous: 20},
		Search:  config.RateBudget{Authenticated: 600, Anonymous: 60},
	}
	if cfg.RateLimit != want {
		t.Errorf("want %+v, got %+v", want, cfg.RateLimit)
	}
}

func TestLoad_RateLimitFromEnv(t *testing.T) {
	env := map[string]string{
		"CZ_RATE_LIMIT_ENABLED":               "false",
		"CZ_RATE_LIMIT_WINDOW":                "10m",
		"CZ_RATE_LIMIT_CORE_AUTHENTICATED":    "1",
		"CZ_RATE_LIMIT_CORE_ANONYMOUS":        "2",
		"CZ_RATE_LIMIT_GIT_AUTHENTICATED":     "3",
		"CZ_RATE_LIMIT_GIT_ANONYMOUS":         "4",
		"CZ_RATE_LIMIT_ARCHIVE_AUTHENTICATED": "5",
		"CZ_RATE_LIMIT_ARCHIVE_ANONYMOUS":     "6",
		"CZ_RATE_LIMIT_SEARCH_AUTHENTICATED":  "7",
		"CZ_RATE_LIMIT_SEARCH_ANONYMOUS":      "0",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.RateLimitConfig{
		Window:  10 * time.Minute,
		Core:    config.RateBudget{Authenticated: 1, Anonymous: 2},
		Git:     config.RateBudget{Authenticated: 3, Anonymous: 4},
		Archive: config.RateBudget{Authenticated: 5, Anonymous: 6},
		Search:  config.RateBudget{Authenticated: 7, Anonymous: 0},
	}
	if cfg.RateLimit != want {
		t.Errorf("want %+v, got %+v", want, cfg.RateLimit)
	}
}

func TestLoad_RateLimitFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "rate_limit:\n  window: 30m\n  git:\n    anonymous: 50\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RateLimit.Window != 30*time.Minute || cfg.RateLimit.Git.Anonymous != 50 || cfg.RateLimit.Git.Authenticated != 1000 {
		t.Errorf("want window 30m, git anonymous 50 and the default git authenticated budget, got %+v", cfg.RateLimit)
	}
}

func TestLoad_RateLimitRejectsBadValues(t *testing.T) {
	for _, tc := range []struct{ env, value string }{
		{"CZ_RATE_LIMIT_CORE_ANONYMOUS", "-1"},
		{"CZ_RATE_LIMIT_SEARCH_AUTHENTICATED", "-5"},
		{"CZ_RATE_LIMIT_WINDOW", "0s"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			if _, err := config.Load(""); err == nil {
				t.Errorf("%s=%s: want a startup error", tc.env, tc.value)
			}
		})
	}
}
