//go:build integration

package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/dialects/common" // For MigrationRecord if needed later
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Blank import necessary dialect drivers for testing
	_ "github.com/chmenegatti/typegorm/pkg/dialects/mysql"
	// _ "github.com/chmenegatti/typegorm/pkg/dialects/sqlite"
	// _ "github.com/chmenegatti/typegorm/pkg/dialects/postgres"
)

const (
	testMigrationTable = "test_runner_migrations_history"
)

// --- Test Setup Helper ---

// Creates a temporary migration directory and config pointing to it.
// Also provides a connected DataSource and cleans up the history table.
// Creates a temporary migration directory and config pointing to it.
// Also provides a connected DataSource and cleans up the history table.
func setupMigrationTest(t *testing.T) (context.Context, config.Config, common.DataSource) {
	t.Helper()

	dialectEnv := os.Getenv("TYPEGORM_TEST_DIALECT")
	dsnEnv := os.Getenv("TYPEGORM_TEST_DSN")
	if dialectEnv == "" || dsnEnv == "" {
		t.Skip("Skipping migration integration test: TYPEGORM_TEST_DIALECT and TYPEGORM_TEST_DSN environment variables must be set.")
	}

	tempDir := t.TempDir()
	cfg := config.Config{
		Database:  config.DatabaseConfig{Dialect: dialectEnv, DSN: dsnEnv},
		Migration: config.MigrationConfig{Directory: tempDir, TableName: testMigrationTable},
	}
	ctx := context.Background()

	ds, err := getDataSource(cfg.Database)
	require.NoError(t, err, "Failed to get data source for migration test")
	require.NotNil(t, ds, "DataSource is nil")

	historyTableNameQuoted := ds.Dialect().Quote(testMigrationTable)

	// --- Cleanup Registration (LIFO Order) ---

	// 1. Register DB Close (will run LAST)
	t.Cleanup(func() {
		fmt.Printf("Closing migration test DB connection for %s...\n", t.Name())
		assert.NoError(t, ds.Close(), "Error closing migration test DB connection")
	})

	// 2. Register History Table Drop (will run before DB Close)
	t.Cleanup(func() {
		fmt.Printf("Dropping migration history table %s after test %s...\n", historyTableNameQuoted, t.Name())
		_, dropErr := ds.Exec(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", historyTableNameQuoted))
		assert.NoError(t, dropErr, "Failed to drop migration history table")
	})

	// *** NEW: 3. Register Data Table Drop (will run BEFORE History Table Drop) ***
	// Add all tables potentially created by any test using this setup
	t.Cleanup(func() {
		tablesToDrop := []string{"widgets", "gadgets", "items", "widgets_down", "gadgets_down", "good_table"}
		fmt.Printf("Dropping test data tables after test %s...\n", t.Name())
		for _, tableName := range tablesToDrop {
			tableNameQuoted := ds.Dialect().Quote(tableName)
			_, dropErr := ds.Exec(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", tableNameQuoted))
			// Don't fail the test if drop fails (table might not have been created), just log?
			if dropErr != nil {
				fmt.Printf("Warning: Failed to drop test data table %s: %v\n", tableNameQuoted, dropErr)
			}
			// assert.NoError(t, dropErr, "Failed to drop test data table %s", tableName)
		}
	})
	// *** End New Cleanup Step ***

	// Clean history table before test
	fmt.Printf("Dropping migration history table %s before test %s (if exists)...\n", historyTableNameQuoted, t.Name())
	_, _ = ds.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", historyTableNameQuoted)) // Ignore error if not exists

	// Also ensure data tables are dropped before test for good measure
	// (This might be redundant with the cleanup, but ensures clean state if previous cleanup failed)
	tablesToDrop := []string{"widgets", "gadgets", "items", "widgets_down", "gadgets_down", "good_table"}
	fmt.Printf("Dropping test data tables before test %s (if exists)...\n", t.Name())
	for _, tableName := range tablesToDrop {
		tableNameQuoted := ds.Dialect().Quote(tableName)
		_, _ = ds.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tableNameQuoted))
	}

	return ctx, cfg, ds
}

