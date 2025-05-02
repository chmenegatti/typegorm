// cmd/typegorm/migrate_status.go
package main

import (
	"fmt"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/migration"
	"github.com/spf13/cobra"
)

var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of all migrations",
	Long:  `Compares the available migration files with the applied migrations recorded in the database and shows the status of each (applied or pending).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("error loading configuration: %w", err)
		}

		fmt.Println("Running migrate status...")
		err = migration.RunStatus(cfg) // Placeholder call
		if err != nil {
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		// Status details would be printed by RunStatus itself
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateStatusCmd)
}
