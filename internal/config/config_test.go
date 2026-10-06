package config_test

import (
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
