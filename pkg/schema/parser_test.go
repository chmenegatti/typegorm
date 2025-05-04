// pkg/schema/parser_test.go
package schema

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test Structs ---

type BasicModel struct {
	ID        uint   `typegorm:"primaryKey;autoIncrement"`
	Name      string `typegorm:"size:100;not null"`
	Value     float64
	CreatedAt time.Time
}

type TaggedModel struct {
	UUID       string     `typegorm:"primaryKey;column:tagged_uuid;type:varchar(36)"`
	Email      *string    `typegorm:"column:user_email;unique;size:255"` // Nullable string
	Status     string     `typegorm:"type:varchar(20);default:'pending';not null"`
	Notes      string     `typegorm:"type:text;null"`           // Explicitly nullable text
	Data       []byte     `typegorm:"type:blob"`                // Byteslice
	IgnoreMe   string     `typegorm:"-"`                        // Ignored field
	MaybeTime  *time.Time `typegorm:"column:maybe_time"`        // Nullable time
	CreatedBy  string     `typegorm:"index:idx_creator"`        // Named index
	UpdatedBy  string     `typegorm:"index:idx_creator"`        // Part of composite index
	InternalID int        `typegorm:"uniqueIndex:uix_internal"` // Named unique index
}

type InvalidModelDuplicateDBName struct {
	FieldA string `typegorm:"column:the_name"`
	FieldB int    `typegorm:"column:the_name"`
}

// --- Test Cases ---

func TestParse_BasicModel(t *testing.T) {
	parser := NewParser(nil) // Use default naming strategy
	model, err := parser.Parse(&BasicModel{})

	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Model attributes
	assert.Equal(t, "BasicModel", model.Name)
	assert.Equal(t, "basic_models", model.TableName) // Default snake_case + 's'
	assert.Len(t, model.Fields, 4, "Should have 4 fields")
	require.Len(t, model.PrimaryKeys, 1, "Should have 1 primary key field")
	assert.Equal(t, "ID", model.PrimaryKeys[0].GoName)
	assert.Empty(t, model.Indexes, "Should have no explicit indexes") // No index tags

	// Test ID field
	idField, ok := model.GetField("ID")
	require.True(t, ok)
	assert.Equal(t, "id", idField.DBName)
	assert.True(t, idField.IsPrimaryKey)
	assert.True(t, idField.AutoIncrement)
	assert.False(t, idField.IsNullable(), "Primary keys usually non-nullable") // Check IsNullable method
	assert.False(t, idField.Nullable, "Go type uint isn't nullable")
	assert.True(t, idField.IsRequired, "PK should implicitly be required/not null") // Check IsRequired flag
	assert.Equal(t, reflect.Uint, idField.GoType.Kind())

	// Test Name field
	nameField, ok := model.GetField("Name")
	require.True(t, ok)
	assert.Equal(t, "name", nameField.DBName)
	assert.False(t, nameField.IsPrimaryKey)
	assert.False(t, nameField.AutoIncrement)
	assert.Equal(t, 100, nameField.Size)
	assert.True(t, nameField.IsRequired, "Should have 'not null' from tag")
	assert.False(t, nameField.IsNullable(), "Should not be nullable due to 'not null' tag")
	assert.Equal(t, reflect.String, nameField.GoType.Kind())

	// Test Value field (no tags)
	valueField, ok := model.GetField("Value")
	require.True(t, ok)
	assert.Equal(t, "value", valueField.DBName)
	assert.False(t, valueField.IsPrimaryKey)
	assert.True(t, valueField.IsRequired, "Basic value types (float64) should default to required (NOT NULL)")
	assert.False(t, valueField.IsNullable(), "Basic value types (float64) should default to NOT NULL")
	assert.Equal(t, reflect.Float64, valueField.GoType.Kind())

	// Test CreatedAt field (no tags)
	createdAtField, ok := model.GetField("CreatedAt")
	require.True(t, ok)
	assert.Equal(t, "created_at", createdAtField.DBName)
	assert.False(t, createdAtField.IsPrimaryKey)
	assert.Equal(t, reflect.Struct, createdAtField.GoType.Kind())
	assert.Equal(t, "Time", createdAtField.GoType.Name())
	assert.True(t, createdAtField.IsRequired, "time.Time should default to required (NOT NULL)")
	assert.False(t, createdAtField.IsNullable(), "time.Time should default to NOT NULL")
}

