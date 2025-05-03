// cmd/typegorm/migrate_create.go
package main

import (
	"fmt"
	// Import only necessary packages
	"github.com/spf13/cobra"
	// Import the migration package
	"github.com/chmenegatti/typegorm/pkg/migration"
)

var migrateCreateCmd = &cobra.Command{
	Use:   "create <migration_name>",
	Short: "Create a new SQL migration file",
	Long: `Creates a new timestamped SQL migration file in the configured migration directory.
The name should be descriptive, e.g., "AddUserTable" or "CreateProductsIndex".
Example: typegorm migrate create AddUserTable`,
	Args: cobra.ExactArgs(1), // Requires exactly one argument: the migration name
	RunE: func(cmd *cobra.Command, args []string) error {
		migrationName := args[0]
		fmt.Printf("Executing 'migrate create' for name: %s\n", migrationName)

		// Call the RunCreate function, passing the loaded config and the migration name from args
		err := migration.RunCreate(cfg, migrationName)
		if err != nil {
			return fmt.Errorf("migration create command failed: %w", err)
		}
		// Success message is handled within RunCreate
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateCreateCmd)
	// Flags specific to 'migrate create', if any (e.g., --type=go in the future)
}
