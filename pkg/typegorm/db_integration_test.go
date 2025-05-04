// pkg/typegorm/db_integration_test.go
//go:build integration

// To run tests: go test -tags=integration ./pkg/typegorm/... -v

package typegorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
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

// *** NEW Test for FindByID Success ***
func TestDBFindByID_Success(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t) // We don't need the model back here directly

	// 1. Arrange: Create a record first
	userName := "FindMeUser"
	userEmail := fmt.Sprintf("findme_%d@example.com", time.Now().UnixNano())
	originalUser := CreateTestUser{
		Name:  userName,
		Email: &userEmail,
		Age:   42,
	}
	createResult := db.Create(ctx, &originalUser)
	require.NoError(t, createResult.Error, "Setup: Failed to create user for FindByID test")
	require.True(t, originalUser.ID > 0, "Setup: Created user should have an ID")

	// 2. Act: Find the record by its ID
	var foundUser CreateTestUser                                // Create a new variable to scan into
	findResult := db.FindByID(ctx, &foundUser, originalUser.ID) // Pass pointer and ID

	// 3. Assert
	require.NoError(t, findResult.Error, "FindByID returned an error")
	assert.Equal(t, originalUser.ID, foundUser.ID, "Found user ID mismatch")
	assert.Equal(t, originalUser.Name, foundUser.Name, "Found user Name mismatch")
	require.NotNil(t, foundUser.Email, "Found user Email should not be nil")
	assert.Equal(t, *originalUser.Email, *foundUser.Email, "Found user Email mismatch")
	assert.Equal(t, originalUser.Age, foundUser.Age, "Found user Age mismatch")
	assert.False(t, foundUser.CreatedAt.IsZero(), "Found user CreatedAt should not be zero")
	// UpdatedAt was inserted as nil, should still be nil
	assert.Nil(t, foundUser.UpdatedAt, "Found user UpdatedAt should be nil")
}

// *** NEW Test for FindByID Not Found ***
func TestDBFindByID_NotFound(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t) // Setup ensures table exists but is empty

	nonExistentID := uint(999999999) // An ID that is extremely unlikely to exist
	var foundUser CreateTestUser     // Variable to attempt scanning into

	// Act: Find a non-existent record
	findResult := db.FindByID(ctx, &foundUser, nonExistentID)

	// Assert
	require.Error(t, findResult.Error, "FindByID should return an error for non-existent ID")
	// Check if the error is specifically sql.ErrNoRows
	assert.True(t, errors.Is(findResult.Error, sql.ErrNoRows), "Error should be sql.ErrNoRows")

	// Ensure the destination struct was not modified (should still have zero values)
	assert.Zero(t, foundUser.ID)
	assert.Empty(t, foundUser.Name)
	assert.Nil(t, foundUser.Email)
}

// --- NEW Tests for DB.Delete ---

func TestDBDelete_Success(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create a record to delete
	userToCreate := CreateTestUser{Name: "ToDelete", Age: 99}
	createResult := db.Create(ctx, &userToCreate)
	require.NoError(t, createResult.Error, "Setup: Failed to create user for Delete test")
	require.True(t, userToCreate.ID > 0, "Setup: Created user should have an ID")
	createdID := userToCreate.ID // Store the ID

	// 2. Act: Delete the record using the struct instance (which now has the ID)
	deleteResult := db.Delete(ctx, &userToCreate)

	// 3. Assert Delete Result
	require.NoError(t, deleteResult.Error, "Delete returned an error")
	assert.EqualValues(t, 1, deleteResult.RowsAffected, "RowsAffected should be 1 for successful delete")

	// 4. Assert: Verify the record is actually gone using FindByID
	var foundUser CreateTestUser
	verifyResult := db.FindByID(ctx, &foundUser, createdID) // Try to find the deleted ID

	require.Error(t, verifyResult.Error, "FindByID after Delete should return an error")
	assert.True(t, errors.Is(verifyResult.Error, sql.ErrNoRows), "FindByID error after Delete should be sql.ErrNoRows")
}

func TestDBDelete_NotFound(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create a user struct with an ID that doesn't exist
	nonExistentUser := CreateTestUser{
		ID:   9999999, // Non-existent ID
		Name: "Ghost",
	}

	// 2. Act: Attempt to delete the non-existent record
	deleteResult := db.Delete(ctx, &nonExistentUser)

	// 3. Assert
	require.NoError(t, deleteResult.Error, "Delete should not return a SQL error when record not found")
	assert.EqualValues(t, 0, deleteResult.RowsAffected, "RowsAffected should be 0 when deleting non-existent record")
}

func TestDBDelete_ZeroPK(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create a user struct with the default zero ID
	zeroPKUser := CreateTestUser{
		Name: "ZeroIDUser",
		// ID is implicitly 0
	}

	// 2. Act: Attempt to delete the record with a zero PK
	deleteResult := db.Delete(ctx, &zeroPKUser)

	// 3. Assert
	require.Error(t, deleteResult.Error, "Delete should return an error when PK is zero")
	assert.Contains(t, deleteResult.Error.Error(), "primary key field", "Error message should mention primary key")
	assert.Contains(t, deleteResult.Error.Error(), "zero value", "Error message should mention zero value")

	// Optional: Verify no records were accidentally deleted (e.g., query count)
	// ds := db.GetDataSource()
	// var count int
	// row := ds.QueryRow(ctx, "SELECT COUNT(*) FROM `create_test_users`")
	// err := row.Scan(&count)
	// require.NoError(t, err)
	// assert.Zero(t, count, "No records should be present after failed delete attempt")
}

// --- NEW Tests for DB.FindFirst ---

