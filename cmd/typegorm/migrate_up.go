// cmd/typegorm/migrate_up.go
package main

import (
	"fmt"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/migration"
	"github.com/spf13/cobra"
)

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	Long:  `Executes the 'Up' function for all migrations that have not yet been applied to the database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load configuration using the --config flag (if provided) or defaults
		cfg, err := config.LoadConfig(cfgFile) // cfgFile is the global variable from main.go
		if err != nil {
			return fmt.Errorf("error loading configuration: %w", err)
		}

		// Call the migration logic (placeholder for now)
		fmt.Println("Running migrate up...") // Temporary log
		err = migration.RunUp(cfg)           // Placeholder call
		if err != nil {
			return fmt.Errorf("failed to apply migrations: %w", err)
		}

		fmt.Println("Migrations applied successfully (placeholder).")
		return nil // Return nil on success
	},
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd) // Add 'up' as a subcommand of 'migrate'
	// Add specific flags for 'up' if needed later (e.g., --steps N)
}