// Helper to create a migration file in the temp directory
func createMigrationFile(t *testing.T, dir, id, name, upSQL, downSQL string) string {
	t.Helper()
	filename := fmt.Sprintf("%s_%s.sql", id, name)
	filePath := filepath.Join(dir, filename)
	content := fmt.Sprintf("-- Migration: %s\n%s\n%s\n\n%s\n%s\n", name, markerUp, upSQL, markerDown, downSQL)
	err := os.WriteFile(filePath, []byte(content), 0644)
	require.NoError(t, err, "Failed to write migration file %s", filename)
	return filePath
}

// Helper to create a dummy Go migration file (content doesn't matter much anymore)
// We still need the file on disk for discovery, but registration happens in the test.
func createDummyGoMigrationFile(t *testing.T, dir, id, name string) string {
	t.Helper()
	safeName := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	filePath := filepath.Join(dir, fmt.Sprintf("%s_%s.go", id, safeName))
	// Minimal content, init() is not needed here
	content := fmt.Sprintf(`package main

// Migration %s - %s
// Registration happens in the test file.
`, id, name)
	errWriteDir := os.MkdirAll(dir, os.ModePerm)
	require.NoError(t, errWriteDir)
	errWrite := os.WriteFile(filePath, []byte(content), 0644)
	require.NoError(t, errWrite)
	return filePath
}

// Helper to check if a table exists (basic check)
func tableExists(ctx context.Context, ds common.DataSource, tableName string) bool {
	dialect := ds.Dialect()
	var query string
	var args []any
	var dbName string // Variable to hold database name if needed

	// Get current database name - dialect specific
	switch dialect.Name() {
	case "mysql":
		// For MySQL/MariaDB, DATABASE() function works
		err := ds.QueryRow(ctx, "SELECT DATABASE()").Scan(&dbName)
		if err != nil || dbName == "" {
			fmt.Printf("Warning: Could not determine database name for tableExists check: %v\n", err)
			// Fallback to a simple select, less reliable
			query = fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", dialect.Quote(tableName))
		} else {
			query = fmt.Sprintf("SELECT 1 FROM information_schema.tables WHERE table_schema = %s AND table_name = %s LIMIT 1",
				dialect.BindVar(1), dialect.BindVar(2))
			args = append(args, dbName, tableName)
		}
	case "postgres":
		// For PostgreSQL, current_database() works
		err := ds.QueryRow(ctx, "SELECT current_database()").Scan(&dbName)
		if err != nil || dbName == "" {
			fmt.Printf("Warning: Could not determine database name for tableExists check: %v\n", err)
			query = fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", dialect.Quote(tableName))
		} else {
			// Postgres uses table_schema='public' by default, but let's check information_schema
			query = fmt.Sprintf("SELECT 1 FROM information_schema.tables WHERE table_catalog = %s AND table_name = %s LIMIT 1",
				dialect.BindVar(1), dialect.BindVar(2))
			args = append(args, dbName, tableName)
		}
	case "sqlite":
		// SQLite uses sqlite_master
		query = fmt.Sprintf("SELECT 1 FROM sqlite_master WHERE type='table' AND name = %s LIMIT 1", dialect.BindVar(1))
		args = append(args, tableName)
	default:
		// Fallback for unknown dialects - less reliable
		fmt.Printf("Warning: Using fallback tableExists check for dialect %s\n", dialect.Name())
		query = fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", dialect.Quote(tableName))
	}

	// fmt.Printf("DEBUG [tableExists] Query: %s | Args: %v\n", query, args) // Optional debug log

	var exists int
	err := ds.QueryRow(ctx, query, args...).Scan(&exists)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// fmt.Printf("DEBUG [tableExists] Table '%s' not found (sql.ErrNoRows).\n", tableName)
			return false // Definitely not found
		}
		// Log other errors but assume it might exist or access is denied
		fmt.Printf("Warning [tableExists] Error checking for table '%s': %v\n", tableName, err)
		return false // Treat other errors as "not found" for simplicity in tests
	}
	// fmt.Printf("DEBUG [tableExists] Table '%s' found.\n", tableName)
	return exists == 1 // Found if scan succeeded
}