func TestDBFindFirst_ByStruct_Success(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create multiple records
	email1 := "findfirst1@example.com"
	email2 := "findfirst2@example.com"
	user1 := CreateTestUser{Name: "FindFirstAlice", Email: &email1, Age: 30}
	user2 := CreateTestUser{Name: "FindFirstBob", Email: &email2, Age: 35}
	res1 := db.Create(ctx, &user1)
	require.NoError(t, res1.Error)
	res2 := db.Create(ctx, &user2)
	require.NoError(t, res2.Error)

	// 2. Act: Find using a struct pointer with one non-zero field
	var foundUser CreateTestUser
	// Query by example: find where Name is "FindFirstBob"
	query := &CreateTestUser{Name: "FindFirstBob"}
	findResult := db.FindFirst(ctx, &foundUser, query)

	// 3. Assert
	require.NoError(t, findResult.Error, "FindFirst returned an error")
	assert.EqualValues(t, 1, findResult.RowsAffected, "FindFirst should affect 1 row")
	assert.Equal(t, user2.ID, foundUser.ID, "Found wrong user ID") // Should match user2
	assert.Equal(t, "FindFirstBob", foundUser.Name)
	require.NotNil(t, foundUser.Email)
	assert.Equal(t, email2, *foundUser.Email)
	assert.Equal(t, 35, foundUser.Age)
}

func TestDBFindFirst_ByStruct_NotFound(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create some records (optional, table is empty initially)
	email1 := "findfirst_nf1@example.com"
	user1 := CreateTestUser{Name: "NotFoundAlice", Email: &email1, Age: 30}
	res1 := db.Create(ctx, &user1)
	require.NoError(t, res1.Error)

	// 2. Act: Find using criteria that doesn't match
	var foundUser CreateTestUser
	query := &CreateTestUser{Name: "NoSuchUserExists"}
	findResult := db.FindFirst(ctx, &foundUser, query)

	// 3. Assert
	require.Error(t, findResult.Error, "FindFirst should return an error when record not found")
	assert.True(t, errors.Is(findResult.Error, sql.ErrNoRows), "Error should be sql.ErrNoRows")
	assert.Zero(t, foundUser.ID, "foundUser should not be populated") // Check if dest is untouched
}

func TestDBFindFirst_ByMap_Success(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: Create multiple records
	email1 := "findmap1@example.com"
	email2 := "findmap2@example.com"
	user1 := CreateTestUser{Name: "FindMapAlice", Email: &email1, Age: 40}
	user2 := CreateTestUser{Name: "FindMapBob", Email: &email2, Age: 45}
	res1 := db.Create(ctx, &user1)
	require.NoError(t, res1.Error)
	res2 := db.Create(ctx, &user2)
	require.NoError(t, res2.Error)

	// 2. Act: Find using a map with DB column names
	var foundUser CreateTestUser
	// Use column names from parsed model for safety
	nameCol, _ := model.GetFieldByDBName("user_name")
	ageCol, _ := model.GetFieldByDBName("age")
	query := map[string]any{
		nameCol.DBName: "FindMapAlice",
		ageCol.DBName:  40,
	}
	findResult := db.FindFirst(ctx, &foundUser, query)

	// 3. Assert
	require.NoError(t, findResult.Error, "FindFirst returned an error")
	assert.EqualValues(t, 1, findResult.RowsAffected)
	assert.Equal(t, user1.ID, foundUser.ID, "Found wrong user ID") // Should match user1
	assert.Equal(t, "FindMapAlice", foundUser.Name)
	require.NotNil(t, foundUser.Email)
	assert.Equal(t, email1, *foundUser.Email)
	assert.Equal(t, 40, foundUser.Age)
}

func TestDBFindFirst_ByMap_NotFound(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: Create some records (optional)
	email1 := "findmap_nf1@example.com"
	user1 := CreateTestUser{Name: "NotFoundMapAlice", Email: &email1, Age: 30}
	res1 := db.Create(ctx, &user1)
	require.NoError(t, res1.Error)

	// 2. Act: Find using map criteria that doesn't match
	var foundUser CreateTestUser
	nameCol, _ := model.GetFieldByDBName("user_name") // Get correct column name
	query := map[string]any{
		nameCol.DBName: "NoSuchUser",
	}
	findResult := db.FindFirst(ctx, &foundUser, query)

	// 3. Assert
	require.Error(t, findResult.Error, "FindFirst should return an error when record not found")
	assert.True(t, errors.Is(findResult.Error, sql.ErrNoRows), "Error should be sql.ErrNoRows")
	assert.Zero(t, foundUser.ID)
}

func TestDBFindFirst_InvalidConditionType(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)
	var foundUser CreateTestUser

	// Act: Call FindFirst with an unsupported condition type (e.g., int)
	findResult := db.FindFirst(ctx, &foundUser, 12345)

	// Assert
	require.Error(t, findResult.Error, "FindFirst should return error for invalid condition type")
	assert.Contains(t, findResult.Error.Error(), "unsupported condition type", "Error message mismatch")
}

func TestDBFindFirst_InvalidMapKey(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)
	var foundUser CreateTestUser

	// Act: Call FindFirst with a map containing a key that isn't a valid column
	query := map[string]any{"non_existent_column": "some_value"}
	findResult := db.FindFirst(ctx, &foundUser, query)

	// Assert
	require.Error(t, findResult.Error, "FindFirst should return error for invalid map key")
	assert.Contains(t, findResult.Error.Error(), "invalid column name 'non_existent_column'", "Error message mismatch")
}

// --- NEW Tests for DB.Updates ---