func TestParse_TaggedModel(t *testing.T) {
	parser := NewParser(nil)
	model, err := parser.Parse(&TaggedModel{})

	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Model attributes
	assert.Equal(t, "tagged_models", model.TableName)
	assert.Len(t, model.Fields, 9, "Should have 9 mapped fields (IgnoreMe is ignored)") // Corrected count
	require.Len(t, model.PrimaryKeys, 1, "Should have 1 PK")
	assert.Equal(t, "UUID", model.PrimaryKeys[0].GoName)
	require.Len(t, model.Indexes, 3, "Should have 3 indexes defined (idx_creator, uix_internal, default for Email)") // Corrected count

	// Test UUID field (PK, column, type)
	uuidField, ok := model.GetField("UUID")
	require.True(t, ok)
	assert.True(t, uuidField.IsPrimaryKey)
	assert.Equal(t, "tagged_uuid", uuidField.DBName)
	assert.Equal(t, "varchar(36)", uuidField.SQLType) // Explicit type override
	assert.False(t, uuidField.IsNullable())

	// Test Email field (column, unique, size, pointer)
	emailField, ok := model.GetField("Email")
	require.True(t, ok)
	assert.Equal(t, "user_email", emailField.DBName)
	assert.True(t, emailField.Unique, "'unique' tag")
	assert.Equal(t, 255, emailField.Size)
	assert.True(t, emailField.Nullable, "Go type *string is nullable")
	assert.False(t, emailField.IsRequired, "No 'not null' tag")
	assert.True(t, emailField.IsNullable(), "Pointer type should be nullable in DB")
	assert.Equal(t, reflect.Pointer, emailField.GoType.Kind()) // It's a pointer

	// Test Status field (type, default, not null)
	statusField, ok := model.GetField("Status")
	require.True(t, ok)
	assert.Equal(t, "status", statusField.DBName)
	assert.Equal(t, "varchar(20)", statusField.SQLType)
	require.NotNil(t, statusField.DefaultValue)
	assert.Equal(t, "'pending'", *statusField.DefaultValue) // Default value is stored as string
	assert.True(t, statusField.IsRequired)
	assert.False(t, statusField.IsNullable())

	// Test Notes field (type, null tag)
	notesField, ok := model.GetField("Notes")
	require.True(t, ok)
	assert.Equal(t, "notes", notesField.DBName)
	assert.Equal(t, "text", notesField.SQLType)
	assert.True(t, notesField.Nullable, "Explicitly marked as nullable via tag")
	assert.False(t, notesField.IsRequired)
	assert.True(t, notesField.IsNullable())

	// Test Data field (type)
	dataField, ok := model.GetField("Data")
	require.True(t, ok)
	assert.Equal(t, "data", dataField.DBName)
	assert.Equal(t, "blob", dataField.SQLType)
	assert.False(t, dataField.IsRequired)
	assert.True(t, dataField.Nullable, "Slice type is nullable")
	assert.True(t, dataField.IsNullable())

	// Test IgnoreMe field
	_, ok = model.GetField("IgnoreMe")
	assert.False(t, ok, "IgnoreMe field should not be present in parsed fields")

	// Test MaybeTime field (column, pointer)
	maybeTimeField, ok := model.GetField("MaybeTime")
	require.True(t, ok)
	assert.Equal(t, "maybe_time", maybeTimeField.DBName)
	assert.True(t, maybeTimeField.Nullable, "*time.Time is nullable")
	assert.True(t, maybeTimeField.IsNullable())

	// Test Indexes
	idxCreatorFound := false
	uixInternalFound := false
	for _, idx := range model.Indexes {
		if idx.Name == "idx_creator" {
			idxCreatorFound = true
			assert.False(t, idx.IsUnique)
			require.Len(t, idx.Fields, 2, "idx_creator should have 2 fields")
			assert.Equal(t, "CreatedBy", idx.Fields[0].GoName) // Assuming sorted by GoName
			assert.Equal(t, "UpdatedBy", idx.Fields[1].GoName)
		}
		if idx.Name == "uix_internal" {
			uixInternalFound = true
			assert.True(t, idx.IsUnique)
			require.Len(t, idx.Fields, 1, "uix_internal should have 1 field")
			assert.Equal(t, "InternalID", idx.Fields[0].GoName)
		}
	}
	assert.True(t, idxCreatorFound, "Index 'idx_creator' not found")
	assert.True(t, uixInternalFound, "Unique Index 'uix_internal' not found")

}

func TestParse_Error_NonStruct(t *testing.T) {
	parser := NewParser(nil)
	_, err := parser.Parse(123) // Pass an int
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input must be a struct instance or pointer to struct")
}

func TestParse_Error_Nil(t *testing.T) {
	parser := NewParser(nil)
	_, err := parser.Parse(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot parse nil value")
}

func TestParse_Error_DuplicateDBName(t *testing.T) {
	parser := NewParser(nil)
	_, err := parser.Parse(&InvalidModelDuplicateDBName{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate DB column name 'the_name' detected")
}

func TestParse_Cache(t *testing.T) {
	parser := NewParser(nil)
	model1, err1 := parser.Parse(&BasicModel{})
	model2, err2 := parser.Parse(&BasicModel{}) // Parse same type again

	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NotNil(t, model1)
	require.NotNil(t, model2)

	// Check if the pointers are the same (indicating cache hit)
	assert.Same(t, model1, model2, "Parsing the same struct type should return cached instance")
}

// TODO: Add tests for:
// - More complex tags (precision, scale, default variations)
// - sql.Null* types
// - Embedded structs
// - Tag parsing errors (e.g., invalid size)
// - Custom naming strategy
