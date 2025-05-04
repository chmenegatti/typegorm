// // cmd/typegorm/migrate_down.go
package main

// import (
// 	"fmt"

// 	"github.com/spf13/cobra"
// 	// Import the migration package
// 	"github.com/chmenegatti/typegorm/pkg/migration"
// )

// var (
// 	// Flag variable to store the number of steps from --steps flag
// 	downSteps int
// )

// var migrateDownCmd = &cobra.Command{
// 	Use:   "down",
// 	Short: "Revert the last applied migration or a specific number of steps",
// 	Long:  `Reverts migrations that have already been applied. By default, it reverts the last applied migration. Use --steps N to revert N migrations.`,
// 	RunE: func(cmd *cobra.Command, args []string) error {
// 		fmt.Println("Executing 'migrate down' command...")

// 		// Call the RunDown function, passing the loaded config and the steps flag value
// 		err := migration.RunDown(cfg, downSteps)
// 		if err != nil {
// 			return fmt.Errorf("migration down command failed: %w", err)
// 		}
// 		// Success message is handled within RunDown in this example
// 		return nil
// 	},
// }

// func init() {
// 	migrateCmd.AddCommand(migrateDownCmd)
// 	// Define the --steps flag
// 	migrateDownCmd.Flags().IntVarP(&downSteps, "steps", "s", 1, "Number of migrations to revert (default: 1)")
// }
