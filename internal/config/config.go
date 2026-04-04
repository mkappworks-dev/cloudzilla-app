package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Git      GitConfig      `mapstructure:"git"`
	OAuth    OAuthConfig    `mapstructure:"oauth"`
	SMTP     SMTPConfig     `mapstructure:"smtp"`
}

type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	Host         string        `mapstructure:"host"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type DatabaseConfig struct {
	Driver       string `mapstructure:"driver"`
	DSN          string `mapstructure:"dsn"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

type AuthConfig struct {
	JWTSecret  string        `mapstructure:"jwt_secret"`
	JWTExpiry  time.Duration `mapstructure:"jwt_expiry"`
	CookieName string        `mapstructure:"cookie_name"`
}

type GitConfig struct {
	ReposRoot  string `mapstructure:"repos_root"`
	SSHPort    int    `mapstructure:"ssh_port"`
	SSHHostKey string `mapstructure:"ssh_host_key"`
}

type OAuthConfig struct {
	GoogleClientID     string `mapstructure:"google_client_id"`
	GoogleClientSecret string `mapstructure:"google_client_secret"`
	GoogleRedirectURL  string `mapstructure:"google_redirect_url"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
	TLS      bool   `mapstructure:"tls"`
}

func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.read_timeout", "15s")
	v.SetDefault("server.write_timeout", "15s")
	v.SetDefault("database.driver", "postgres")
	v.SetDefault("database.dsn", "postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla?sslmode=disable")
	v.SetDefault("database.max_open_conns", 10)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("auth.jwt_secret", "change-me")
	v.SetDefault("auth.jwt_expiry", "24h")
	v.SetDefault("auth.cookie_name", "cz_token")
	v.SetDefault("git.repos_root", "./git-repos")
	v.SetDefault("git.ssh_port", 2222)
	v.SetDefault("git.ssh_host_key", "./cloudzilla_host_key")
	v.SetDefault("oauth.google_client_id", "")
	v.SetDefault("oauth.google_client_secret", "")
	v.SetDefault("oauth.google_redirect_url", "http://localhost:8080/auth/google/callback")
	v.SetDefault("smtp.host", "")
	v.SetDefault("smtp.port", 587)
	v.SetDefault("smtp.username", "")
	v.SetDefault("smtp.password", "")
	v.SetDefault("smtp.from", "noreply@localhost")
	v.SetDefault("smtp.tls", false)

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