func TestDBUpdates_Success(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: Create a record
	initialEmail := "update_me@example.com"
	user := CreateTestUser{Name: "UpdateInitial", Email: &initialEmail, Age: 50}
	createResult := db.Create(ctx, &user)
	require.NoError(t, createResult.Error)
	require.True(t, user.ID > 0)

	// 2. Act: Update specific fields using a map with DB column names
	newName := "UpdatedName"
	newAge := 55
	// Get DB column names from parsed model
	nameCol, _ := model.GetFieldByDBName("user_name")
	ageCol, _ := model.GetFieldByDBName("age")
	updateData := map[string]any{
		nameCol.DBName: newName,
		ageCol.DBName:  newAge,
		// Email is NOT included in the update map
	}
	updateResult := db.Updates(ctx, &user, updateData) // Pass user struct (contains ID) and data map

	// 3. Assert Update Result
	require.NoError(t, updateResult.Error, "Updates returned an error")
	assert.EqualValues(t, 1, updateResult.RowsAffected, "Updates should affect 1 row")

	// 4. Assert: Verify using FindByID
	var updatedUser CreateTestUser
	findResult := db.FindByID(ctx, &updatedUser, user.ID)
	require.NoError(t, findResult.Error)

	assert.Equal(t, user.ID, updatedUser.ID)
	assert.Equal(t, newName, updatedUser.Name, "Name was not updated") // Check updated field
	assert.Equal(t, newAge, updatedUser.Age, "Age was not updated")    // Check updated field
	require.NotNil(t, updatedUser.Email, "Email should not have been cleared")
	assert.Equal(t, initialEmail, *updatedUser.Email, "Email should not have changed") // Check unchanged field
	assert.False(t, updatedUser.CreatedAt.IsZero(), "CreatedAt should still be set")   // Check unchanged field
}

func TestDBUpdates_UpdateNullableToNil(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: Create a record with non-nil email
	initialEmail := "non_nil_to_nil@example.com"
	user := CreateTestUser{Name: "UpdateToNil", Email: &initialEmail}
	createResult := db.Create(ctx, &user)
	require.NoError(t, createResult.Error)
	require.True(t, user.ID > 0)

	// 2. Act: Update email to nil using map
	emailCol, _ := model.GetFieldByDBName("email")
	updateData := map[string]any{
		emailCol.DBName: nil, // Set value to nil
	}
	updateResult := db.Updates(ctx, &user, updateData)

	// 3. Assert Update Result
	require.NoError(t, updateResult.Error)
	assert.EqualValues(t, 1, updateResult.RowsAffected)

	// 4. Assert: Verify using FindByID
	var updatedUser CreateTestUser
	findResult := db.FindByID(ctx, &updatedUser, user.ID)
	require.NoError(t, findResult.Error)
	assert.Nil(t, updatedUser.Email, "Email should now be nil in the struct")
}

func TestDBUpdates_UpdateNullableToValue(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: Create a record with nil email
	user := CreateTestUser{Name: "UpdateToVal", Email: nil} // Start with nil
	createResult := db.Create(ctx, &user)
	require.NoError(t, createResult.Error)
	require.True(t, user.ID > 0)

	// 2. Act: Update email to a non-nil value
	newEmail := "nil_to_val@example.com"
	emailCol, _ := model.GetFieldByDBName("email")
	updateData := map[string]any{
		emailCol.DBName: newEmail,
	}
	updateResult := db.Updates(ctx, &user, updateData)

	// 3. Assert Update Result
	require.NoError(t, updateResult.Error)
	assert.EqualValues(t, 1, updateResult.RowsAffected)

	// 4. Assert: Verify using FindByID
	var updatedUser CreateTestUser
	findResult := db.FindByID(ctx, &updatedUser, user.ID)
	require.NoError(t, findResult.Error)
	require.NotNil(t, updatedUser.Email, "Email should now be non-nil")
	assert.Equal(t, newEmail, *updatedUser.Email)
}

func TestDBUpdates_NotFound(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: A user struct with a non-existent ID
	nonExistentUser := CreateTestUser{ID: 999999}
	nameCol, _ := model.GetFieldByDBName("user_name")
	updateData := map[string]any{nameCol.DBName: "Doesn't Matter"}

	// 2. Act: Attempt to update
	updateResult := db.Updates(ctx, &nonExistentUser, updateData)

	// 3. Assert
	require.NoError(t, updateResult.Error, "Updates should not return SQL error for not found PK")
	assert.EqualValues(t, 0, updateResult.RowsAffected, "RowsAffected should be 0 for non-existent PK")
}

func TestDBUpdates_EmptyDataMap(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create a user
	user := CreateTestUser{Name: "NoUpdateData"}
	createResult := db.Create(ctx, &user)
	require.NoError(t, createResult.Error)
	require.True(t, user.ID > 0)

	// 2. Act: Attempt update with empty map
	updateData := map[string]any{}
	updateResult := db.Updates(ctx, &user, updateData)

	// 3. Assert: Expect an error because no fields were provided for update
	require.Error(t, updateResult.Error, "Updates should return error for empty data map")
	assert.Contains(t, updateResult.Error.Error(), "no valid fields provided for update")
}

func TestDBUpdates_InvalidColumnName(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create a user
	user := CreateTestUser{Name: "InvalidColUpdate"}
	createResult := db.Create(ctx, &user)
	require.NoError(t, createResult.Error)
	require.True(t, user.ID > 0)

	// 2. Act: Attempt update with invalid column name in map
	updateData := map[string]any{"invalid_column_name": "some_value"}
	updateResult := db.Updates(ctx, &user, updateData)

	// 3. Assert: Expect an error because column name is not valid
	require.Error(t, updateResult.Error, "Updates should return error for invalid column name")
	assert.Contains(t, updateResult.Error.Error(), "invalid column name 'invalid_column_name'")
}

func TestDBUpdates_AttemptUpdatePK(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: Create a user
	user := CreateTestUser{Name: "UpdatePKAttempt"}
	createResult := db.Create(ctx, &user)
	require.NoError(t, createResult.Error)
	require.True(t, user.ID > 0)
	originalID := user.ID

	// 2. Act: Attempt update including the PK column and another valid column
	pkCol, _ := model.GetFieldByDBName("id")
	nameCol, _ := model.GetFieldByDBName("user_name")
	updateData := map[string]any{
		pkCol.DBName:   originalID + 100, // Attempt to change PK
		nameCol.DBName: "NewNameForPKTest",
	}
	updateResult := db.Updates(ctx, &user, updateData)

	// 3. Assert: Should succeed, but only update Name, skipping PK. RowsAffected should be 1.
	require.NoError(t, updateResult.Error, "Updates should skip PK and succeed if other fields are valid")
	assert.EqualValues(t, 1, updateResult.RowsAffected, "Updates should affect 1 row even when skipping PK")

	// 4. Verify: Fetch and check that ID didn't change, but Name did
	var updatedUser CreateTestUser
	findResult := db.FindByID(ctx, &updatedUser, originalID)
	require.NoError(t, findResult.Error)
	assert.Equal(t, originalID, updatedUser.ID, "Primary Key should NOT have been updated")
	assert.Equal(t, "NewNameForPKTest", updatedUser.Name, "Name field should have been updated")
}

