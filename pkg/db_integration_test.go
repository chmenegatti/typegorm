// pkg/typegorm/db_integration_test.go
//go:build integration

// To run tests: go test -tags=integration ./pkg/typegorm/... -v

package typegorm

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chmenegatti/typegorm/pkg/config" // Use correct import path
	"github.com/chmenegatti/typegorm/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Blank import necessary dialect drivers for testing
	_ "github.com/chmenegatti/typegorm/pkg/dialects/mysql"
	// _ "github.com/chmenegatti/typegorm/pkg/dialects/sqlite"
	// _ "github.com/chmenegatti/typegorm/pkg/dialects/postgres"
)

// --- Test Struct ---

type CreateTestUser struct {
	ID        uint       `typegorm:"primaryKey;autoIncrement"`
	Name      string     `typegorm:"column:user_name;size:100;not null"`
	Email     *string    `typegorm:"unique;size:255"` // Nullable unique email
	Age       int        `typegorm:"default:20"`      // Default value
	CreatedAt time.Time  // Auto not null
	UpdatedAt *time.Time // Nullable time
}

// --- Test Setup Helper ---

// Reads connection details from ENV vars and sets up DB connection and table.
// Skips test if ENV vars are not set.
func setupIntegrationTest(t *testing.T) (context.Context, *DB, *schema.Model) {
	t.Helper()

	dialect := os.Getenv("TYPEGORM_TEST_DIALECT")
	dsn := os.Getenv("TYPEGORM_TEST_DSN")

	if dialect == "" || dsn == "" {
		t.Skip("Skipping integration test: TYPEGORM_TEST_DIALECT and TYPEGORM_TEST_DSN environment variables must be set.")
	}

	// Create minimal config for Open
	cfg := config.Config{
		Database: config.DatabaseConfig{
			Dialect: dialect,
			DSN:     dsn,
			// Pool settings can be default or also from ENV if needed
		},
		// Other config sections if needed by Open or DB
	}

	ctx := context.Background()

	// Open connection
	db, err := Open(cfg) // Use the main Open function
	require.NoError(t, err, "Failed to open DB connection")
	require.NotNil(t, db, "DB object should not be nil")

	model, err := db.GetModel(&CreateTestUser{})
	require.NoError(t, err, "Failed to parse test model schema")
	require.NotNil(t, model)
	require.NotEmpty(t, model.TableName, "Parsed model should have a table name")

	//tableNameQuoted := db.source.Dialect().Quote(model.TableName)

	// Ensure DB is closed after test
	t.Cleanup(func() {
		fmt.Println("Closing test database connection...")
		err := db.Close()
		assert.NoError(t, err, "Error closing test DB connection")
	})

	tableNameQuoted := db.source.Dialect().Quote(model.TableName)

	// Use AutoMigrate to ensure table exists
	// fmt.Printf("Ensuring table '%s' exists for integration test...\n", tableName)
	fmt.Printf("Ensuring table %s exists for test %s...\n", tableNameQuoted, t.Name())
	err = db.AutoMigrate(ctx, &CreateTestUser{})
	require.NoError(t, err, "AutoMigrate failed")

	// Clean up table before test runs (optional, ensures clean slate)
	// // Alternatively, drop at the end in Cleanup. Dropping before is safer.
	// fmt.Printf("Cleaning up table '%s' before test...\n", tableNameQuoted)
	// cleanupSQL := fmt.Sprintf("DELETE FROM %s", tableNameQuoted)
	// // DROP TABLE IF EXISTS is another option for Cleanup func below
	// _, delErr := db.source.Exec(ctx, cleanupSQL)
	// require.NoError(t, delErr, "Failed to clean up table before test")
	// // Ignore "table not found" errors during cleanup delete if AutoMigrate handled creation
	// // require.NoError(t, delErr, "Failed to clean up table before test")

	// Optional: Add explicit DROP TABLE in Cleanup for after the test run
	t.Cleanup(func() {
		fmt.Printf("Dropping table '%s' after test...\n", tableNameQuoted)
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableNameQuoted)
		_, dropErr := db.source.Exec(context.Background(), dropSQL) // Use fresh context
		assert.NoError(t, dropErr, "Failed to drop table after test")

	})

	return ctx, db, model
}