// Helper to get applied migration IDs from history table
func getHistoryIDs(ctx context.Context, ds common.DataSource, tableName string) ([]string, error) {
	records, err := getAppliedMigrationsOrdered(ctx, ds, tableName, "ASC")
	if err != nil {
		// If table doesn't exist yet, return empty list, no error
		// Need a better way to check this error across DBs
		if strings.Contains(err.Error(), "exist") || strings.Contains(err.Error(), "no such table") { // Basic check
			return []string{}, nil
		}
		return nil, err
	}
	ids := make([]string, len(records))
	for i, rec := range records {
		ids[i] = rec.ID
	}
	return ids, nil
}

// --- Test Cases ---

// Test migration for creating and dropping a table
type CreateGoTestTableMig struct{}

func (m *CreateGoTestTableMig) Up(ctx context.Context, db *sql.DB) error {
	fmt.Println("Running Go migration Up: create_go_test_table")
	_, err := db.ExecContext(ctx, "CREATE TABLE go_test_create_go_test_table (id INT);")
	return err
}
func (m *CreateGoTestTableMig) Down(ctx context.Context, db *sql.DB) error {
	fmt.Println("Running Go migration Down: create_go_test_table")
	_, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS go_test_create_go_test_table;")
	return err
}

// Test migration for creating and dropping another table
type CreateAndDropGoTableMig struct{}

func (m *CreateAndDropGoTableMig) Up(ctx context.Context, db *sql.DB) error {
	fmt.Println("Running Go migration Up: create_and_drop_go_table")
	_, err := db.ExecContext(ctx, "CREATE TABLE go_test_create_and_drop_go_table (id INT);")
	return err
}
func (m *CreateAndDropGoTableMig) Down(ctx context.Context, db *sql.DB) error {
	fmt.Println("Running Go migration Down: create_and_drop_go_table")
	_, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS go_test_create_and_drop_go_table;")
	return err
}

// Test migration with an error in the Up method
type GoUpErrorMig struct{}

func (m *GoUpErrorMig) Up(ctx context.Context, db *sql.DB) error {
	fmt.Println("Running Go migration Up: go_up_error (will fail)")
	_, err := db.ExecContext(ctx, "CREATE TABL bad_sql;") // Invalid SQL
	return err
}
func (m *GoUpErrorMig) Down(ctx context.Context, db *sql.DB) error {
	fmt.Println("Running Go migration Down: go_up_error")
	return nil // Down should succeed or do nothing if Up failed
}

func TestMigrationRunner_RunUp_Success(t *testing.T) {
	ctx, cfg, ds := setupMigrationTest(t)
	migrationDir := cfg.Migration.Directory

	// 1. Arrange: Create migration files
	ts1 := time.Now().UTC().Add(-2 * time.Minute).Format("20060102150405")
	ts2 := time.Now().UTC().Add(-1 * time.Minute).Format("20060102150405")
	createMigrationFile(t, migrationDir, ts1, "create_widgets",
		"CREATE TABLE widgets (id INT PRIMARY KEY, name VARCHAR(50));",
		"DROP TABLE widgets;")
	createMigrationFile(t, migrationDir, ts2, "create_gadgets",
		"CREATE TABLE gadgets (gadget_id VARCHAR(10) PRIMARY KEY, description TEXT);",
		"DROP TABLE gadgets;")

	// 2. Act: Run migrations up
	err := RunUp(cfg)
	require.NoError(t, err, "RunUp failed")

	// 3. Assert: Check tables exist and history table is correct
	// Use require for critical checks like table existence after migration
	require.True(t, tableExists(ctx, ds, "widgets"), "widgets table should exist")
	require.True(t, tableExists(ctx, ds, "gadgets"), "gadgets table should exist")

	history, err := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, err, "Failed to get migration history")
	assert.Equal(t, []string{ts1, ts2}, history, "Migration history should contain both migration IDs in order")
}