func TestDBUpdates_ZeroPKInput(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)

	// 1. Arrange: User struct with zero ID
	zeroPKUser := CreateTestUser{ID: 0, Name: "ZeroPK"}
	nameCol, _ := model.GetFieldByDBName("user_name")
	updateData := map[string]any{nameCol.DBName: "NewNameIrrelevant"}

	// 2. Act: Attempt update
	updateResult := db.Updates(ctx, &zeroPKUser, updateData)

	// 3. Assert: Expect error due to zero PK
	require.Error(t, updateResult.Error, "Updates should return error for zero PK input")
	assert.Contains(t, updateResult.Error.Error(), "primary key field")
	assert.Contains(t, updateResult.Error.Error(), "zero value")
}

// --- NEW Tests for DB.Find ---

func TestDBFind_ByStruct_SuccessMultiple(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create multiple records, some matching criteria
	email1 := "find_struct1@example.com"
	email2 := "find_struct2@example.com"
	email3 := "find_struct3@example.com"
	_ = db.Create(ctx, &CreateTestUser{Name: "FindStructAlice", Email: &email1, Age: 30})
	user2 := db.Create(ctx, &CreateTestUser{Name: "FindStructBob", Email: &email2, Age: 35})     // Match Age
	user3 := db.Create(ctx, &CreateTestUser{Name: "FindStructCharlie", Email: &email3, Age: 35}) // Match Age
	require.NoError(t, user2.Error)
	require.NoError(t, user3.Error)

	// 2. Act: Find using a struct pointer where Age is 35
	var foundUsers []CreateTestUser // Slice of values
	query := &CreateTestUser{Age: 35}
	findResult := db.Find(ctx, &foundUsers, query)

	// 3. Assert
	require.NoError(t, findResult.Error, "Find returned an error")
	assert.EqualValues(t, 2, findResult.RowsAffected, "Should find 2 records")
	require.Len(t, foundUsers, 2, "Slice should contain 2 users")

	// Verify the content (IDs might vary depending on insert order, check names/age)
	var names []string
	var ages []int
	for _, u := range foundUsers {
		names = append(names, u.Name)
		ages = append(ages, u.Age)
	}
	sort.Strings(names) // Sort for predictable comparison
	assert.Equal(t, []string{"FindStructBob", "FindStructCharlie"}, names, "Found incorrect names")
	assert.Equal(t, []int{35, 35}, ages, "Found incorrect ages")
}

func TestDBFind_ByMap_SuccessMultiple(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	email1 := "find_map1@example.com"
	email2 := "find_map2@example.com"
	email3 := "find_map3@example.com"
	user1 := db.Create(ctx, &CreateTestUser{Name: "FindMapAlice", Email: &email1, Age: 40})   // Match Age
	user2 := db.Create(ctx, &CreateTestUser{Name: "FindMapBob", Email: &email2, Age: 40})     // Match Age
	user3 := db.Create(ctx, &CreateTestUser{Name: "FindMapCharlie", Email: &email3, Age: 40}) // Match Age
	require.NoError(t, user1.Error)
	require.NoError(t, user2.Error)
	require.NoError(t, user3.Error)

	var foundUsers []*CreateTestUser // Slice of pointers
	ageCol, _ := model.GetFieldByDBName("age")
	query := map[string]any{ageCol.DBName: 40}
	findResult := db.Find(ctx, &foundUsers, query)

	require.NoError(t, findResult.Error, "Find returned an error")
	// *** CORRECTED ASSERTIONS: Expect 3 records ***
	assert.EqualValues(t, 3, findResult.RowsAffected, "Should find 3 records")
	require.Len(t, foundUsers, 3, "Slice should contain 3 users")
	// *** End Corrected Assertions ***

	var names []string
	var ages []int
	for _, uPtr := range foundUsers {
		require.NotNil(t, uPtr)
		names = append(names, uPtr.Name)
		ages = append(ages, uPtr.Age)
	}
	sort.Strings(names)
	// *** CORRECTED ASSERTIONS: Expect 3 names/ages ***
	assert.Equal(t, []string{"FindMapAlice", "FindMapBob", "FindMapCharlie"}, names, "Found incorrect names")
	assert.Equal(t, []int{40, 40, 40}, ages, "Found incorrect ages")
	// *** End Corrected Assertions ***
}

func TestDBFind_NoConditions(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Create multiple records
	email1 := "findall1@example.com"
	email2 := "findall2@example.com"
	res1 := db.Create(ctx, &CreateTestUser{Name: "FindAllAlice", Email: &email1, Age: 50})
	res2 := db.Create(ctx, &CreateTestUser{Name: "FindAllBob", Email: &email2, Age: 55})
	require.NoError(t, res1.Error)
	require.NoError(t, res2.Error)

	// 2. Act: Find with no conditions
	var foundUsers []CreateTestUser
	findResult := db.Find(ctx, &foundUsers) // No conds argument

	// 3. Assert
	require.NoError(t, findResult.Error, "Find returned an error")
	assert.EqualValues(t, 2, findResult.RowsAffected, "Should find all 2 records")
	require.Len(t, foundUsers, 2, "Slice should contain 2 users")
	// Could add more checks on the content if needed
}

func TestDBFind_NotFound(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	email1 := "find_none1@example.com"
	_ = db.Create(ctx, &CreateTestUser{Name: "FindNoneAlice", Email: &email1, Age: 60})
	var foundUsers []CreateTestUser
	nameCol, _ := model.GetFieldByDBName("user_name")
	query := map[string]any{nameCol.DBName: "NoSuchUser"}
	findResult := db.Find(ctx, &foundUsers, query)

	// *** Assertions for NotFound case ***
	require.NoError(t, findResult.Error, "Find should not return error when no records found")
	assert.EqualValues(t, 0, findResult.RowsAffected, "RowsAffected should be 0")
	assert.Empty(t, foundUsers, "Slice should be empty when no records found")
	// *** Removed incorrect assertions comparing content ***
}

