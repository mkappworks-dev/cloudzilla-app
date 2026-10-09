package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestLoad_RateLimitDisabledSkipsValidation(t *testing.T) {
	t.Setenv("CZ_RATE_LIMIT_ENABLED", "false")
	t.Setenv("CZ_RATE_LIMIT_WINDOW", "0s")
	if _, err := config.Load(""); err != nil {
		t.Errorf("a disabled limiter's settings don't matter; got %v", err)
	}
}

func TestLoad_QuotaDefaultsToUnlimited(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Quota != (config.QuotaConfig{}) {
		t.Errorf("want every quota at 0, got %+v", cfg.Quota)
	}
}

func TestLoad_QuotaFromEnv(t *testing.T) {
	t.Setenv("CZ_QUOTA_USER_REPOS", "50")
	t.Setenv("CZ_QUOTA_USER_STORAGE_BYTES", "10737418240")
	t.Setenv("CZ_QUOTA_ORG_REPOS", "200")
	t.Setenv("CZ_QUOTA_ORG_STORAGE_BYTES", "107374182400")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.QuotaConfig{
		User: config.QuotaLimits{Repos: 50, StorageBytes: 10 << 30},
		Org:  config.QuotaLimits{Repos: 200, StorageBytes: 100 << 30},
	}
	if cfg.Quota != want {
		t.Errorf("want %+v, got %+v", want, cfg.Quota)
	}
}

func TestLoad_QuotaFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("quota:\n  org:\n    repos: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Quota.Org.Repos != 7 || cfg.Quota.User.Repos != 0 {
		t.Errorf("want org repos 7 and user repos 0, got %+v", cfg.Quota)
	}
}

func TestLoad_QuotaRejectsNegativeValues(t *testing.T) {
	for _, env := range []string{
		"CZ_QUOTA_USER_REPOS", "CZ_QUOTA_USER_STORAGE_BYTES", "CZ_QUOTA_ORG_REPOS", "CZ_QUOTA_ORG_STORAGE_BYTES",
	} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "-1")
			if _, err := config.Load(""); err == nil {
				t.Errorf("%s=-1: want a startup error", env)
			}
		})
	}
}

func TestLoad_WebhookDefaultsAndEnv(t *testing.T) {
	t.Setenv("CZ_IMPORT_ALLOW_LOCAL_NETWORKS", "true")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Webhook.AllowLocalNetworks {
		t.Error("defaults: webhook.allow_local_networks must stay false when only imports allow local networks")
	}

	t.Setenv("CZ_WEBHOOK_ALLOW_LOCAL_NETWORKS", "true")
	cfg, err = config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Webhook.AllowLocalNetworks {
		t.Error("env: want webhook.allow_local_networks=true")
	}
}

func TestLoad_StorageDefaultsAndEnv(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Backend != "local" || cfg.Storage.Local.Root != "./storage" || cfg.Storage.S3.Region != "us-east-1" {
		t.Errorf("defaults: got %+v", cfg.Storage)
	}

	t.Setenv("CZ_STORAGE_BACKEND", "s3")
	t.Setenv("CZ_STORAGE_S3_ENDPOINT", "http://localhost:7070")
	t.Setenv("CZ_STORAGE_S3_BUCKET", "avatars")
	t.Setenv("CZ_STORAGE_S3_PATH_STYLE", "true")
	t.Setenv("CZ_STORAGE_S3_PREFIX", "prod")
	cfg, err = config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s3 := cfg.Storage.S3
	if cfg.Storage.Backend != "s3" || s3.Endpoint != "http://localhost:7070" || s3.Bucket != "avatars" || !s3.PathStyle || s3.Prefix != "prod" {
		t.Errorf("env: got %+v", cfg.Storage)
	}
}

func TestLoad_SecretKey(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Security.SecretKey != "" {
		t.Errorf("default secret_key = %q, want empty", cfg.Security.SecretKey)
	}

	t.Setenv("CZ_SECURITY_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	if cfg, err = config.Load(""); err != nil || cfg.Security.SecretKey != "0123456789abcdef0123456789abcdef" {
		t.Errorf("env: got %v, %v", cfg, err)
	}

	t.Setenv("CZ_SECURITY_SECRET_KEY", "too-short")
	_, err = config.Load("")
	if err == nil || !strings.Contains(err.Error(), "security.secret_key") || strings.Contains(err.Error(), "too-short") {
		t.Errorf("short key: err = %v, want one naming security.secret_key without the value", err)
	}
}

func TestLoad_MirrorDefaultsAndEnv(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.MirrorConfig{Enabled: true, MinInterval: 10 * time.Minute, DefaultInterval: 8 * time.Hour, MaxConcurrent: 3, Timeout: 30 * time.Minute}
	if cfg.Mirror != want {
		t.Errorf("defaults = %+v, want %+v", cfg.Mirror, want)
	}

	t.Setenv("CZ_MIRROR_ENABLED", "false")
	t.Setenv("CZ_MIRROR_ALLOW_LOCAL_NETWORKS", "true")
	t.Setenv("CZ_MIRROR_MIN_INTERVAL", "1m")
	t.Setenv("CZ_MIRROR_DEFAULT_INTERVAL", "1h")
	t.Setenv("CZ_MIRROR_MAX_CONCURRENT", "1")
	t.Setenv("CZ_MIRROR_TIMEOUT", "5m")
	cfg, err = config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want = config.MirrorConfig{AllowLocalNetworks: true, MinInterval: time.Minute, DefaultInterval: time.Hour, MaxConcurrent: 1, Timeout: 5 * time.Minute}
	if cfg.Mirror != want {
		t.Errorf("env = %+v, want %+v", cfg.Mirror, want)
	}
}

func TestLoad_MirrorInvalid(t *testing.T) {
	for _, tc := range []struct{ env, value, wantKey string }{
		{"CZ_MIRROR_MIN_INTERVAL", "0s", "mirror.min_interval"},
		{"CZ_MIRROR_MIN_INTERVAL", "9h", "mirror.default_interval"},
		{"CZ_MIRROR_DEFAULT_INTERVAL", "800h", "mirror.default_interval"},
		{"CZ_MIRROR_MAX_CONCURRENT", "0", "mirror.max_concurrent"},
		{"CZ_MIRROR_TIMEOUT", "0s", "mirror.timeout"},
	} {
		t.Run(tc.env+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			_, err := config.Load("")
			if err == nil || !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("err = %v, want one naming %s", err, tc.wantKey)
			}
		})
	}
}
