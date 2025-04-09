// driver/sqlite/sqlite_test.go
package sqlite // Test file is in the same package 'sqlite'

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	// Import the root typegorm package mainly for the DriverType constant
	"github.com/chmenegatti/typegorm"
)

func TestSQLiteConnectionAndPing(t *testing.T) {
	// t.Parallel() // You could mark this test as parallelizable if needed

	// Use t.TempDir() to create a temporary directory for the database file.
	// Go's testing package automatically cleans this up after the test runs.
	tempDir := t.TempDir() // Change this to a suitable temp directory for your environment
	dbFilePath := filepath.Join(tempDir, "test_connection.db")

	t.Logf("TestSQLiteConnectionAndPing: Using temporary database file: %s", dbFilePath)

	// --- Test Setup ---
	config := Config{
		Database: dbFilePath,
		Options: map[string]string{
			"_journal":      "WAL",    // Recommended for concurrency
			"_busy_timeout": "5000",   // Wait 5s if db is locked
			"_foreign_keys": "true",   // Enforce foreign keys
			"_synchronous":  "NORMAL", // Balance speed and safety
		},
	}

	// Instantiate the DataSource directly using the package's constructor
	dataSource := NewDataSource()

	// --- Test Actions ---

	// 1. Test Connect
	err := dataSource.Connect(config)
	if err != nil {
		// t.Fatal stops the current test function immediately
		t.Fatalf("dataSource.Connect(%+v) failed: %v", config, err)
	}
	t.Log("dataSource.Connect() successful.")

	// 2. Setup cleanup using t.Cleanup - guarantees execution even on test failures/panics
	// Cleanup functions run in LIFO (Last-In, First-Out) order.
	t.Cleanup(func() {
		t.Log("Cleanup: Closing connection...")
		if err := dataSource.Close(); err != nil {
			// t.Error logs the error but allows the test (and other cleanups) to continue
			t.Errorf("dataSource.Close() failed during cleanup: %v", err)
		} else {
			t.Log("Cleanup: Connection closed.")
		}
		// We don't need to explicitly remove the file, t.TempDir() handles the directory.
	})

	// 3. Test Ping
	t.Log("Pinging database...")
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel() // Ensure context is cancelled

	if err := dataSource.Ping(pingCtx); err != nil {
		t.Fatalf("dataSource.Ping() failed: %v", err)
	}
	t.Log("dataSource.Ping() successful.")

	// --- Optional Assertions ---

	// 4. Verify Driver Type
	if driverType := dataSource.GetDriverType(); driverType != typegorm.SQLite {
		t.Errorf("dataSource.GetDriverType() = %q, want %q", driverType, typegorm.SQLite)
	} else {
		t.Logf("dataSource.GetDriverType() returned correct type: %q", driverType)
	}

	// 5. Verify GetDB
	sqlDB, dbErr := dataSource.GetDB()
	if dbErr != nil {
		t.Errorf("dataSource.GetDB() returned error: %v", dbErr)
	} else if sqlDB == nil {
		t.Error("dataSource.GetDB() returned nil *sql.DB")
	} else {
		// Optional: Check stats if *sql.DB is available
		stats := sqlDB.Stats()
		t.Logf("sql.DB Stats: OpenConnections=%d, InUse=%d, Idle=%d, WaitCount=%d",
			stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
		if stats.OpenConnections == 0 {
			// After connect & ping, we expect at least one connection to have been opened.
			// It might be idle now, but should have been established.
			t.Error("Expected at least one open connection in DB stats after successful connect/ping")
		}
	}

	// --- Test End ---
	// No explicit action needed, cleanup runs automatically
	t.Log("TestSQLiteConnectionAndPing finished successfully.")
}

// You can add more test functions in this file later, e.g.,
// func TestSQLiteConnectionFailure_EmptyPath(t *testing.T) { ... }
// func TestSQLiteConnectionFailure_InvalidOptions(t *testing.T) { ... }
