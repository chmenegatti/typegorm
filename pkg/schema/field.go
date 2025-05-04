// pkg/schema/field.go
package schema

import (
	"reflect"
)

// Field represents metadata about a Go struct field mapped to a database column.
type Field struct {
	// --- Struct Information ---
	StructField reflect.StructField // Original reflection info (includes Name, Type, Tag, Offset, etc.)
	GoName      string              // Field name in the Go struct (e.g., "ProductID")
	GoType      reflect.Type        // reflect.Type of the field (e.g., uint64, *string)

	// --- Database Mapping ---
	DBName        string  // Database column name (e.g., "product_id", "stock_keeping_unit")
	IsPrimaryKey  bool    // Is this field part of the primary key?
	IsIgnored     bool    // Should this field be ignored by the ORM (tag "-")?
	IsRequired    bool    // Does this field have a NOT NULL constraint (tag "not null")?
	Nullable      bool    // Can the DB column be NULL? (Inferred from pointer/sql.Null*, adjusted by "not null" tag)
	Unique        bool    // Does this field have a column-level UNIQUE constraint (tag "unique")?
	AutoIncrement bool    // Is this an auto-incrementing field (tag "autoIncrement")?
	DefaultValue  *string // SQL default value as a string literal (e.g., "'active'", "0", "CURRENT_TIMESTAMP")
	Size          int     // Size constraint (e.g., for VARCHAR) - parsed from size tag
	Precision     int     // Precision for decimal types - parsed from precision tag
	Scale         int     // Scale for decimal types - parsed from scale tag
	SQLType       string  // Explicit SQL data type override from tag (e.g., "VARCHAR(150)")

	// --- Indexing ---
	// Note: A field can potentially be part of multiple indexes. Storing the names here.

	IsIndex       bool // True if `index` tag was present (with or without name)
	IsUniqueIndex bool // True if `uniqueIndex` tag was present (with or without name)
	// The actual index definition (which fields belong to which index name)
	// might be better stored in the Model struct.
	IndexNames       []string // Names of non-unique indexes this field belongs to
	UniqueIndexNames []string // Names of unique indexes this field belongs to

	// --- Relationships (Future) ---
	// Relation *Relation // Details about the relationship if this field represents one

	// --- Internal ---
	Tags map[string]string // Optional: Store raw parsed key-value tags if needed later
}

// HasSQLTypeOverride checks if an explicit SQL type was set via tags.
func (f *Field) HasSQLTypeOverride() bool {
	return f.SQLType != ""
}

// IsNullable checks if the field allows NULL values in the database.
// Considers both the Go type and the "not null" tag.
func (f *Field) IsNullable() bool {
	return f.Nullable && !f.IsRequired
}
