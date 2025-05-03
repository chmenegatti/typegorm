// pkg/migration/runner.go
package migration

import (
	"errors"
	"fmt"
	"os"            // Import os
	"path/filepath" // Import filepath
	"strings"       // Import strings
	"time"

	"github.com/chmenegatti/typegorm/pkg/config"
)

// RunCreate creates a new migration file.
func RunCreate(cfg config.Config, name string) error {
	fmt.Printf("Creating migration file for: %s\n", name)

	migrationsDir := cfg.Migration.Directory
	fmt.Printf("   Using migration directory: %s\n", migrationsDir)

	// 1. Validate the name (basic validation)
	if name == "" {
		return errors.New("migration name cannot be empty")
	}
	// Example sanitization: lowercase, replace spaces with underscores
	safeName := strings.ReplaceAll(strings.ToLower(name), " ", "_")

	// 2. Generate timestamp (YYYYMMDDHHMMSS format, UTC).
	timestamp := time.Now().UTC().Format("20060102150405")

	// 3. Create the filename (e.g., "migrations/YYYYMMDDHHMMSS_add_user_table.sql").
	//    TODO: Adapt extension based on config or future flag (e.g., .go)
	filename := fmt.Sprintf("%s_%s.sql", timestamp, safeName)
	filePath := filepath.Join(migrationsDir, filename)

	// 4. Ensure the migrations directory exists.
	// Use os.ModePerm (0777) for simplicity, adjust if more restrictive permissions needed.
	if err := os.MkdirAll(migrationsDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create migration directory '%s': %w", migrationsDir, err)
	}

	// 5. Check if file already exists (highly unlikely with timestamp, but good practice)
	if _, err := os.Stat(filePath); err == nil {
		return fmt.Errorf("migration file '%s' already exists", filePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		// Handle unexpected errors from os.Stat (e.g., permission issues)
		return fmt.Errorf("failed checking for existing file '%s': %w", filePath, err)
	}

	// 6. Write a basic template to the file.
	//    TODO: Make template language-specific if supporting .go migrations later.
	content := []byte(fmt.Sprintf("-- Migration: %s\n-- Created at: %s UTC\n\n-- +migrate Up\n-- SQL in this section is executed when migrating Up.\n\n\n\n-- +migrate Down\n-- SQL in this section is executed when migrating Down.\n\n",
		name, // Use original name in comment for clarity
		time.Now().UTC().Format(time.RFC3339),
	))

	// Use 0644 permissions for the created file (read/write for owner, read for group/others).
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		return fmt.Errorf("failed to create migration file '%s': %w", filePath, err)
	}

	fmt.Printf("   Successfully created migration file: %s\n", filePath)
	return nil
}

// RunUp applies pending migrations.
func RunUp(cfg config.Config) error {
	fmt.Printf("Placeholder: Applying 'Up' migrations\n")
	fmt.Printf("   Using Dialect: %s\n", cfg.Database.Dialect)
	fmt.Printf("   Using DSN: %s\n", cfg.Database.DSN) // Be careful logging DSNs
	fmt.Printf("   Migrations Directory: %s\n", cfg.Migration.Directory)
	fmt.Printf("   Migrations Table: %s\n", cfg.Migration.TableName)
	// Actual logic would involve:
	// 1. Get dialect implementation.
	// 2. Connect to the DB.
	// 3. Ensure the migration control table exists.
	// 4. Read migration files (*.sql, *.go) from cfg.Migration.Directory.
	// 5. Query the control table for applied migrations.
	// 6. Determine pending migrations.
	// 7. Execute the `Up` logic of pending migrations in order, within a transaction.
	// 8. Record each applied migration in the control table.
	fmt.Println("   'Up' migrations applied successfully (placeholder).")
	return nil
}

// RunDown reverts applied migrations.
func RunDown(cfg config.Config, steps int) error {
	fmt.Printf("Placeholder: Reverting 'Down' migrations\n")
	fmt.Printf("   Steps to revert: %d\n", steps)
	fmt.Printf("   Using Dialect: %s\n", cfg.Database.Dialect)
	fmt.Printf("   Using DSN: %s\n", cfg.Database.DSN) // Be careful logging DSNs
	fmt.Printf("   Migrations Directory: %s\n", cfg.Migration.Directory)
	fmt.Printf("   Migrations Table: %s\n", cfg.Migration.TableName)
	// Actual logic:
	// 1. Get dialect, Connect.
	// 2. Query control table for the last 'steps' applied migrations.
	// 3. Read corresponding migration files.
	// 4. Execute the 'Down' logic for these migrations in reverse order, within a transaction.
	// 5. Remove entries from the control table.
	fmt.Println("   'Down' migrations reverted successfully (placeholder).")
	return nil
}

// RunStatus checks the status of migrations.
func RunStatus(cfg config.Config) error {
	fmt.Printf("Placeholder: Checking migration status\n")
	fmt.Printf("   Using Dialect: %s\n", cfg.Database.Dialect)
	fmt.Printf("   Using DSN: %s\n", cfg.Database.DSN) // Be careful logging DSNs
	fmt.Printf("   Migrations Directory: %s\n", cfg.Migration.Directory)
	fmt.Printf("   Migrations Table: %s\n", cfg.Migration.TableName)
	// Actual logic:
	// 1. Get dialect, Connect.
	// 2. Read all migration files from directory.
	// 3. Query control table for all applied migrations.
	// 4. Compare the two lists and print status (applied/pending) for each file.
	fmt.Println("   Migration status checked successfully (placeholder).")
	return nil
}
