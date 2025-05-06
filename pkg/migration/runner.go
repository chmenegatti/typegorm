// pkg/migration/runner.go
package migration

import (
	"bufio"
	"context" // Need sql for TxOptions, maybe move to common later?
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/dialects"        // Import dialects package
	"github.com/chmenegatti/typegorm/pkg/dialects/common" // Import common interfaces
)

// --- Helper Function: Get DataSource ---

// getDataSource retrieves the appropriate DataSource based on config, connects it, and returns it.
// It's the caller's responsibility to call Close() on the returned DataSource.
func getDataSource(cfg config.DatabaseConfig) (common.DataSource, error) {
	factory := dialects.Get(cfg.Dialect)
	if factory == nil {
		// This case should technically be prevented by the 'found' check above, but let's be safe.
		return nil, fmt.Errorf("internal error: found factory is nil for dialect %s", cfg.Dialect)
	}

	ds := factory() // Create a new DataSource instance
	if ds == nil {
		return nil, fmt.Errorf("internal error: factory for dialect %s returned a nil DataSource instance", cfg.Dialect)
	}

	fmt.Printf("Attempting to connect to %s database...\n", ds.Dialect().Name())
	err := ds.Connect(cfg) // Connect using the provided config
	if err != nil {
		return nil, fmt.Errorf("failed to connect data source: %w", err)
	}

	// Optional: Ping to be absolutely sure connection is live after Connect
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ds.Ping(ctx); err != nil {
		ds.Close() // Attempt to clean up if ping fails
		return nil, fmt.Errorf("failed to ping database after connect: %w", err)
	}

	fmt.Printf("Successfully established database connection.\n")
	return ds, nil
}

// --- Helper Function: Ensure Schema Migrations Table ---

// ensureMigrationsTable checks if the schema migrations table exists and creates it if not.
func ensureMigrationsTable(ctx context.Context, ds common.DataSource, tableName string) error {
	dialect := ds.Dialect()
	createTableSQL := dialect.CreateSchemaMigrationsTableSQL(tableName)

	fmt.Printf("Ensuring migration history table '%s' exists...\n", tableName)
	// We don't necessarily need a transaction for a CREATE TABLE IF NOT EXISTS
	_, err := ds.Exec(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to ensure migration history table '%s': %w", tableName, err)
	}
	fmt.Printf("Migration history table '%s' is ready.\n", tableName)
	return nil
}

// --- Helper Function: Find Migration Files ---

// migrationFile represents a migration file found on disk.
type migrationFile struct {
	ID   string // Extracted ID (e.g., timestamp or sequence part of the name)
	Path string // Full path to the file
	Name string // Filename
}

// findMigrationFiles scans the directory for valid migration files (e.g., *.sql)
// and returns them sorted by ID.
func findMigrationFiles(dir string) ([]migrationFile, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		// Provide a clearer error message if the directory doesn't exist
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("migration directory '%s' not found", dir)
		}
		return nil, fmt.Errorf("failed to read migration directory '%s': %w", dir, err)
	}

	var migrations []migrationFile
	fmt.Printf("Scanning directory '%s' for migration files...\n", dir)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") { // Simple check for .sql files
			continue
		}

		// Extract ID from filename (e.g., "20230101120000_create_users.sql" -> "20230101120000")
		// This assumes a convention like TIMESTAMP_description.sql or SEQUENCE_description.sql
		parts := strings.SplitN(file.Name(), "_", 2)
		if len(parts) < 1 { // Needs at least the ID part
			fmt.Printf("Skipping file with unexpected name format: %s\n", file.Name())
			continue
		}
		id := parts[0]
		// Basic validation: Ensure ID is not empty (could add more checks, e.g., digits only)
		if id == "" {
			fmt.Printf("Skipping file with empty ID part: %s\n", file.Name())
			continue
		}

		migrations = append(migrations, migrationFile{
			ID:   id,
			Path: filepath.Join(dir, file.Name()),
			Name: file.Name(),
		})
		fmt.Printf("  Found: %s (ID: %s)\n", file.Name(), id)
	}

	// Sort migrations by ID to process them in order
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].ID < migrations[j].ID
	})

	fmt.Printf("Found %d migration files, sorted by ID.\n", len(migrations))
	return migrations, nil
}

