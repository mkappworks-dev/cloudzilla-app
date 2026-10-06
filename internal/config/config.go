package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// DevJWTSecret is the loud placeholder used as the default jwt_secret in dev.
// main.go warns at startup if the loaded secret matches this value.
const DevJWTSecret = "dev-only-do-not-use-in-production-override-via-CZ_AUTH_JWT_SECRET"

// Config holds the full application configuration loaded from YAML and environment variables.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Git      GitConfig      `mapstructure:"git"`
	OAuth    OAuthConfig    `mapstructure:"oauth"`
	SMTP     SMTPConfig     `mapstructure:"smtp"`
	Import   ImportConfig   `mapstructure:"import"`
	Storage  StorageConfig  `mapstructure:"storage"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	Host         string        `mapstructure:"host"`
	BaseURL      string        `mapstructure:"base_url"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	// TrustedProxies lists the IPs or CIDRs of reverse proxies whose
	// X-Forwarded-For header is believed. Empty means clients connect directly.
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	DSN          string `mapstructure:"dsn"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// AuthConfig holds JWT and cookie authentication settings.
type AuthConfig struct {
	JWTSecret    string        `mapstructure:"jwt_secret"`
	JWTExpiry    time.Duration `mapstructure:"jwt_expiry"`
	CookieName   string        `mapstructure:"cookie_name"`
	CookieSecure bool          `mapstructure:"cookie_secure"`
}

// GitConfig holds git repository storage and SSH server settings.
type GitConfig struct {
	ReposRoot  string `mapstructure:"repos_root"`
	SSHPort    int    `mapstructure:"ssh_port"`
	SSHHostKey string `mapstructure:"ssh_host_key"`
	// MaxPackBytes caps the post-decompression size of a pushed pack on
	// both transports. Zero disables the cap.
	MaxPackBytes int64 `mapstructure:"max_pack_bytes"`
	// SSHMaxSession is the absolute lifetime of an SSH connection — a
	// backstop against slow-trickle connections that defeat the idle
	// timeout. Zero disables it.
	SSHMaxSession time.Duration `mapstructure:"ssh_max_session"`
}

// ImportConfig holds repository import settings.
type ImportConfig struct {
	// Off by default, so a user can't make the server probe its own network.
	AllowLocalNetworks bool          `mapstructure:"allow_local_networks"`
	Timeout            time.Duration `mapstructure:"timeout"`
}

// StorageConfig selects where uploaded objects, such as avatars, are kept.
type StorageConfig struct {
	Backend string             `mapstructure:"backend"`
	Local   LocalStorageConfig `mapstructure:"local"`
	S3      S3StorageConfig    `mapstructure:"s3"`
}

type LocalStorageConfig struct {
	Root string `mapstructure:"root"`
}

// S3StorageConfig works for AWS and S3-compatible servers. Without both keys
// the AWS SDK's default credential chain applies.
type S3StorageConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	Region          string `mapstructure:"region"`
	Bucket          string `mapstructure:"bucket"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	PathStyle       bool   `mapstructure:"path_style"`
	Prefix          string `mapstructure:"prefix"`
}

// OAuthConfig holds Google OAuth provider settings.
type OAuthConfig struct {
	GoogleClientID     string `mapstructure:"google_client_id"`
	GoogleClientSecret string `mapstructure:"google_client_secret"`
	GoogleRedirectURL  string `mapstructure:"google_redirect_url"`
}

// SMTPConfig holds outgoing SMTP email settings.
type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
	TLS      bool   `mapstructure:"tls"`
}

// Load reads config from cfgFile (or config.yaml in the current directory) and applies CZ_ env overrides.
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.base_url", "http://localhost:8080")
	v.SetDefault("server.read_timeout", "15s")
	v.SetDefault("server.write_timeout", "15s")
	v.SetDefault("server.trusted_proxies", []string{})
	v.SetDefault("database.dsn", "postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla?sslmode=disable")
	v.SetDefault("database.max_open_conns", 10)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("auth.jwt_secret", DevJWTSecret)
	v.SetDefault("auth.jwt_expiry", "24h")
	v.SetDefault("auth.cookie_name", "cz_token")
	v.SetDefault("auth.cookie_secure", false)
	v.SetDefault("git.repos_root", "./git-repos")
	v.SetDefault("git.ssh_port", 2222)
	v.SetDefault("git.ssh_host_key", "./cloudzilla_host_key")
	v.SetDefault("git.max_pack_bytes", int64(2)<<30)
	v.SetDefault("git.ssh_max_session", "2h")
	v.SetDefault("oauth.google_client_id", "")
	v.SetDefault("oauth.google_client_secret", "")
	v.SetDefault("oauth.google_redirect_url", "http://localhost:8080/auth/google/callback")
	v.SetDefault("smtp.host", "")
	v.SetDefault("smtp.port", 587)
	v.SetDefault("smtp.username", "")
	v.SetDefault("smtp.password", "")
	v.SetDefault("smtp.from", "noreply@localhost")
	v.SetDefault("smtp.tls", false)
	v.SetDefault("import.allow_local_networks", false)
	v.SetDefault("import.timeout", "30m")
	v.SetDefault("storage.backend", "local")
	v.SetDefault("storage.local.root", "./storage")
	v.SetDefault("storage.s3.endpoint", "")
	v.SetDefault("storage.s3.region", "us-east-1")
	v.SetDefault("storage.s3.bucket", "")
	v.SetDefault("storage.s3.access_key_id", "")
	v.SetDefault("storage.s3.secret_access_key", "")
	v.SetDefault("storage.s3.path_style", false)
	v.SetDefault("storage.s3.prefix", "")

	// Env overrides
	v.SetEnvPrefix("CZ")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
