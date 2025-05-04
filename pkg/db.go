// pkg/typegorm/db.go
package typegorm

import (
	"context"
	"fmt"
	"reflect"
	"strings" // For SQL builder

	"github.com/chmenegatti/typegorm/pkg/config" // Needed if Open stays here
	"github.com/chmenegatti/typegorm/pkg/dialects/common"
	"github.com/chmenegatti/typegorm/pkg/schema"
)

// DB represents the main ORM database handle. Provides ORM methods.
type DB struct {
	source common.DataSource // The underlying connected DataSource (MySQL, Postgres, etc.)
	parser *schema.Parser    // The schema parser instance
	config config.Config     // Store original config for potential use
	// TODO: Add logger, context, etc.
}

// NewDB creates a new DB instance. Typically called via typegorm.Open.
// It requires a connected DataSource and a schema parser.
func NewDB(source common.DataSource, parser *schema.Parser, cfg config.Config) *DB {
	if source == nil {
		panic("cannot create DB with nil DataSource") // Or return error
	}
	if parser == nil {
		parser = schema.NewParser(nil) // Use default parser if none provided
	}
	return &DB{
		source: source,
		parser: parser,
		config: cfg,
	}
}

// Close closes the underlying database connection pool.
func (db *DB) Close() error {
	if db.source == nil {
		return fmt.Errorf("db source is nil, cannot close")
	}
	return db.source.Close()
}

// Ping checks the database connection.
func (db *DB) Ping(ctx context.Context) error {
	if db.source == nil {
		return fmt.Errorf("db source is nil, cannot ping")
	}
	return db.source.Ping(ctx)
}

// GetDataSource returns the underlying common.DataSource.
// Useful for executing raw SQL or accessing dialect-specific features if needed.
func (db *DB) GetDataSource() common.DataSource {
	return db.source
}

func (db *DB) GetModel(value any) (*schema.Model, error) {
	if db.parser == nil {
		return nil, fmt.Errorf("internal error: db instance has no schema parser")
	}
	return db.parser.Parse(value) // Delegate to the internal parser
}

// --- AutoMigrate Method ---

// AutoMigrate runs schema migrations for the given struct types.
// Currently, it only attempts to CREATE TABLE IF NOT EXISTS.
// It does NOT handle table alterations (dropping/adding/modifying columns/indexes).
func (db *DB) AutoMigrate(ctx context.Context, values ...any) error {
	dialect := db.source.Dialect()

	for _, value := range values {
		model, err := db.parser.Parse(value)
		if err != nil {
			return fmt.Errorf("automigrate: failed to parse schema for type %T: %w", value, err)
		}

		tableName := dialect.Quote(model.TableName)
		fmt.Printf("AutoMigrate: Ensuring table %s exists for model %s...\n", tableName, model.Name)

		var columnDefs []string
		var primaryKeyNames []string

		for _, field := range model.Fields {
			if field.IsIgnored {
				continue
			}

			// Get column type definition using the dialect's refined GetDataType
			colType, err := dialect.GetDataType(field)
			if err != nil {
				return fmt.Errorf("automigrate: failed to get data type for field %s.%s: %w", model.Name, field.GoName, err)
			}

			columnDefs = append(columnDefs, fmt.Sprintf("%s %s", dialect.Quote(field.DBName), colType))

			if field.IsPrimaryKey {
				primaryKeyNames = append(primaryKeyNames, dialect.Quote(field.DBName))
			}
			// TODO: Handle UNIQUE constraints defined directly via GetDataType? Or add separately?
		}

		if len(columnDefs) == 0 {
			fmt.Printf("AutoMigrate: Skipping model %s, no migratable fields found.\n", model.Name)
			continue
		}

		// Add composite primary key constraint if multiple PKs defined
		if len(primaryKeyNames) > 1 {
			// If more than one field is marked as PK, add a separate composite key constraint.
			// Assumes GetDataType does NOT add PRIMARY KEY inline in this composite case
			// (or we would need to modify GetDataType too). Let's assume GetDataType only adds PK inline for single PKs.
			pkConstraint := fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(primaryKeyNames, ", "))
			columnDefs = append(columnDefs, pkConstraint)
			fmt.Printf("AutoMigrate: Adding composite primary key constraint for %s.\n", model.Name)
		}
		// Assemble CREATE TABLE statement
		createTableSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s);",
			tableName,
			strings.Join(columnDefs, ", "),
		)

		// Execute CREATE TABLE statement
		fmt.Printf("AutoMigrate: Executing: %s\n", createTableSQL) // Log the SQL
		_, err = db.source.Exec(ctx, createTableSQL)
		if err != nil {
			return fmt.Errorf("automigrate: failed to create/ensure table %s for model %s: %w", tableName, model.Name, err)
		}

		// TODO: Index Creation - requires iterating model.Indexes and generating CREATE INDEX SQL
		// for _, index := range model.Indexes {
		//     // Generate CREATE (UNIQUE) INDEX sql using dialect
		//     // Execute index creation SQL
		// }

		fmt.Printf("AutoMigrate: Table %s ensured for model %s.\n", tableName, model.Name)
	} // end loop through values

	return nil
}

