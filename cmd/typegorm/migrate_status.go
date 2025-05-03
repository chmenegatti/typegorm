// cmd/typegorm/migrate_status.go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
	// Import the migration package
	"github.com/chmenegatti/typegorm/pkg/migration"
)

var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of all migrations",
	Long:  `Displays which migrations have been applied and which are pending based on files in the migration directory and records in the database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Executing 'migrate status' command...")

		// Call the RunStatus function, passing the loaded config
		err := migration.RunStatus(cfg)
		if err != nil {
			return fmt.Errorf("migration status command failed: %w", err)
		}
		// Output is handled within RunStatus in this example
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateStatusCmd)
}
