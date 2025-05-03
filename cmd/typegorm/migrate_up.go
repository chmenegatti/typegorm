// cmd/typegorm/migrate_up.go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
	// Import the migration package
	"github.com/chmenegatti/typegorm/pkg/migration"
)

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	Long:  `Applies all migrations that have not yet been run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Executing 'migrate up' command...")

		// Call the RunUp function from the migration package, passing the loaded config
		err := migration.RunUp(cfg)
		if err != nil {
			// Return the error directly; Cobra will print it. Add context for clarity.
			return fmt.Errorf("migration up command failed: %w", err)
		}
		// Success message is handled within RunUp in this example
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd)
}
