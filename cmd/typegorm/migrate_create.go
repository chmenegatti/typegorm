// cmd/typegorm/migrate_create.go
package main

import (
	"fmt"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/migration"
	"github.com/spf13/cobra"
)

var migrateCreateCmd = &cobra.Command{
	Use:   "create <migration_name>",
	Short: "Create a new migration file",
	Long:  `Creates a new migration file with the current timestamp and the provided name.`,
	Args:  cobra.ExactArgs(1), // Expect exactly one argument: the migration name
	RunE: func(cmd *cobra.Command, args []string) error {
		migrationName := args[0]

		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("error loading configuration: %w", err)
		}

		fmt.Printf("Running migrate create for '%s'...\n", migrationName)
		err = migration.RunCreate(cfg, migrationName) // Placeholder call
		if err != nil {
			return fmt.Errorf("failed to create migration file: %w", err)
		}

		// Message printed by RunCreate placeholder
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateCreateCmd)
}
