// cmd/typegorm/migrate_create.go
package main

import (
	"fmt"
	"strings" // Import strings

	"github.com/chmenegatti/typegorm/pkg/migration" // Use correct import path
	"github.com/spf13/cobra"
)

var migrationType string // Variable to hold the --type flag value

var migrateCreateCmd = &cobra.Command{
	Use:   "create <migration_name>",
	Short: "Create a new migration file (.sql or .go)",
	Long: `Creates a new migration file with the current timestamp and the provided name.
Use the --type flag to specify 'sql' (default) or 'go'.`,
	Args: cobra.ExactArgs(1), // Expect exactly one argument: the migration name
	RunE: func(cmd *cobra.Command, args []string) error {
		migrationName := args[0]
		migrationType = strings.ToLower(migrationType) // Normalize type

		if migrationType != "sql" && migrationType != "go" {
			return fmt.Errorf("invalid migration type '%s', must be 'sql' or 'go'", migrationType)
		}

		// cfg is loaded by rootCmd's PersistentPreRunE
		fmt.Printf("Running migrate create for '%s' (type: %s)...\n", migrationName, migrationType)

		// Pass the type to RunCreate
		err := migration.RunCreate(cfg, migrationName, migrationType)
		if err != nil {
			return fmt.Errorf("failed to create migration file: %w", err)
		}

		// Success message printed by RunCreate
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateCreateCmd)
	// Add the --type flag
	migrateCreateCmd.Flags().StringVarP(&migrationType, "type", "t", "sql", "Type of migration file to create ('sql' or 'go')")
}
