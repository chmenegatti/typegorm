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

// TODO: Add test for FindFirst without conditions (if desired)
// TODO: Add test for FindFirst with query-by-example struct having zero values (should they be ignored?)
// TODO: Add more test cases for Create:
// - TestDBCreate_NilInput
// - TestDBCreate_NonPointerInput
// - TestDBCreate_NonStructPointerInput
// - TestDBCreate_UniqueConstraintViolation (e.g., insert same email twice)
// - TestDBCreate_NotNullConstraintViolation (e.g., try inserting with Name="" when column is NOT NULL)
// - TestDBCreate_DefaultValue (insert with Age=0, verify it becomes 20 in DB)
