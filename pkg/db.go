// pkg/typegorm/db.go
package typegorm

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings" // For SQL builder
	"time"

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
	result := &Result{}

	// 1. Validate input & Get Reflect Value/Type
	reflectValue := reflect.ValueOf(value)
	if reflectValue.Kind() != reflect.Pointer || reflectValue.IsNil() {
		result.Error = fmt.Errorf("input value must be a non-nil pointer to a struct, got %T", value)
		return result
	}
	structValue := reflectValue.Elem() // The struct instance itself
	if structValue.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("input value must be a pointer to a struct, got pointer to %s", structValue.Kind())
		return result
	}
	structType := structValue.Type()

	// 2. Parse Schema
	model, err := db.GetModel(value) // Use GetModel which uses cache
	if err != nil {
		result.Error = fmt.Errorf("failed to parse schema for type %s: %w", structType.Name(), err)
		return result
	}

	// 3. Build INSERT statement parts
	var columns []string
	var placeholders []string
	var args []any
	tableName := model.TableName
	dialect := db.source.Dialect()

	// Iterate through parsed fields to build the INSERT
	for _, field := range model.Fields {
		if field.IsIgnored {
			continue
		} // Skip ignored fields

		fieldValue := structValue.FieldByName(field.GoName)
		if !fieldValue.IsValid() {
			continue
		} // Skip if field somehow invalid

		// --- Skip columns that should use DB defaults ---
		// a) Skip auto-increment PKs if zero
		if field.IsPrimaryKey && field.AutoIncrement && fieldValue.IsZero() {
			fmt.Printf("Skipping auto-increment PK field: %s\n", field.GoName)
			continue
		}
		// b) Skip conventional timestamp fields if zero/nil to allow DB defaults
		if field.GoName == "CreatedAt" || field.GoName == "UpdatedAt" {
			isZeroTime := false
			if fieldValue.Kind() == reflect.Struct && fieldValue.Type() == reflect.TypeOf(time.Time{}) {
				isZeroTime = fieldValue.Interface().(time.Time).IsZero()
			} else if fieldValue.Kind() == reflect.Pointer && fieldValue.Type().Elem() == reflect.TypeOf(time.Time{}) {
				isZeroTime = fieldValue.IsNil() // Consider nil pointer as zero for skipping
				if !isZeroTime {
					// Also check if it's a pointer to a zero time
					if tPtr, ok := fieldValue.Interface().(*time.Time); ok && tPtr != nil && tPtr.IsZero() {
						isZeroTime = true
					}
				}
			}
			if isZeroTime {
				fmt.Printf("Skipping zero/nil timestamp field: %s\n", field.GoName)
				continue // Skip this field, let DB handle default
			}
		}
		// --- End skipping columns ---

		// Add column, placeholder, and the actual value from the struct
		columns = append(columns, dialect.Quote(field.DBName))
		placeholders = append(placeholders, dialect.BindVar(len(args)+1))
		args = append(args, fieldValue.Interface())
	}

	if len(columns) == 0 {
		result.Error = fmt.Errorf("no columns available for insert in type %s", structType.Name())
		return result
	}

	// Construct the SQL query string
	sqlQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		dialect.Quote(tableName),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	// 4. Execute SQL
	fmt.Printf("Executing SQL: %s | Args: %v\n", sqlQuery, args) // Debug log
	sqlResult, err := db.source.Exec(ctx, sqlQuery, args...)
	if err != nil {
		result.Error = fmt.Errorf("failed to execute insert for %s: %w", structType.Name(), err)
		return result
	}

	// 5. Populate Result object (RowsAffected, LastInsertID)
	if affected, errAff := sqlResult.RowsAffected(); errAff == nil {
		result.RowsAffected = affected
	} else {
		fmt.Printf("Warning: could not get RowsAffected after insert: %v\n", errAff)
	}

	// Handle setting AutoIncrement ID back onto the input struct
	var pkField *schema.Field = nil
	if len(model.PrimaryKeys) == 1 && model.PrimaryKeys[0].AutoIncrement {
		pkField = model.PrimaryKeys[0] // Get the single auto-inc PK field
		if lastID, errID := sqlResult.LastInsertId(); errID == nil {
			result.LastInsertID = lastID
			pkValueField := structValue.FieldByName(pkField.GoName)
			if pkValueField.IsValid() && pkValueField.CanSet() {
				// Convert lastID to the appropriate type and set it
				targetType := pkValueField.Type()
				targetValue := reflect.ValueOf(lastID)
				if targetType.Kind() != reflect.Int64 && targetValue.CanConvert(targetType) {
					pkValueField.Set(targetValue.Convert(targetType))
				} else if targetType.Kind() == reflect.Int64 {
					pkValueField.SetInt(lastID)
				} else {
					fmt.Printf("Warning: Cannot set auto-increment ID back on PK field %s (type mismatch: %s vs %s)\n", pkField.GoName, targetType, targetValue.Type())
				}
			} else {
				fmt.Printf("Warning: Cannot set auto-increment ID back on PK field %s (invalid or not settable)\n", pkField.GoName)
			}
		} else {
			fmt.Printf("Warning: could not get LastInsertId after insert (driver/DB may not support it): %v\n", errID)
		}
	}

	// 6. *** Re-fetch record to update fields set by DB (like CreatedAt) ***
	// We need the primary key value(s) to query
	pkValueArgs := []any{}
	pkWhereClauses := []string{}
	canRefetch := true
	for i, pk := range model.PrimaryKeys {
		var pkValue reflect.Value
		if pk == pkField && result.LastInsertID > 0 { // Use LastInsertID if available for the PK
			pkValue = reflect.ValueOf(result.LastInsertID) // Use the ID we just got
		} else { // Otherwise, use the value from the input struct
			pkValue = structValue.FieldByName(pk.GoName)
		}

		if !pkValue.IsValid() {
			fmt.Printf("Warning: Cannot build query to re-fetch created record: invalid primary key field %s\n", pk.GoName)
			canRefetch = false
			break
		}
		pkWhereClauses = append(pkWhereClauses, fmt.Sprintf("%s = %s", dialect.Quote(pk.DBName), dialect.BindVar(i+1)))
		pkValueArgs = append(pkValueArgs, pkValue.Interface())
	}

	if canRefetch && len(pkWhereClauses) > 0 {
		// Build SELECT statement for all non-ignored fields
		selectCols := []string{}
		scanDest := []any{}             // Slice to hold pointers for Scan
		scanFields := []*schema.Field{} // Keep track of fields being scanned

		for _, field := range model.Fields {
			if !field.IsIgnored {
				selectCols = append(selectCols, dialect.Quote(field.DBName))
				// Create a pointer to the field in the original input struct `value`
				fieldRef := structValue.FieldByName(field.GoName)
				if fieldRef.IsValid() && fieldRef.CanAddr() {
					scanDest = append(scanDest, fieldRef.Addr().Interface())
					scanFields = append(scanFields, field)
				} else {
					// Should not happen if struct is valid
					fmt.Printf("Warning: Cannot create scan destination for field %s\n", field.GoName)
					result.Error = fmt.Errorf("internal error preparing re-fetch scan for field %s", field.GoName)
					return result // Abort if we can't scan properly
				}
			}
		}

		if len(selectCols) > 0 {
			selectQuery := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
				strings.Join(selectCols, ", "),
				dialect.Quote(tableName),
				strings.Join(pkWhereClauses, " AND "),
			)

			// Execute SELECT query using QueryRow
			fmt.Printf("Re-fetching record with query: %s | Args: %v\n", selectQuery, pkValueArgs)
			rowScanner := db.source.QueryRow(ctx, selectQuery, pkValueArgs...)

			// Scan the result directly back into the fields of the original struct
			if scanErr := rowScanner.Scan(scanDest...); scanErr != nil {
				// Don't overwrite the original insert success, just warn
				fmt.Printf("Warning: Failed to re-fetch record after create to update default values: %v\n", scanErr)
				// If the error is sql.ErrNoRows, it's particularly strange after an insert
				if scanErr == sql.ErrNoRows {
					fmt.Println("Error: Record not found immediately after insert during re-fetch.")
				}
			} else {
				fmt.Println("Successfully re-fetched record after create.")
			}
		}
	} else if canRefetch { // Only warn if we could have refetched but didn't have PKs
		fmt.Println("Warning: Cannot re-fetch record after create without primary key information.")
	}

	return result // Contains error=nil if successful
}
