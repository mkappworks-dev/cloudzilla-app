package config_test

import (
	"slices"
	"testing"

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