func TestDBFind_InvalidDest(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Arrange: Invalid destinations
	var notAPointer []CreateTestUser
	var pointerToNonSlice *CreateTestUser
	var nilPointer *[]CreateTestUser = nil

	// 2. Act & Assert
	result1 := db.Find(ctx, notAPointer) // Not a pointer
	require.Error(t, result1.Error)
	assert.Contains(t, result1.Error.Error(), "destination must be a non-nil pointer to a slice")

	result2 := db.Find(ctx, &pointerToNonSlice) // Pointer to non-slice
	require.Error(t, result2.Error)
	assert.Contains(t, result2.Error.Error(), "destination must be a pointer to a slice")

	result3 := db.Find(ctx, nilPointer) // Nil pointer
	require.Error(t, result3.Error)
	assert.Contains(t, result3.Error.Error(), "destination must be a non-nil pointer to a slice")

	var sliceOfInts []int
	result4 := db.Find(ctx, &sliceOfInts) // Slice of non-structs
	require.Error(t, result4.Error)
	assert.Contains(t, result4.Error.Error(), "destination slice elements must be structs")
}

// TODO: Add test for FindFirst without conditions (if desired)
// TODO: Add test for FindFirst with query-by-example struct having zero values (should they be ignored?)
// TODO: Add more test cases for Create:
// - TestDBCreate_NilInput
// - TestDBCreate_NonPointerInput
// - TestDBCreate_NonStructPointerInput
// - TestDBCreate_UniqueConstraintViolation (e.g., insert same email twice)
// - TestDBCreate_NotNullConstraintViolation (e.g., try inserting with Name="" when column is NOT NULL)
// - TestDBCreate_DefaultValue (insert with Age=0, verify it becomes 20 in DB)

// --- NEW Tests for Transaction Support ---

func TestDBTransaction_Commit(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Begin Transaction
	tx, err := db.Begin(ctx)
	require.NoError(t, err, "db.Begin failed")
	require.NotNil(t, tx, "Begin returned nil transaction")

	// 2. Perform operations within transaction
	user1 := CreateTestUser{Name: "CommitUser1", Age: 1}
	user2 := CreateTestUser{Name: "CommitUser2", Age: 2}

	res1 := tx.Create(ctx, &user1)
	require.NoError(t, res1.Error, "tx.Create user1 failed")
	require.True(t, user1.ID > 0)

	res2 := tx.Create(ctx, &user2)
	require.NoError(t, res2.Error, "tx.Create user2 failed")
	require.True(t, user2.ID > 0)

	// 3. Commit Transaction
	err = tx.Commit()
	require.NoError(t, err, "tx.Commit failed")

	// 4. Verify changes outside transaction (using original db handle)
	var foundUser1 CreateTestUser
	findRes1 := db.FindByID(ctx, &foundUser1, user1.ID)
	require.NoError(t, findRes1.Error, "User1 not found after commit")
	assert.Equal(t, user1.Name, foundUser1.Name)

	var foundUser2 CreateTestUser
	findRes2 := db.FindByID(ctx, &foundUser2, user2.ID)
	require.NoError(t, findRes2.Error, "User2 not found after commit")
	assert.Equal(t, user2.Name, foundUser2.Name)
}

