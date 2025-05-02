// cmd/typegorm/migrate.go
package main

import (
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Manage database migrations",
	Long:  `Allows creating, applying (up), reverting (down), and checking the status of migrations.`,
}

func init() {
	rootCmd.AddCommand(migrateCmd) // Add 'migrate' as a subcommand of the root
}
