// cmd/typegorm/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	// Import the config package we created
	"github.com/chmenegatti/typegorm/pkg/config" // Adjust the import path as necessary
)

var (
	// cfgFile will store the configuration file path provided via the --config flag
	cfgFile string

	// cfg will hold the loaded and validated configuration.
	// Making it accessible to other files within the 'main' package (cmd/typegorm).
	cfg config.Config
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "typegorm",
	Short: "A database migration tool inspired by TypeORM",
	Long: `TypeGORM is a CLI tool for managing database schema migrations,
following principles similar to TypeORM's migrations.`,

	// PersistentPreRunE runs *before* the Run/RunE function of any subcommand.
	// It's the ideal place to load configuration or initialize connections.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Informative log (can be adjusted or made conditional later)
		// fmt.Printf("Attempting to load configuration using path: %q\n", cfgFile)

		// Call the LoadConfig function we created.
		// Pass the cfgFile flag value. If it's an empty string, LoadConfig
		// will try to find the default files (typegorm.yaml, etc.).
		loadedCfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			// If LoadConfig returns an error (file not found AND specified,
			// parsing error, or validation error), return the error.
			// Cobra will handle printing the error to the user and exiting.
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		// If loading succeeds, store the loaded configuration
		// in the package-level 'cfg' variable for subcommands to use.
		cfg = loadedCfg

		// Informative log (optional)
		// fmt.Println("Configuration loaded successfully.")
		// fmt.Printf("  -> DSN from config: %s\n", cfg.Database.DSN) // Example, be careful with sensitive data

		return nil // Return nil to indicate successful preparation.
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once for the rootCmd.
func Execute() {
	// Error handling is simplified. If rootCmd.Execute() returns an error
	// (either from PersistentPreRunE or a subcommand's RunE), Cobra will print it,
	// and os.Exit(1) below ensures the correct exit code.
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Add the persistent --config flag to the root command.
	// Persistent flags are available to the root command and all its subcommands.
	// - First argument is a pointer to the variable storing the flag's value (cfgFile).
	// - Second is the long flag name ("config").
	// - Third is the short flag name ("c").
	// - Fourth is the default value ("" - empty string, causing LoadConfig to check defaults).
	// - Fifth is the help description.
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is typegorm.yaml in ., $HOME/.typegorm, /etc/typegorm/)")

	// Add the 'migrate' command (defined in migrate.go) as a subcommand of rootCmd.
	rootCmd.AddCommand(migrateCmd)
	// Add other top-level commands here, if any.
}

// The main function remains simple, just calling Execute.
func main() {
	Execute()
}
