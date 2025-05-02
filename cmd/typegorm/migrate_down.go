// cmd/typegorm/migrate_down.go
package main

import (
	"fmt"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/migration"
	"github.com/spf13/cobra"
)

var downSteps int // Variable to hold the value of the --steps flag

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Revert the last applied migration(s)",
	Long:  `Executes the 'Down' function for the specified number of last applied migrations. Defaults to reverting one migration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("error loading configuration: %w", err)
		}

		fmt.Printf("Running migrate down (steps: %d)...\n", downSteps)
		err = migration.RunDown(cfg, downSteps) // Placeholder call
		if err != nil {
			return fmt.Errorf("failed to revert migrations: %w", err)
		}

		fmt.Println("Migrations reverted successfully (placeholder).")
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateDownCmd)
	// Add the --steps flag, defaulting to 1
	migrateDownCmd.Flags().IntVarP(&downSteps, "steps", "s", 1, "Number of migrations to revert")
}