// *** IMPLEMENT Create Method ***
func (db *DB) Create(ctx context.Context, value any) *Result {
	result := &Result{} // Initialize result object

	// 1. Validate input
	reflectValue := reflect.ValueOf(value)
	if reflectValue.Kind() != reflect.Pointer || reflectValue.IsNil() {
		result.Error = fmt.Errorf("input value must be a non-nil pointer to a struct, got %T", value)
		return result
	}
	// Get the struct value itself (e.g., User from *User)
	structValue := reflectValue.Elem()
	if structValue.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("input value must be a pointer to a struct, got pointer to %s", structValue.Kind())
		return result
	}

	// 2. Parse Schema
	model, err := db.parser.Parse(value) // Pass original pointer or elem? Parse handles both.
	if err != nil {
		result.Error = fmt.Errorf("failed to parse schema for type %T: %w", value, err)
		return result
	}

	// 3. Build INSERT statement
	var columns []string
	var placeholders []string
	var args []any // Arguments for the SQL query

	tableName := model.TableName // Already quoted by dialect? No, quote here.
	dialect := db.source.Dialect()

	// Iterate through parsed fields
	for _, field := range model.Fields {
		if field.IsIgnored {
			continue
		} // Skip ignored fields

		// Skip auto-increment primary keys if the value is zero
		// (assumes zero value means "not set, let DB generate")
		if field.IsPrimaryKey && field.AutoIncrement {
			fieldValue := structValue.FieldByName(field.GoName)
			if fieldValue.IsValid() && fieldValue.IsZero() {
				continue // Skip this column in INSERT, DB will generate it
			}
		}

		// TODO: Handle read-only fields (e.g., CreatedAt updated by DB)

		// Get the value from the input struct
		fieldValue := structValue.FieldByName(field.GoName)
		if !fieldValue.IsValid() {
			result.Error = fmt.Errorf("internal error: invalid field value for %s", field.GoName)
			return result
		}

		columns = append(columns, dialect.Quote(field.DBName))
		placeholders = append(placeholders, dialect.BindVar(len(args)+1)) // Get placeholder (?, $1, etc.)
		args = append(args, fieldValue.Interface())                       // Add the actual value to args slice
	}

	if len(columns) == 0 {
		result.Error = fmt.Errorf("no columns available for insert in type %T", value)
		return result
	}

	// Construct the SQL query string
	sqlQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		dialect.Quote(tableName),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)
	// Maybe add RETURNING clause for dialects that support it (Postgres) to get ID?

	// 4. Execute SQL
	// fmt.Printf("Executing SQL: %s | Args: %v\n", sqlQuery, args) // Debug log
	sqlResult, err := db.source.Exec(ctx, sqlQuery, args...)
	if err != nil {
		result.Error = fmt.Errorf("failed to execute insert for %T: %w", value, err)
		return result
	}

	// 5. Populate Result object
	if affected, err := sqlResult.RowsAffected(); err == nil {
		result.RowsAffected = affected
	} else {
		fmt.Printf("Warning: could not get RowsAffected after insert: %v\n", err)
	}

	// 6. Handle AutoIncrement ID
	// Check if there's a single auto-increment PK
	var pkField *schema.Field
	pkCount := 0
	for _, f := range model.PrimaryKeys {
		if f.AutoIncrement {
			pkField = f
			pkCount++
		}
	}

	if pkCount == 1 { // Only try to get LastInsertId if there's one auto-increment PK
		if lastID, err := sqlResult.LastInsertId(); err == nil {
			result.LastInsertID = lastID
			// Set the ID back on the input struct value
			pkValueField := structValue.FieldByName(pkField.GoName)
			if pkValueField.IsValid() && pkValueField.CanSet() {
				switch pkValueField.Kind() {
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					pkValueField.SetInt(lastID)
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					// Be careful about potential overflow if lastID is negative (unlikely for PK)
					if lastID >= 0 {
						pkValueField.SetUint(uint64(lastID))
					} else {
						fmt.Printf("Warning: LastInsertId (%d) is negative, cannot set on unsigned PK field %s\n", lastID, pkField.GoName)
					}
				default:
					fmt.Printf("Warning: Cannot set auto-increment ID back on PK field %s (type %s)\n", pkField.GoName, pkValueField.Type())
				}
			} else {
				fmt.Printf("Warning: Cannot set auto-increment ID back on PK field %s (invalid or not settable)\n", pkField.GoName)
			}
		} else {
			fmt.Printf("Warning: could not get LastInsertId after insert (driver/DB may not support it): %v\n", err)
		}
	}

	return result // Contains error=nil if successful
}