// --- Helper Function: Get Applied Migrations ---

func getAppliedMigrationsOrdered(ctx context.Context, ds common.DataSource, tableName string, order string) ([]common.MigrationRecord, error) {
	dialect := ds.Dialect()
	query := dialect.GetAppliedMigrationsSQL(tableName)
	// Adjust query slightly if specific ordering is needed and not default
	if strings.ToUpper(order) == "DESC" {
		query = strings.Replace(query, "ASC", "DESC", 1) // Simple replacement
	}
	// fmt.Printf("Querying database for applied migrations from '%s' (Order: %s)...\n", tableName, order) // Reduce noise
	rows, err := ds.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()
	var applied []common.MigrationRecord
	for rows.Next() {
		var record common.MigrationRecord
		if err := rows.Scan(&record.ID, &record.AppliedAt); err != nil {
			return nil, fmt.Errorf("failed to scan applied migration record: %w", err)
		}
		applied = append(applied, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating applied migration rows: %w", err)
	}
	// fmt.Printf("Found %d applied migrations in the database.\n", len(applied)) // Reduce noise
	return applied, nil
}

const (
	markerUp   = "-- +migrate Up"
	markerDown = "-- +migrate Down"
)

// parseSQLMigration extracts the 'Up' and 'Down' SQL statements from a reader.
// Returns: upSQL string, downSQL string, error
func parseSQLMigration(r io.Reader) (string, string, error) {
	var upSQL, downSQL strings.Builder
	var currentBuffer *strings.Builder // Points to either upSQL or downSQL

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		if strings.HasPrefix(trimmedLine, markerUp) {
			currentBuffer = &upSQL
			continue // Skip the marker line itself
		}
		if strings.HasPrefix(trimmedLine, markerDown) {
			currentBuffer = &downSQL
			continue // Skip the marker line itself
		}

		// Ignore empty lines and simple SQL comments unless inside a section
		if currentBuffer != nil && trimmedLine != "" && !strings.HasPrefix(trimmedLine, "--") {
			// Write the line, preserving original whitespace within the line
			// Add a newline character manually, as scanner removes it.
			// Add a space for safety, some DBs require space before semicolon etc.
			if _, err := currentBuffer.WriteString(line + "\n"); err != nil {
				return "", "", fmt.Errorf("failed writing to SQL buffer: %w", err) // Should not happen with strings.Builder
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("error reading migration file: %w", err)
	}

	// Basic check: Ensure Up marker was found if content exists
	if upSQL.Len() == 0 && (downSQL.Len() > 0 || currentBuffer != nil) {
		// Allow empty Up if the file only contained Down or was empty after marker
	}

	return upSQL.String(), downSQL.String(), nil
}

// --- Runner Function Implementation ---

// RunCreate creates a new migration file.
// (Keep existing implementation - may need minor adjustments later)
func RunCreate(cfg config.Config, name string) error {
	fmt.Println("Running Create Migration...")
	fmt.Printf("  Name: %s\n", name)
	fmt.Printf("  Directory: %s\n", cfg.Migration.Directory)

	if name == "" {
		return fmt.Errorf("migration name cannot be empty")
	}

	// Simple timestamp prefix
	timestamp := time.Now().UTC().Format("20060102150405")
	// Basic sanitization of name (replace spaces, convert to lower)
	safeName := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	filename := fmt.Sprintf("%s_%s.sql", timestamp, safeName)
	filepath := filepath.Join(cfg.Migration.Directory, filename)

	// Ensure directory exists
	if err := os.MkdirAll(cfg.Migration.Directory, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create migration directory '%s': %w", cfg.Migration.Directory, err)
	}

	// Check if file already exists
	if _, err := os.Stat(filepath); !os.IsNotExist(err) {
		return fmt.Errorf("migration file already exists: %s", filepath)
	}

	// Create basic SQL file content
	content := fmt.Sprintf("-- Migration: %s\n-- Created at: %s UTC\n\n%s\n\n\n\n%s\n\n", name, time.Now().UTC().Format(time.RFC3339), markerUp, markerDown)

	err := os.WriteFile(filepath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write migration file '%s': %w", filepath, err)
	}

	fmt.Printf("Successfully created migration file: %s\n", filepath)
	return nil
}

// RunStatus checks the status of migrations.
func RunStatus(cfg config.Config) error {
	fmt.Println("Running Migration Status...")
	ctx := context.Background() // Use a background context for now

	// 1. Get and connect DataSource
	ds, err := getDataSource(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to initialize data source: %w", err)
	}
	defer ds.Close() // Ensure connection is closed

	// 2. Ensure migration table exists
	migrationTable := cfg.Migration.TableName
	if migrationTable == "" {
		return fmt.Errorf("migration table name is not configured")
	}
	if err := ensureMigrationsTable(ctx, ds, migrationTable); err != nil {
		return err // Error already includes context
	}

	// 3. Find migration files on disk
	diskMigrations, err := findMigrationFiles(cfg.Migration.Directory)
	if err != nil {
		return err // Error already includes context
	}

	// 4. Get applied migrations from DB
	appliedMigrationsList, err := getAppliedMigrationsOrdered(ctx, ds, migrationTable, "ASC")
	if err != nil {
		return err
	}
	dbMigrationsMap := make(map[string]time.Time, len(appliedMigrationsList))
	for _, rec := range appliedMigrationsList {
		dbMigrationsMap[rec.ID] = rec.AppliedAt
	}

	// 5. Compare and Report Status
	fmt.Println("\nMigration Status Report:")
	fmt.Println("------------------------")
	foundPending := false
	if len(diskMigrations) == 0 {
		fmt.Println("No migration files found.")
		if len(dbMigrationsMap) > 0 {
			fmt.Printf("WARNING: %d migrations found in database table '%s' but no files found in directory '%s'.\n",
				len(dbMigrationsMap), migrationTable, cfg.Migration.Directory)
		}
		return nil
	}

	fmt.Printf("%-17s %-40s %s\n", "Status", "Migration ID", "Filename")
	fmt.Printf("%-17s %-40s %s\n", "------", "--------------", "--------")

	for _, mf := range diskMigrations {
		if appliedAt, ok := dbMigrationsMap[mf.ID]; ok {
			// Applied
			fmt.Printf("[✓] Applied       %-40s %s (at %s)\n", mf.ID, mf.Name, appliedAt.Local().Format(time.RFC1123))
		} else {
			// Pending
			fmt.Printf("[ ] Pending       %-40s %s\n", mf.ID, mf.Name)
			foundPending = true
		}
		// Remove from dbMigrations map to track orphaned DB entries later (optional)
		delete(dbMigrationsMap, mf.ID)
	}

	// Check for migrations recorded in DB but not found on disk (optional, but good practice)
	if len(dbMigrationsMap) > 0 {
		fmt.Println("\nWARNING: The following migrations are recorded in the database but their files were not found:")
		for id, appliedAt := range dbMigrationsMap {
			fmt.Printf("  - %s (Applied at: %s)\n", id, appliedAt.Local().Format(time.RFC1123))
		}
	}

	fmt.Println("------------------------")
	if !foundPending && len(dbMigrationsMap) == 0 { // Only print "Up to date" if no pending AND no orphans
		fmt.Println("Database schema is up to date.")
	} else if !foundPending && len(dbMigrationsMap) > 0 {
		fmt.Println("No pending migrations, but orphaned records found in DB (see warnings).")
	} else {
		fmt.Println("Pending migrations found.")
	}

	return nil
}

// RunUp applies pending migrations.
func RunUp(cfg config.Config) error {
	fmt.Println("Running Migrate Up...")
	ctx := context.Background()

	// 1. Connect to DB
	ds, err := getDataSource(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to initialize data source for migrate up: %w", err)
	}
	defer ds.Close()
	dialect := ds.Dialect()
	migrationTable := cfg.Migration.TableName
	if migrationTable == "" {
		return fmt.Errorf("migration table name is not configured")
	}

	// 2. Ensure History Table Exists
	if err := ensureMigrationsTable(ctx, ds, migrationTable); err != nil {
		return err
	}

	// 3. Find Disk Migrations & Applied Migrations
	diskMigrations, err := findMigrationFiles(cfg.Migration.Directory)
	if err != nil {
		return err
	}
	appliedList, err := getAppliedMigrationsOrdered(ctx, ds, migrationTable, "ASC")
	if err != nil {
		return err
	}
	appliedMap := make(map[string]bool, len(appliedList))
	for _, rec := range appliedList {
		appliedMap[rec.ID] = true
	}

	// 4. Determine & Execute Pending Migrations
	pendingCount := 0
	appliedCount := 0
	fmt.Println("Applying pending migrations...")
	for _, mf := range diskMigrations {
		if _, applied := appliedMap[mf.ID]; !applied {
			pendingCount++
			fmt.Printf("--> Applying migration %s (%s)...\n", mf.ID, mf.Name)

			// Read and parse SQL
			file, err := os.Open(mf.Path)
			if err != nil {
				return fmt.Errorf("failed to open migration file '%s': %w", mf.Path, err)
			}
			upSQL, _, err := parseSQLMigration(file)
			file.Close()
			if err != nil {
				return fmt.Errorf("failed to parse migration file '%s': %w", mf.Path, err)
			}

			trimmedUpSQL := strings.TrimSpace(upSQL)
			if trimmedUpSQL == "" {
				fmt.Printf("    Skipping migration %s: No 'Up' SQL found.\n", mf.ID)
				// We still need to record it as applied even if empty
			}

			// Execute within a transaction
			err = func() error { // Use anonymous func for easier tx management
				txHandle, err := ds.BeginTx(ctx, nil) // Begin transaction
				if err != nil {
					return fmt.Errorf("failed to begin transaction for migration %s: %w", mf.ID, err)
				}
				defer txHandle.Rollback() // Ensure rollback happens if commit isn't reached

				// Execute Up SQL if present
				if trimmedUpSQL != "" {
					fmt.Printf("    Executing Up SQL...\n")
					if _, err := txHandle.Exec(ctx, trimmedUpSQL); err != nil {
						return fmt.Errorf("failed to execute 'Up' SQL for migration %s: %w", mf.ID, err)
					}
					fmt.Printf("    'Up' SQL executed successfully.\n")
				}

				// Record migration in history table
				insertSQL := dialect.InsertMigrationSQL(migrationTable)
				appliedTimestamp := time.Now().UTC()
				if _, err := txHandle.Exec(ctx, insertSQL, mf.ID, appliedTimestamp); err != nil {
					return fmt.Errorf("failed to record migration %s in history table: %w", mf.ID, err)
				}
				fmt.Printf("    Recorded migration %s in history table.\n", mf.ID)

				// Commit transaction
				if err := txHandle.Commit(); err != nil {
					return fmt.Errorf("failed to commit transaction for migration %s: %w", mf.ID, err)
				}
				return nil // Success
			}() // End anonymous func

			if err != nil {
				return err // Return error from transaction block
			}

			fmt.Printf("--> Successfully applied migration %s.\n", mf.ID)
			appliedCount++
		} // end if !applied
	} // end for diskMigrations

	if pendingCount == 0 {
		fmt.Println("No pending migrations to apply. Database is up to date.")
	} else {
		fmt.Printf("Finished applying migrations. Applied %d migration(s).\n", appliedCount)
	}
	return nil
}

// RunDown reverts the last applied migration(s).
// *** RunDown Implementation ***
func RunDown(cfg config.Config, steps int) error {
	fmt.Println("Running Migrate Down...")
	if steps <= 0 {
		fmt.Println("No steps specified for rollback (steps must be > 0).")
		return nil
	}
	fmt.Printf("  Steps to revert: %d\n", steps)
	ctx := context.Background()

	// 1. Connect to DB
	ds, err := getDataSource(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to initialize data source for migrate down: %w", err)
	}
	defer ds.Close()
	dialect := ds.Dialect()
	migrationTable := cfg.Migration.TableName
	if migrationTable == "" {
		return fmt.Errorf("migration table name is not configured")
	}

	// 2. Ensure History Table Exists (optional but good check)
	if err := ensureMigrationsTable(ctx, ds, migrationTable); err != nil {
		return err
	}

	// 3. Get Applied Migrations (Ordered DESC)
	appliedMigrations, err := getAppliedMigrationsOrdered(ctx, ds, migrationTable, "DESC")
	if err != nil {
		return err
	}
	if len(appliedMigrations) == 0 {
		fmt.Println("No migrations have been applied yet. Nothing to revert.")
		return nil
	}

	// 4. Determine Migrations to Revert
	if steps > len(appliedMigrations) {
		fmt.Printf("Requested %d steps rollback, but only %d migrations are applied. Reverting all.\n", steps, len(appliedMigrations))
		steps = len(appliedMigrations)
	}
	migrationsToRevert := appliedMigrations[:steps]

	// 5. Find Corresponding Disk Files (build map for lookup)
	diskFiles, err := findMigrationFiles(cfg.Migration.Directory)
	if err != nil {
		return fmt.Errorf("cannot find migration files needed for rollback: %w", err)
	}
	diskFilesMap := make(map[string]migrationFile, len(diskFiles))
	for _, mf := range diskFiles {
		diskFilesMap[mf.ID] = mf
	}

	// 6. Process Reverts
	revertedCount := 0
	fmt.Printf("Reverting the last %d applied migration(s)...\n", len(migrationsToRevert))
	for _, migrationRecord := range migrationsToRevert {
		fmt.Printf("--> Reverting migration %s...\n", migrationRecord.ID)

		// Find file
		mf, found := diskFilesMap[migrationRecord.ID]
		if !found {
			return fmt.Errorf("cannot revert migration %s: corresponding file not found in %s", migrationRecord.ID, cfg.Migration.Directory)
		}

		// Parse file for Down SQL
		file, err := os.Open(mf.Path)
		if err != nil {
			return fmt.Errorf("failed to open migration file '%s' for revert: %w", mf.Path, err)
		}
		_, downSQL, err := parseSQLMigration(file)
		file.Close()
		if err != nil {
			return fmt.Errorf("failed to parse migration file '%s' for revert: %w", mf.Path, err)
		}

		trimmedDownSQL := strings.TrimSpace(downSQL)

		// Execute within a transaction
		err = func() error {
			txHandle, err := ds.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to begin transaction for reverting migration %s: %w", migrationRecord.ID, err)
			}
			defer txHandle.Rollback()

			// Execute Down SQL if present
			if trimmedDownSQL != "" {
				fmt.Printf("    Executing Down SQL...\n")
				if _, err := txHandle.Exec(ctx, trimmedDownSQL); err != nil {
					return fmt.Errorf("failed to execute 'Down' SQL for migration %s: %w", migrationRecord.ID, err)
				}
				fmt.Printf("    'Down' SQL executed successfully.\n")
			} else {
				fmt.Printf("    No 'Down' SQL found to execute for migration %s.\n", migrationRecord.ID)
			}

			// Delete record from history table
			deleteSQL := dialect.DeleteMigrationSQL(migrationTable)
			if _, err := txHandle.Exec(ctx, deleteSQL, migrationRecord.ID); err != nil {
				return fmt.Errorf("failed to delete migration %s from history table: %w", migrationRecord.ID, err)
			}
			fmt.Printf("    Removed migration %s from history table.\n", migrationRecord.ID)

			// Commit
			if err := txHandle.Commit(); err != nil {
				return fmt.Errorf("failed to commit transaction for reverting migration %s: %w", migrationRecord.ID, err)
			}
			return nil // Success
		}() // End anonymous func

		if err != nil {
			return err // Return error from transaction block
		}

		fmt.Printf("--> Successfully reverted migration %s.\n", migrationRecord.ID)
		revertedCount++
	} // end for migrationsToRevert

	fmt.Printf("Finished reverting migrations. Reverted %d migration(s).\n", revertedCount)
	return nil
}