func TestMigrationRunner_RunUp_NoPending(t *testing.T) {
	ctx, cfg, ds := setupMigrationTest(t)
	migrationDir := cfg.Migration.Directory

	// 1. Arrange: Create migration file AND run it up first
	ts1 := time.Now().UTC().Format("20060102150405")
	createMigrationFile(t, migrationDir, ts1, "create_items", "CREATE TABLE items (item_id INT);", "DROP TABLE items;")
	err := RunUp(cfg) // Run up the first time
	require.NoError(t, err, "Initial RunUp failed")
	require.True(t, tableExists(ctx, ds, "items"), "items table should exist after initial RunUp")
	initialHistory, _ := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.Equal(t, []string{ts1}, initialHistory)

	// 2. Act: Run migrations up again
	err = RunUp(cfg)
	require.NoError(t, err, "Second RunUp failed") // Should succeed with "no pending" message

	// 3. Assert: History table should remain unchanged
	finalHistory, err := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, err, "Failed to get migration history after second RunUp")
	assert.Equal(t, []string{ts1}, finalHistory, "Migration history should not change on second RunUp")
}

func TestMigrationRunner_RunDown_Success(t *testing.T) {
	ctx, cfg, ds := setupMigrationTest(t)
	migrationDir := cfg.Migration.Directory

	// 1. Arrange: Create and apply migrations
	ts1 := time.Now().UTC().Add(-2 * time.Minute).Format("20060102150405")
	ts2 := time.Now().UTC().Add(-1 * time.Minute).Format("20060102150405")
	createMigrationFile(t, migrationDir, ts1, "create_widgets_down", "CREATE TABLE widgets_down (id INT);", "DROP TABLE widgets_down;")
	createMigrationFile(t, migrationDir, ts2, "create_gadgets_down", "CREATE TABLE gadgets_down (id INT);", "DROP TABLE gadgets_down;")
	err := RunUp(cfg)
	require.NoError(t, err)
	require.True(t, tableExists(ctx, ds, "widgets_down"))
	require.True(t, tableExists(ctx, ds, "gadgets_down"))
	initialHistory, _ := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.Equal(t, []string{ts1, ts2}, initialHistory)

	// 2. Act: Run down one step
	err = RunDown(cfg, 1)
	require.NoError(t, err, "RunDown failed")

	// 3. Assert: Last migration reverted
	assert.True(t, tableExists(ctx, ds, "widgets_down"), "widgets_down table should still exist")
	assert.False(t, tableExists(ctx, ds, "gadgets_down"), "gadgets_down table should be dropped")
	history1, err := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, err)
	assert.Equal(t, []string{ts1}, history1, "History should only contain first migration after one step down")

	// 4. Act: Run down another step
	err = RunDown(cfg, 1)
	require.NoError(t, err, "Second RunDown failed")

	// 5. Assert: All migrations reverted
	assert.False(t, tableExists(ctx, ds, "widgets_down"), "widgets_down table should be dropped")
	history2, err := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, err)
	assert.Empty(t, history2, "History should be empty after all down")
}

func TestMigrationRunner_RunUp_ErrorInSQL(t *testing.T) {
	ctx, cfg, ds := setupMigrationTest(t)
	migrationDir := cfg.Migration.Directory

	// 1. Arrange: Create valid first migration, invalid second migration
	ts1 := time.Now().UTC().Add(-2 * time.Minute).Format("20060102150405")
	ts2 := time.Now().UTC().Add(-1 * time.Minute).Format("20060102150405")
	createMigrationFile(t, migrationDir, ts1, "create_good_table", "CREATE TABLE good_table (id INT);", "DROP TABLE good_table;")
	createMigrationFile(t, migrationDir, ts2, "create_bad_table", "CREATE TABL bad_syntax (id INT);", "-- Down") // Invalid SQL

	// 2. Act: Run migrations up - should fail on the second one
	err := RunUp(cfg)
	require.Error(t, err, "RunUp should fail due to bad SQL")
	// Check if the error message indicates the specific migration failure
	assert.Contains(t, err.Error(), ts2, "Error message should mention the failing migration ID")
	assert.Contains(t, err.Error(), "failed to execute 'Up' SQL", "Error message should indicate SQL execution failure")

	// 3. Assert: First migration should be applied, second should not be in history
	assert.True(t, tableExists(ctx, ds, "good_table"), "good_table should exist (first migration)")
	assert.False(t, tableExists(ctx, ds, "bad_syntax"), "bad_syntax table should not exist (second migration failed)") // Check table name used in bad SQL

	history, histErr := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, histErr)
	assert.Equal(t, []string{ts1}, history, "Only the first migration should be recorded in history")
}