// --- Test Cases ---

func TestDBCreate_Success_AutoIncrement(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t) // Setup gets context and DB connection

	userName := fmt.Sprintf("TestUser_%d", time.Now().UnixNano())
	userEmail := fmt.Sprintf("test_%d@example.com", time.Now().UnixNano())
	user := CreateTestUser{
		Name:  userName,
		Email: &userEmail, // Use pointer for nullable string
		Age:   30,
		// ID and CreatedAt/UpdatedAt are zero/nil initially
	}

	// Call the Create method
	result := db.Create(ctx, &user) // Pass pointer to struct

	// Assertions on the Result
	require.NoError(t, result.Error, "db.Create returned an error")
	assert.EqualValues(t, 1, result.RowsAffected, "RowsAffected should be 1")
	assert.True(t, result.LastInsertID > 0, "LastInsertID should be positive for auto-increment")

	// Assertions on the input struct (ID should be set)
	assert.True(t, user.ID > 0, "User ID should be set by Create")
	assert.EqualValues(t, result.LastInsertID, user.ID, "User ID should match LastInsertID") // Use EqualValues for int vs uint comparison if needed
	assert.Equal(t, userName, user.Name)                                                     // Ensure other fields weren't changed

	// Verify directly in the database
	var dbUser CreateTestUser
	// *** Use model info for table/column names ***
	require.NotEmpty(t, model.PrimaryKeys, "Test model requires a primary key for verification")
	tableNameQuoted := db.source.Dialect().Quote(model.TableName)
	pkColNameQuoted := db.source.Dialect().Quote(model.PrimaryKeys[0].DBName) // Assumes single PK

	// Build SELECT query field list dynamically from model? More robust but complex.
	// Manual list for now, ensure it matches CreateTestUser fields.
	selectCols := "id, user_name, email, age, created_at, updated_at"
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?",
		selectCols,
		tableNameQuoted,
		pkColNameQuoted,
	)

	// Use the underlying DataSource to query
	rowScanner := db.GetDataSource().QueryRow(ctx, query, user.ID) // Use the ID set by Create
	scanErr := rowScanner.Scan(                                    // Use Scan method from RowScanner interface
		&dbUser.ID,
		&dbUser.Name,
		&dbUser.Email, // Scan into pointer type
		&dbUser.Age,
		&dbUser.CreatedAt,
		&dbUser.UpdatedAt, // Scan into pointer type
	)

	require.NoError(t, scanErr, "Failed to scan user from database")
	assert.Equal(t, user.ID, dbUser.ID)
	assert.Equal(t, user.Name, dbUser.Name)
	require.NotNil(t, dbUser.Email, "Email should not be nil in DB")
	assert.Equal(t, *user.Email, *dbUser.Email)
	assert.Equal(t, user.Age, dbUser.Age)
	assert.False(t, dbUser.CreatedAt.IsZero(), "CreatedAt should be set in DB")
	assert.Nil(t, dbUser.UpdatedAt, "UpdatedAt should be nil in DB") // Assuming DB default isn't set
}

// TODO: Add more test cases for Create:
// - TestDBCreate_NilInput
// - TestDBCreate_NonPointerInput
// - TestDBCreate_NonStructPointerInput
// - TestDBCreate_UniqueConstraintViolation (e.g., insert same email twice)
// - TestDBCreate_NotNullConstraintViolation (e.g., try inserting with Name="" when column is NOT NULL)
// - TestDBCreate_DefaultValue (insert with Age=0, verify it becomes 20 in DB)
