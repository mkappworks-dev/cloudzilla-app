package main

import (
	"fmt"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/spf13/cobra"
)

var cfgFile string

// Must stay a var: release builds set it with -ldflags "-X main.version=<tag>".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "cloudzilla",
	Short:   "Cloudzilla admin CLI",
	Version: version,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: config.yaml)")

	rootCmd.AddCommand(migrateCmd())
	rootCmd.AddCommand(gcCmd())
	rootCmd.AddCommand(statsCmd())
	rootCmd.AddCommand(seedCmd())
}

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}
			database, err := db.Connect(cfg.Database)
			if err != nil {
				return err
			}
			defer database.Close()
			return db.Migrate(database)
		},
	}
}