// NOTE: Running Go migrations in tests requires the test binary to be built
// in a way that includes the migration packages so their init() functions run.
// This usually happens automatically with `go test`.

// --- Tests for Go Migrations (Updated with manual registration) ---

func TestMigrationRunner_Go_RunUp_Success(t *testing.T) {
	ctx, cfg, ds := setupMigrationTest(t)
	migrationDir := cfg.Migration.Directory

	// 1. Arrange: Create dummy Go migration file and Register the implementation
	migrationID := time.Now().UTC().Format("20060102150405")
	migrationName := "create_go_test_table"
	safeName := "create_go_test_table"
	_ = createDummyGoMigrationFile(t, migrationDir, migrationID, migrationName) // Create file for discovery
	RegisterGoMigration(migrationID, &CreateGoTestTableMig{})                   // Manually register
	t.Cleanup(func() { delete(goMigrationsRegistry, migrationID) })             // Clean up registry

	// 2. Act: Run migrations up
	err := RunUp(cfg)
	require.NoError(t, err, "RunUp failed for Go migration")

	// 3. Assert: Check table exists and history table is correct
	tableName := fmt.Sprintf("go_test_%s", safeName)
	require.True(t, tableExists(ctx, ds, tableName), "%s table should exist", tableName)
	history, err := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, err)
	assert.Equal(t, []string{migrationID}, history, "Migration history should contain the Go migration ID")
}

func TestMigrationRunner_Go_RunDown_Success(t *testing.T) {
	ctx, cfg, ds := setupMigrationTest(t)
	migrationDir := cfg.Migration.Directory

	// 1. Arrange: Create dummy file, register Go migration, and apply it
	migrationID := time.Now().UTC().Format("20060102150405")
	migrationName := "create_and_drop_go_table"
	safeName := "create_and_drop_go_table"
	_ = createDummyGoMigrationFile(t, migrationDir, migrationID, migrationName)
	RegisterGoMigration(migrationID, &CreateAndDropGoTableMig{})
	t.Cleanup(func() { delete(goMigrationsRegistry, migrationID) })
	err := RunUp(cfg)
	require.NoError(t, err)
	tableName := fmt.Sprintf("go_test_%s", safeName)
	require.True(t, tableExists(ctx, ds, tableName), "Table should exist after RunUp")
	initialHistory, _ := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.Equal(t, []string{migrationID}, initialHistory)

	// 2. Act: Run down one step
	err = RunDown(cfg, 1)
	require.NoError(t, err, "RunDown failed for Go migration")

	// 3. Assert: Table should be dropped and history empty
	assert.False(t, tableExists(ctx, ds, tableName), "%s table should be dropped", tableName)
	history, err := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, err)
	assert.Empty(t, history, "History should be empty after down")
}

func TestMigrationRunner_Go_RunUp_Error(t *testing.T) {
	ctx, cfg, ds := setupMigrationTest(t)
	migrationDir := cfg.Migration.Directory

	// 1. Arrange: Create dummy file and register Go migration with faulty Up
	migrationID := time.Now().UTC().Format("20060102150405")
	migrationName := "go_up_error"
	_ = createDummyGoMigrationFile(t, migrationDir, migrationID, migrationName)
	RegisterGoMigration(migrationID, &GoUpErrorMig{}) // Register the faulty one
	t.Cleanup(func() { delete(goMigrationsRegistry, migrationID) })

	// 2. Act: Run migrations up
	err := RunUp(cfg)
	require.Error(t, err, "RunUp should fail")
	// Error comes from the Go migration's Up method directly now
	assert.Contains(t, err.Error(), "failed to execute 'Up' method", "Error message mismatch")
	assert.Contains(t, err.Error(), migrationID, "Error message should contain migration ID")
	// Check for underlying DB error if possible (depends on driver)
	// assert.Contains(t, err.Error(), "syntax error", "Underlying DB error missing")

	// 3. Assert: Table should not exist and history should be empty
	assert.False(t, tableExists(ctx, ds, "bad_sql"), "Table created by failing migration should not exist")
	history, histErr := getHistoryIDs(ctx, ds, cfg.Migration.TableName)
	require.NoError(t, histErr)
	assert.Empty(t, history, "History should be empty after failed migration")
}