func TestDBTransaction_Rollback(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Create an initial record (to ensure rollback doesn't affect existing data)
	initialUser := CreateTestUser{Name: "InitialUser", Age: 99}
	initialRes := db.Create(ctx, &initialUser)
	require.NoError(t, initialRes.Error)
	require.True(t, initialUser.ID > 0)

	// 2. Begin Transaction
	tx, err := db.Begin(ctx)
	require.NoError(t, err, "db.Begin failed")
	require.NotNil(t, tx)

	// 3. Perform operations within transaction
	user1 := CreateTestUser{Name: "RollbackUser1", Age: 1}
	res1 := tx.Create(ctx, &user1) // Create within Tx
	require.NoError(t, res1.Error)
	require.True(t, user1.ID > 0)

	// Also try deleting the initial user within the transaction
	delRes := tx.Delete(ctx, &initialUser)
	require.NoError(t, delRes.Error)
	assert.EqualValues(t, 1, delRes.RowsAffected)

	// 4. Rollback Transaction
	err = tx.Rollback()
	require.NoError(t, err, "tx.Rollback failed")

	// 5. Verify changes were NOT persisted
	// a) User created within Tx should NOT exist
	var foundUser1 CreateTestUser
	findRes1 := db.FindByID(ctx, &foundUser1, user1.ID)
	require.Error(t, findRes1.Error, "User1 should not be found after rollback")
	assert.True(t, errors.Is(findRes1.Error, sql.ErrNoRows))

	// b) User deleted within Tx SHOULD still exist
	var foundInitialUser CreateTestUser
	findResInitial := db.FindByID(ctx, &foundInitialUser, initialUser.ID)
	require.NoError(t, findResInitial.Error, "InitialUser should still exist after rollback")
	assert.Equal(t, initialUser.Name, foundInitialUser.Name)
}

func TestDBTransaction_RollbackOnError(t *testing.T) {
	ctx, db, _ := setupIntegrationTest(t)

	// 1. Create a record to cause a unique constraint violation later
	uniqueEmail := fmt.Sprintf("unique_%d@example.com", time.Now().UnixNano())
	existingUser := CreateTestUser{Name: "ConstraintTester", Email: &uniqueEmail}
	res1 := db.Create(ctx, &existingUser)
	require.NoError(t, res1.Error)
	require.True(t, existingUser.ID > 0)

	// 2. Begin Transaction
	tx, err := db.Begin(ctx)
	require.NoError(t, err, "db.Begin failed")
	require.NotNil(t, tx)

	// 3. Perform operations: one valid, one invalid
	// a) Valid create
	userInTx := CreateTestUser{Name: "ShouldBeRolledBack", Age: 5}
	resTx1 := tx.Create(ctx, &userInTx)
	require.NoError(t, resTx1.Error, "tx.Create for userInTx failed unexpectedly")
	require.True(t, userInTx.ID > 0)

	// b) Invalid create (duplicate unique email)
	duplicateUser := CreateTestUser{Name: "DuplicateEmail", Email: &uniqueEmail}
	resTx2 := tx.Create(ctx, &duplicateUser)
	require.Error(t, resTx2.Error, "tx.Create for duplicateUser should have failed")
	// Note: Depending on the DB/driver, the transaction might already be implicitly rolled back here.

	// 4. Attempt Rollback (explicitly, even if implicit might have happened)
	err = tx.Rollback()
	require.NoError(t, err, "tx.Rollback failed (or failed after implicit rollback)")
	// Alternative: Attempt Commit and expect failure
	// commitErr := tx.Commit()
	// require.Error(t, commitErr, "tx.Commit should fail after constraint violation")

	// 5. Verify NEITHER operation within the transaction was persisted
	// a) User created successfully within Tx should NOT exist
	var foundUserInTx CreateTestUser
	findResTx1 := db.FindByID(ctx, &foundUserInTx, userInTx.ID)
	require.Error(t, findResTx1.Error, "User created within rolled-back Tx should not exist")
	assert.True(t, errors.Is(findResTx1.Error, sql.ErrNoRows))

	// b) The original user should still exist
	var foundExistingUser CreateTestUser
	findResExisting := db.FindByID(ctx, &foundExistingUser, existingUser.ID)
	require.NoError(t, findResExisting.Error, "Original user should still exist")
	assert.Equal(t, existingUser.Name, foundExistingUser.Name)
}

// TODO: Add tests for using TxOptions (Isolation levels) if needed.
// TODO: Add tests combining more operations (Create, Update, Delete) within a single Tx.

// Helper to create users for ordering/limiting tests
func createOrderTestUsers(ctx context.Context, t *testing.T, db *DB) []CreateTestUser {
	users := []CreateTestUser{
		{Name: "Charlie", Age: 35},
		{Name: "Alice", Age: 30},
		{Name: "Bob", Age: 40},
		{Name: "David", Age: 35}, // Duplicate age
	}
	for i := range users {
		res := db.Create(ctx, &users[i])
		require.NoError(t, res.Error, "Failed to create user %s for ordering test", users[i].Name)
		require.True(t, users[i].ID > 0)
	}
	return users
}

func TestDBFind_Order(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOrderTestUsers(ctx, t, db) // Create users

	nameCol, _ := model.GetFieldByDBName("user_name")
	ageCol, _ := model.GetFieldByDBName("age")

	// Test Order by Name ASC
	var usersByNameAsc []CreateTestUser
	resNameAsc := db.Find(ctx, &usersByNameAsc, Order(fmt.Sprintf("%s ASC", nameCol.DBName))) // Use DB column name
	require.NoError(t, resNameAsc.Error)
	require.Len(t, usersByNameAsc, 4)
	assert.Equal(t, "Alice", usersByNameAsc[0].Name)
	assert.Equal(t, "Bob", usersByNameAsc[1].Name)
	assert.Equal(t, "Charlie", usersByNameAsc[2].Name)
	assert.Equal(t, "David", usersByNameAsc[3].Name)

	// Test Order by Age DESC, Name ASC (for ties)
	var usersByAgeDesc []CreateTestUser
	resAgeDesc := db.Find(ctx, &usersByAgeDesc, Order(fmt.Sprintf("%s DESC, %s ASC", ageCol.DBName, nameCol.DBName)))
	require.NoError(t, resAgeDesc.Error)
	require.Len(t, usersByAgeDesc, 4)
	assert.Equal(t, "Bob", usersByAgeDesc[0].Name)     // Age 40
	assert.Equal(t, "Charlie", usersByAgeDesc[1].Name) // Age 35, C comes before D
	assert.Equal(t, "David", usersByAgeDesc[2].Name)   // Age 35
	assert.Equal(t, "Alice", usersByAgeDesc[3].Name)   // Age 30
}

func TestDBFind_Limit(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOrderTestUsers(ctx, t, db) // Create 4 users

	nameCol, _ := model.GetFieldByDBName("user_name")

	// Test Limit 2 (order by name for predictability)
	var usersLimit2 []CreateTestUser
	resLimit2 := db.Find(ctx, &usersLimit2, Order(fmt.Sprintf("%s ASC", nameCol.DBName)), Limit(2))
	require.NoError(t, resLimit2.Error)
	require.Len(t, usersLimit2, 2, "Should return exactly 2 users")
	assert.Equal(t, "Alice", usersLimit2[0].Name)
	assert.Equal(t, "Bob", usersLimit2[1].Name)

	// Test Limit 0 (should mean no limit, return all) - or should it return none? Let's assume all.
	var usersLimit0 []CreateTestUser
	resLimit0 := db.Find(ctx, &usersLimit0, Limit(0)) // Limit 0 or -1 means no limit
	require.NoError(t, resLimit0.Error)
	assert.Len(t, usersLimit0, 4, "Limit 0 should return all users")

	// Test Limit -1 (should mean no limit, return all)
	var usersLimitNeg1 []CreateTestUser
	resLimitNeg1 := db.Find(ctx, &usersLimitNeg1, Limit(-1))
	require.NoError(t, resLimitNeg1.Error)
	assert.Len(t, usersLimitNeg1, 4, "Limit -1 should return all users")

	// Test Limit greater than available records
	var usersLimit10 []CreateTestUser
	resLimit10 := db.Find(ctx, &usersLimit10, Limit(10))
	require.NoError(t, resLimit10.Error)
	assert.Len(t, usersLimit10, 4, "Limit 10 should return all 4 available users")
}

func TestDBFind_Offset(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOrderTestUsers(ctx, t, db) // Create 4 users

	nameCol, _ := model.GetFieldByDBName("user_name")

	// Test Offset 1 (order by name for predictability)
	var usersOffset1 []CreateTestUser
	resOffset1 := db.Find(ctx, &usersOffset1, Order(fmt.Sprintf("%s ASC", nameCol.DBName)), Offset(1))
	require.NoError(t, resOffset1.Error)
	require.Len(t, usersOffset1, 3, "Should return 3 users after skipping 1")
	assert.Equal(t, "Bob", usersOffset1[0].Name) // Skips Alice
	assert.Equal(t, "Charlie", usersOffset1[1].Name)
	assert.Equal(t, "David", usersOffset1[2].Name)

	// Test Offset 3
	var usersOffset3 []CreateTestUser
	resOffset3 := db.Find(ctx, &usersOffset3, Order(fmt.Sprintf("%s ASC", nameCol.DBName)), Offset(3))
	require.NoError(t, resOffset3.Error)
	require.Len(t, usersOffset3, 1, "Should return 1 user after skipping 3")
	assert.Equal(t, "David", usersOffset3[0].Name) // Skips Alice, Bob, Charlie

	// Test Offset greater than available records
	var usersOffset10 []CreateTestUser
	resOffset10 := db.Find(ctx, &usersOffset10, Offset(10))
	require.NoError(t, resOffset10.Error)
	assert.Empty(t, usersOffset10, "Offset 10 should return no users")
}

func TestDBFind_LimitOffsetOrder(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOrderTestUsers(ctx, t, db) // Create 4 users: A(30), B(40), C(35), D(35)

	nameCol, _ := model.GetFieldByDBName("user_name")
	ageCol, _ := model.GetFieldByDBName("age")

	// Order by Age DESC, Name ASC: B(40), C(35), D(35), A(30)
	// Offset 1: Skip B(40) -> Start from C(35)
	// Limit 2: Take C(35), D(35)
	var users []CreateTestUser
	res := db.Find(ctx, &users,
		Order(fmt.Sprintf("%s DESC, %s ASC", ageCol.DBName, nameCol.DBName)),
		Offset(1),
		Limit(2),
	)

	require.NoError(t, res.Error)
	require.Len(t, users, 2, "Should return exactly 2 users")
	assert.Equal(t, "Charlie", users[0].Name) // First user after offset
	assert.Equal(t, 35, users[0].Age)
	assert.Equal(t, "David", users[1].Name) // Second user after offset
	assert.Equal(t, 35, users[1].Age)
}

// --- NEW Tests for DB.Find with Query Operators ---

// Helper to create users for operator tests
func createOperatorTestUsers(ctx context.Context, t *testing.T, db *DB) map[string]CreateTestUser {
	users := map[string]CreateTestUser{
		"Alice": {Name: "Alice", Age: 30, Email: ptr("alice@example.com")},
		"Bob":   {Name: "Bob", Age: 40, Email: ptr("bob@example.com")},
		"Carol": {Name: "Carol", Age: 35, Email: ptr("carol@example.com")},
		"David": {Name: "David", Age: 35, Email: nil}, // Null email
	}
	for name, user := range users {
		u := user // Create local copy for pointer
		res := db.Create(ctx, &u)
		require.NoError(t, res.Error, "Failed to create user %s for operator test", name)
		users[name] = u // Update map with the user containing the ID
	}
	return users
}

// Helper function to create a pointer to a string
func ptr(s string) *string { return &s }

func TestDBFind_Operator_Comparison(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOperatorTestUsers(ctx, t, db)
	ageCol, _ := model.GetFieldByDBName("age")

	// Test >
	var usersGt []CreateTestUser
	resGt := db.Find(ctx, &usersGt, map[string]any{fmt.Sprintf("%s >", ageCol.DBName): 35})
	require.NoError(t, resGt.Error)
	require.Len(t, usersGt, 1)
	assert.Equal(t, "Bob", usersGt[0].Name)

	// Test >=
	var usersGte []CreateTestUser
	resGte := db.Find(ctx, &usersGte, map[string]any{fmt.Sprintf("%s >=", ageCol.DBName): 35}, Order("user_name ASC"))
	require.NoError(t, resGte.Error)
	require.Len(t, usersGte, 3)
	assert.Equal(t, "Bob", usersGte[0].Name)   // 40
	assert.Equal(t, "Carol", usersGte[1].Name) // 35
	assert.Equal(t, "David", usersGte[2].Name) // 35

	// Test <
	var usersLt []CreateTestUser
	resLt := db.Find(ctx, &usersLt, map[string]any{fmt.Sprintf("%s <", ageCol.DBName): 35})
	require.NoError(t, resLt.Error)
	require.Len(t, usersLt, 1)
	assert.Equal(t, "Alice", usersLt[0].Name)

	// Test <=
	var usersLte []CreateTestUser
	resLte := db.Find(ctx, &usersLte, map[string]any{fmt.Sprintf("%s <=", ageCol.DBName): 35}, Order("user_name ASC"))
	require.NoError(t, resLte.Error)
	require.Len(t, usersLte, 3)
	assert.Equal(t, "Alice", usersLte[0].Name) // 30
	assert.Equal(t, "Carol", usersLte[1].Name) // 35
	assert.Equal(t, "David", usersLte[2].Name) // 35

	// Test != or <>
	var usersNe []CreateTestUser
	resNe := db.Find(ctx, &usersNe, map[string]any{fmt.Sprintf("%s !=", ageCol.DBName): 35}, Order("user_name ASC"))
	require.NoError(t, resNe.Error)
	require.Len(t, usersNe, 2)
	assert.Equal(t, "Alice", usersNe[0].Name) // 30
	assert.Equal(t, "Bob", usersNe[1].Name)   // 40
}

func TestDBFind_Operator_Like(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOperatorTestUsers(ctx, t, db)
	nameCol, _ := model.GetFieldByDBName("user_name")

	// Test LIKE 'A%'
	var usersLikeA []CreateTestUser
	resLikeA := db.Find(ctx, &usersLikeA, map[string]any{fmt.Sprintf("%s LIKE", nameCol.DBName): "A%"})
	require.NoError(t, resLikeA.Error)
	require.Len(t, usersLikeA, 1)
	assert.Equal(t, "Alice", usersLikeA[0].Name)

	// Test LIKE '%o%'
	var usersLikeO []CreateTestUser
	resLikeO := db.Find(ctx, &usersLikeO, map[string]any{fmt.Sprintf("%s LIKE", nameCol.DBName): "%o%"}, Order("user_name ASC"))
	require.NoError(t, resLikeO.Error)
	require.Len(t, usersLikeO, 2)
	assert.Equal(t, "Bob", usersLikeO[0].Name)
	assert.Equal(t, "Carol", usersLikeO[1].Name)
}

func TestDBFind_Operator_In(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOperatorTestUsers(ctx, t, db)
	nameCol, _ := model.GetFieldByDBName("user_name")
	ageCol, _ := model.GetFieldByDBName("age")

	// Test IN Names
	var usersInNames []CreateTestUser
	namesToFind := []string{"Alice", "Carol", "Missing"}
	resInNames := db.Find(ctx, &usersInNames, map[string]any{fmt.Sprintf("%s IN", nameCol.DBName): namesToFind}, Order("user_name ASC"))
	require.NoError(t, resInNames.Error)
	require.Len(t, usersInNames, 2)
	assert.Equal(t, "Alice", usersInNames[0].Name)
	assert.Equal(t, "Carol", usersInNames[1].Name)

	// Test IN Ages
	var usersInAges []CreateTestUser
	agesToFind := []int{35, 40}
	resInAges := db.Find(ctx, &usersInAges, map[string]any{fmt.Sprintf("%s IN", ageCol.DBName): agesToFind}, Order("user_name ASC"))
	require.NoError(t, resInAges.Error)
	require.Len(t, usersInAges, 3)
	assert.Equal(t, "Bob", usersInAges[0].Name)   // 40
	assert.Equal(t, "Carol", usersInAges[1].Name) // 35
	assert.Equal(t, "David", usersInAges[2].Name) // 35

	// Test IN with empty slice (should find none)
	var usersInEmpty []CreateTestUser
	resInEmpty := db.Find(ctx, &usersInEmpty, map[string]any{fmt.Sprintf("%s IN", nameCol.DBName): []string{}})
	require.NoError(t, resInEmpty.Error)
	assert.Empty(t, usersInEmpty)
}

func TestDBFind_Operator_NotIn(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOperatorTestUsers(ctx, t, db)
	nameCol, _ := model.GetFieldByDBName("user_name")

	// Test NOT IN Names
	var usersNotInNames []CreateTestUser
	namesToExclude := []string{"Bob", "David"}
	resNotInNames := db.Find(ctx, &usersNotInNames, map[string]any{fmt.Sprintf("%s NOT IN", nameCol.DBName): namesToExclude}, Order("user_name ASC"))
	require.NoError(t, resNotInNames.Error)
	require.Len(t, usersNotInNames, 2)
	assert.Equal(t, "Alice", usersNotInNames[0].Name)
	assert.Equal(t, "Carol", usersNotInNames[1].Name)

	// Test NOT IN with empty slice (should find all)
	var usersNotInEmpty []CreateTestUser
	resNotInEmpty := db.Find(ctx, &usersNotInEmpty, map[string]any{fmt.Sprintf("%s NOT IN", nameCol.DBName): []string{}})
	require.NoError(t, resNotInEmpty.Error)
	assert.Len(t, usersNotInEmpty, 4)
}

func TestDBFind_Operator_IsNull(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOperatorTestUsers(ctx, t, db) // David has NULL email
	emailCol, _ := model.GetFieldByDBName("email")

	// Test IS NULL
	var usersNullEmail []CreateTestUser
	// Value for IS NULL doesn't matter, presence of key is enough
	resIsNull := db.Find(ctx, &usersNullEmail, map[string]any{fmt.Sprintf("%s IS NULL", emailCol.DBName): true})
	require.NoError(t, resIsNull.Error)
	require.Len(t, usersNullEmail, 1)
	assert.Equal(t, "David", usersNullEmail[0].Name)
	assert.Nil(t, usersNullEmail[0].Email)
}

func TestDBFind_Operator_IsNotNull(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOperatorTestUsers(ctx, t, db) // Alice, Bob, Carol have non-NULL email
	emailCol, _ := model.GetFieldByDBName("email")

	// Test IS NOT NULL
	var usersNotNullEmail []CreateTestUser
	resIsNotNull := db.Find(ctx, &usersNotNullEmail, map[string]any{fmt.Sprintf("%s IS NOT NULL", emailCol.DBName): true}, Order("user_name ASC"))
	require.NoError(t, resIsNotNull.Error)
	require.Len(t, usersNotNullEmail, 3)
	assert.Equal(t, "Alice", usersNotNullEmail[0].Name)
	assert.Equal(t, "Bob", usersNotNullEmail[1].Name)
	assert.Equal(t, "Carol", usersNotNullEmail[2].Name)
	assert.NotNil(t, usersNotNullEmail[0].Email)
}

func TestDBFind_Operator_Multiple(t *testing.T) {
	ctx, db, model := setupIntegrationTest(t)
	_ = createOperatorTestUsers(ctx, t, db)
	nameCol, _ := model.GetFieldByDBName("user_name")
	ageCol, _ := model.GetFieldByDBName("age")
	emailCol, _ := model.GetFieldByDBName("email")

	// Find users where age >= 35 AND email IS NOT NULL
	var users []CreateTestUser
	query := map[string]any{
		fmt.Sprintf("%s >=", ageCol.DBName):            35,
		fmt.Sprintf("%s IS NOT NULL", emailCol.DBName): true, // Find non-null emails
	}
	res := db.Find(ctx, &users, query, Order(fmt.Sprintf("%s ASC", nameCol.DBName)))

	require.NoError(t, res.Error)
	require.Len(t, users, 2) // Bob (40), Carol (35)
	assert.Equal(t, "Bob", users[0].Name)
	assert.Equal(t, "Carol", users[1].Name)
}
