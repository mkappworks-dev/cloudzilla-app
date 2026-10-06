package config_test

import (
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
