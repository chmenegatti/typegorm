// pkg/schema/model.go
package schema

import (
	"reflect"
	"strings"
	"sync"
)

// --- Naming Strategy ---

// NamingStrategy defines methods for converting Go names to database names.
type NamingStrategy interface {
	TableName(structName string) string
	ColumnName(fieldName string) string
	// Add methods for IndexName, UniqueName, JoinTableName etc. if needed
}

// DefaultNamingStrategy provides snake_case naming.
type DefaultNamingStrategy struct{}

var defaultNamingStrategy NamingStrategy = DefaultNamingStrategy{} // Singleton

func (ns DefaultNamingStrategy) TableName(structName string) string {
	var output []rune
	for i, r := range structName {
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Check if previous char was not uppercase to avoid USERID -> u_s_e_r_i_d
			if len(output) > 0 && !(output[len(output)-1] >= 'A' && output[len(output)-1] <= 'Z') {
				output = append(output, '_')
			}
		}
		output = append(output, r)
	}
	// Basic pluralization: append 's', doesn't handle edge cases (y->ies, es)
	// Consider using an external library for better pluralization if needed.
	return strings.ToLower(string(output)) + "s"
}

func (ns DefaultNamingStrategy) ColumnName(fieldName string) string {
	var output []rune
	for i, r := range fieldName {
		if i > 0 && r >= 'A' && r <= 'Z' {
			if len(output) > 0 && !(output[len(output)-1] >= 'A' && output[len(output)-1] <= 'Z') {
				output = append(output, '_')
			}
		}
		output = append(output, r)
	}
	return strings.ToLower(string(output))
}

// --- Index Representation ---

// Index represents a database index definition.
type Index struct {
	Name     string   // Explicit name from tag (e.g., "idx_name") or generated
	IsUnique bool     // Is it a UNIQUE index?
	Fields   []*Field // Ordered list of fields included in the index
}

// --- Model ---

// Model represents the parsed schema of a Go struct for ORM mapping.
type Model struct {
	Name           string            // Name of the Go struct (e.g., "Product")
	Type           reflect.Type      // reflect.Type of the struct
	TableName      string            // Database table name (e.g., "products")
	Fields         []*Field          // Slice of all mapped fields (ordered as in struct)
	FieldsByName   map[string]*Field // Quick lookup by Go field name ("ProductID")
	FieldsByDBName map[string]*Field // Quick lookup by DB column name ("product_id")
	PrimaryKeys    []*Field          // Slice of primary key fields (usually one, but could be composite)
	Indexes        []*Index          // Slice of all defined indexes (unique and non-unique)

	// --- Relationships (Future) ---
	// Relations      []*Relation

	// These flags indicate if the model implements the corresponding hook interface.
	// Checked during parsing.
	HasBeforeCreate bool
	HasAfterCreate  bool
	HasBeforeUpdate bool
	HasAfterUpdate  bool
	HasBeforeDelete bool
	HasAfterDelete  bool
	HasAfterFind    bool
	// --- End Hook Flags ---

	// --- Internal ---
	instance       any            // Keep a zero-value instance for creating new objects (optional)
	mux            sync.RWMutex   // For thread-safe access if modified after parse (unlikely)
	NamingStrategy NamingStrategy // Naming strategy used during parsing
}

// Helper methods for Model

// GetField retrieves a field by its Go struct field name.
func (m *Model) GetField(goName string) (*Field, bool) {
	// m.mux.RLock() // Lock if model can be modified concurrently
	// defer m.mux.RUnlock()
	field, ok := m.FieldsByName[goName]
	return field, ok
}

// GetFieldByDBName retrieves a field by its database column name.
func (m *Model) GetFieldByDBName(dbName string) (*Field, bool) {
	// m.mux.RLock()
	// defer m.mux.RUnlock()
	field, ok := m.FieldsByDBName[dbName]
	return field, ok
}
