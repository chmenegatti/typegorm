// cmd/typegorm/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	// viper import removed if not directly used here, Cobra handles flags
)

var (
	cfgFile string // Persistent flag for the config file path

	rootCmd = &cobra.Command{
		Use:   "typegorm",
		Short: "TypeGORM CLI for database management and migrations",
		Long: `The TypeGORM CLI is a tool to assist development
with the TypeGORM ORM, including features like
database migration management.`,
	}
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error executing command: '%s'\n", err) // English error message
		os.Exit(1)
	}
}

func init() {
	// Add persistent --config flag, available to all subcommands
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Configuration file (default is ./typegorm.yaml or $HOME/.typegorm/typegorm.yaml)")
	// TODO: Add other global flags if needed (e.g., --verbose)
}

func main() {
	Execute()
}
