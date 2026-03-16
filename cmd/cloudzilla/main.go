package main

import (
	"fmt"
	"os"

	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/db"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "cloudzilla",
	Short: "Cloudzilla admin CLI",
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
	rootCmd.AddCommand(createUserCmd())
	rootCmd.AddCommand(createRepoCmd())
}

func loadDeps() (*service.Services, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	database, err := db.Connect(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	stores := store.New(database)
	return service.New(stores, cfg), nil
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

func createUserCmd() *cobra.Command {
	var username, email, password string
	cmd := &cobra.Command{
		Use:   "create-user",
		Short: "Create a new user",
		RunE: func(cmd *cobra.Command, args []string) error {
			svcs, err := loadDeps()
			if err != nil {
				return err
			}
			user, err := svcs.User.Create(cmd.Context(), username, email, password)
			if err != nil {
				return err
			}
			fmt.Printf("Created user: %s (id=%d)\n", user.Username, user.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "Username (required)")
	cmd.Flags().StringVar(&email, "email", "", "Email (required)")
	cmd.Flags().StringVar(&password, "password", "", "Password (required)")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("password")
	return cmd
}

func createRepoCmd() *cobra.Command {
	var owner, name, description string
	cmd := &cobra.Command{
		Use:   "create-repo",
		Short: "Create a new repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			svcs, err := loadDeps()
			if err != nil {
				return err
			}
			repo, err := svcs.Repo.Create(cmd.Context(), owner, name, description, false)
			if err != nil {
				return err
			}
			fmt.Printf("Created repo: %s/%s (id=%d)\n", owner, repo.Name, repo.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "Owner username (required)")
	cmd.Flags().StringVar(&name, "name", "", "Repo name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	_ = cmd.MarkFlagRequired("owner")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
