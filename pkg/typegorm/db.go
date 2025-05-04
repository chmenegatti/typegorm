// pkg/typegorm/db.go
package typegorm

import (
	"context"
	"database/sql"
	"errors"
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
		scanDest := []any{} // Slice to hold pointers for Scan
		// scanFields := []*schema.Field{} // Keep track of fields being scanned

		for _, field := range model.Fields {
			if !field.IsIgnored {
				selectCols = append(selectCols, dialect.Quote(field.DBName))
				// Create a pointer to the field in the original input struct `value`
				fieldRef := structValue.FieldByName(field.GoName)
				if fieldRef.IsValid() && fieldRef.CanAddr() {
					scanDest = append(scanDest, fieldRef.Addr().Interface())
					// scanFields = append(scanFields, field)
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

// FindByID finds the first record matching the given primary key value and scans it into dest.
// 'dest' must be a pointer to a struct.
// 'id' is the primary key value to search for. Assumes a single primary key column for now.
// Returns a Result object. Result.Error will be sql.ErrNoRows if the record is not found.
func (db *DB) FindByID(ctx context.Context, dest any, id any) *Result {
	result := &Result{}

	// 1. Validate dest input
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		result.Error = fmt.Errorf("destination must be a non-nil pointer to a struct, got %T", dest)
		return result
	}
	destElem := destValue.Elem() // The struct instance itself
	if destElem.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("destination must be a pointer to a struct, got pointer to %s", destElem.Kind())
		return result
	}
	destType := destElem.Type()

	// 2. Parse Schema for dest type
	model, err := db.GetModel(dest) // Use cache-enabled parser
	if err != nil {
		result.Error = fmt.Errorf("failed to parse schema for type %s: %w", destType.Name(), err)
		return result
	}

	// 3. Identify Primary Key Column (assuming single PK for now)
	if len(model.PrimaryKeys) != 1 {
		result.Error = fmt.Errorf("FindByID currently supports models with exactly one primary key, found %d for %s", len(model.PrimaryKeys), model.Name)
		return result
	}
	pkField := model.PrimaryKeys[0]

	// 4. Build SELECT SQL
	dialect := db.source.Dialect()
	selectCols := []string{}
	scanFields := []*schema.Field{} // Keep track of fields to scan into

	for _, field := range model.Fields {
		if !field.IsIgnored {
			selectCols = append(selectCols, dialect.Quote(field.DBName))
			scanFields = append(scanFields, field)
		}
	}

	if len(selectCols) == 0 {
		result.Error = fmt.Errorf("no selectable columns found for model %s", model.Name)
		return result
	}

	tableNameQuoted := dialect.Quote(model.TableName)
	pkColNameQuoted := dialect.Quote(pkField.DBName)
	// Use LIMIT 1 for safety, although QueryRow should handle it
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s LIMIT 1",
		strings.Join(selectCols, ", "),
		tableNameQuoted,
		pkColNameQuoted,
		dialect.BindVar(1), // Placeholder for the ID arg
	)

	// 5. Execute Query using QueryRow
	fmt.Printf("Executing SQL: %s | Args: [%v]\n", query, id) // Debug log
	rowScanner := db.source.QueryRow(ctx, query, id)

	// 6. Prepare Scan Destinations
	scanDest := make([]any, len(scanFields))
	for i, field := range scanFields {
		// Get a pointer to the corresponding field in the dest struct
		fieldValue := destElem.FieldByName(field.GoName)
		if !fieldValue.IsValid() {
			result.Error = fmt.Errorf("internal error: struct field %s not found in destination", field.GoName)
			return result
		}
		if !fieldValue.CanAddr() {
			result.Error = fmt.Errorf("internal error: struct field %s is not addressable", field.GoName)
			return result
		}
		scanDest[i] = fieldValue.Addr().Interface() // Get pointer to field
	}

	// 7. Scan the row into the destinations
	err = rowScanner.Scan(scanDest...)
	if err != nil {
		// Check specifically for ErrNoRows
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Printf("Record not found for ID %v in table %s\n", id, tableNameQuoted)
			result.Error = sql.ErrNoRows // Set standard error for not found
		} else {
			// Other database/scan error
			result.Error = fmt.Errorf("failed to scan result for model %s: %w", model.Name, err)
		}
		return result
	}

	// If scan succeeded, error is nil
	result.RowsAffected = 1 // QueryRow affects 1 row if found
	fmt.Printf("Successfully found and scanned record for ID %v into %s\n", id, destType.Name())
	return result
}

// Delete deletes a record based on the primary key found in the provided value.
// 'value' must be a pointer to a struct instance containing the primary key value(s).
// Returns a Result object; check Result.Error for issues and Result.RowsAffected
// (RowsAffected == 0 indicates the record was not found or not deleted).
func (db *DB) Delete(ctx context.Context, value any) *Result {
	result := &Result{}

	// 1. Validate input & Get Reflect Value/Type
	reflectValue := reflect.ValueOf(value)
	if reflectValue.Kind() != reflect.Pointer || reflectValue.IsNil() {
		result.Error = fmt.Errorf("input value must be a non-nil pointer to a struct, got %T", value)
		return result
	}
	structValue := reflectValue.Elem()
	if structValue.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("input value must be a pointer to a struct, got pointer to %s", structValue.Kind())
		return result
	}
	structType := structValue.Type()

	// 2. Parse Schema
	model, err := db.GetModel(value)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse schema for type %s: %w", structType.Name(), err)
		return result
	}

	// 3. Extract Primary Key values
	if len(model.PrimaryKeys) == 0 {
		result.Error = fmt.Errorf("cannot delete: model %s has no primary key defined", model.Name)
		return result
	}

	pkArgs := make([]any, 0, len(model.PrimaryKeys))
	pkWhereClauses := make([]string, 0, len(model.PrimaryKeys))
	dialect := db.source.Dialect()

	for i, pkField := range model.PrimaryKeys {
		pkValueField := structValue.FieldByName(pkField.GoName)
		if !pkValueField.IsValid() {
			result.Error = fmt.Errorf("internal error: primary key field %s not found in struct %s", pkField.GoName, model.Name)
			return result
		}
		// Check if the PK value is its zero value - we usually don't delete records with zero PKs.
		if pkValueField.IsZero() {
			result.Error = fmt.Errorf("cannot delete: primary key field %s has zero value", pkField.GoName)
			return result
		}
		pkArgs = append(pkArgs, pkValueField.Interface())
		pkWhereClauses = append(pkWhereClauses, fmt.Sprintf("%s = %s", dialect.Quote(pkField.DBName), dialect.BindVar(i+1)))
	}

	// 4. Build DELETE SQL
	tableNameQuoted := dialect.Quote(model.TableName)
	sqlQuery := fmt.Sprintf("DELETE FROM %s WHERE %s",
		tableNameQuoted,
		strings.Join(pkWhereClauses, " AND "),
	)

	// 5. Execute SQL
	fmt.Printf("Executing SQL: %s | Args: %v\n", sqlQuery, pkArgs) // Debug log
	sqlResult, err := db.source.Exec(ctx, sqlQuery, pkArgs...)
	if err != nil {
		result.Error = fmt.Errorf("failed to execute delete for %s: %w", model.Name, err)
		return result
	}

	// 6. Populate Result
	affected, err := sqlResult.RowsAffected()
	if err != nil {
		fmt.Printf("Warning: could not get RowsAffected after delete: %v\n", err)
		// Don't set result.Error here, the delete itself succeeded if err above was nil
	}
	result.RowsAffected = affected

	if affected == 0 {
		fmt.Printf("Warning: Delete executed but no rows affected (record with PK probably didn't exist).\n")
		// Optional: Set a specific "not found" error if desired, but RowsAffected=0 is often sufficient indication.
		// result.Error = ErrRecordNotFound // A custom error type
	} else {
		fmt.Printf("Successfully deleted %d record(s) for %s.\n", affected, model.Name)
	}

	return result // Error will be nil if execution succeeded
}

// --- NEW: FindFirst Method ---

// FindFirst finds the first record matching the given conditions and scans it into dest.
// 'dest' must be a pointer to a struct.
// 'conds' can be:
//   - A pointer to a struct (query-by-example, uses non-zero fields).
//   - A map[string]any (keys are DB column names).
//   - TODO: A string followed by args (raw WHERE clause).
//
// Returns a Result object. Result.Error will be sql.ErrNoRows if no record is found.
func (db *DB) FindFirst(ctx context.Context, dest any, conds ...any) *Result {
	result := &Result{}

	// 1. Validate dest input
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		result.Error = fmt.Errorf("destination must be a non-nil pointer to a struct, got %T", dest)
		return result
	}
	destElem := destValue.Elem()
	if destElem.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("destination must be a pointer to a struct, got pointer to %s", destElem.Kind())
		return result
	}
	destType := destElem.Type()

	// 2. Parse Schema for dest type
	model, err := db.GetModel(dest)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse schema for type %s: %w", destType.Name(), err)
		return result
	}

	// 3. Build WHERE clause and arguments based on conds
	dialect := db.source.Dialect()
	whereClauses := []string{}
	whereArgs := []any{}

	if len(conds) > 0 {
		// Simple condition handling for now: assumes first arg is struct ptr or map
		queryCond := conds[0]
		queryValue := reflect.ValueOf(queryCond)

		if queryValue.Kind() == reflect.Pointer && queryValue.Elem().Kind() == reflect.Struct {
			// Query-by-example (struct pointer)
			queryStruct := queryValue.Elem()
			for i := 0; i < queryStruct.NumField(); i++ {
				fieldValue := queryStruct.Field(i)
				// Only use exported, non-zero fields for conditions
				if fieldValue.IsValid() && !fieldValue.IsZero() {
					goFieldName := queryStruct.Type().Field(i).Name
					schemaField, ok := model.GetField(goFieldName)
					if !ok || schemaField.IsIgnored {
						continue // Skip fields not in the model or ignored
					}
					// Add condition: "column_name" = ?
					whereClauses = append(whereClauses, fmt.Sprintf("%s = %s",
						dialect.Quote(schemaField.DBName),
						dialect.BindVar(len(whereArgs)+1),
					))
					whereArgs = append(whereArgs, fieldValue.Interface())
				}
			}
		} else if queryValue.Kind() == reflect.Map {
			// Query by map[string]any (keys are DB column names)
			iter := queryValue.MapRange()
			for iter.Next() {
				key := iter.Key()
				value := iter.Value()
				if key.Kind() != reflect.String {
					result.Error = fmt.Errorf("map condition keys must be strings (column names), got %s", key.Kind())
					return result
				}
				dbColName := key.String()
				// Verify key is a valid DB column name for the model
				schemaField, ok := model.GetFieldByDBName(dbColName)
				if !ok {
					result.Error = fmt.Errorf("invalid column name '%s' in map condition for model %s", dbColName, model.Name)
					return result
				}
				if schemaField.IsIgnored {
					continue
				} // Should not happen if GetFieldByDBName worked

				whereClauses = append(whereClauses, fmt.Sprintf("%s = %s",
					dialect.Quote(dbColName),
					dialect.BindVar(len(whereArgs)+1),
				))
				whereArgs = append(whereArgs, value.Interface())
			}
		} else {
			// TODO: Handle raw WHERE string + args: if reflect.TypeOf(conds[0]).Kind() == reflect.String { ... }
			result.Error = fmt.Errorf("unsupported condition type: %T. Expecting struct pointer or map[string]any", queryCond)
			return result
		}
	} // End if len(conds) > 0

	// 4. Build SELECT SQL
	selectCols := []string{}
	scanFields := []*schema.Field{}
	for _, field := range model.Fields {
		if !field.IsIgnored {
			selectCols = append(selectCols, dialect.Quote(field.DBName))
			scanFields = append(scanFields, field)
		}
	}
	if len(selectCols) == 0 {
		result.Error = fmt.Errorf("no selectable columns found for model %s", model.Name)
		return result
	}

	tableNameQuoted := dialect.Quote(model.TableName)
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString("SELECT ")
	queryBuilder.WriteString(strings.Join(selectCols, ", "))
	queryBuilder.WriteString(" FROM ")
	queryBuilder.WriteString(tableNameQuoted)
	if len(whereClauses) > 0 {
		queryBuilder.WriteString(" WHERE ")
		queryBuilder.WriteString(strings.Join(whereClauses, " AND "))
	}
	// LIMIT 1 for FindFirst
	queryBuilder.WriteString(" LIMIT 1") // Add LIMIT clause

	sqlQuery := queryBuilder.String()

	// 5. Execute Query using QueryRow
	fmt.Printf("Executing SQL: %s | Args: %v\n", sqlQuery, whereArgs) // Debug log
	rowScanner := db.source.QueryRow(ctx, sqlQuery, whereArgs...)

	// 6. Prepare Scan Destinations
	scanDest := make([]any, len(scanFields))
	for i, field := range scanFields {
		fieldValue := destElem.FieldByName(field.GoName)
		if !fieldValue.IsValid() {
			result.Error = fmt.Errorf("internal error: struct field %s not found in destination", field.GoName)
			return result
		}
		if !fieldValue.CanAddr() {
			result.Error = fmt.Errorf("internal error: struct field %s is not addressable", field.GoName)
			return result
		}
		scanDest[i] = fieldValue.Addr().Interface() // Get pointer to field
	}

	// 7. Scan the row
	err = rowScanner.Scan(scanDest...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Printf("Record not found matching conditions for %s\n", model.Name)
			result.Error = sql.ErrNoRows // Use standard error
		} else {
			result.Error = fmt.Errorf("failed to scan result for model %s: %w", model.Name, err)
		}
		return result
	}

	result.RowsAffected = 1 // Found and scanned one row
	fmt.Printf("Successfully found and scanned first record into %s\n", destType.Name())
	return result
}

// --- NEW: Updates Method ---

// Updates updates specific fields of a record identified by the primary key in modelWithValue.
// 'modelWithValue' must be a pointer to a struct instance containing the primary key value(s).
// 'data' is a map[string]any where keys are DATABASE COLUMN NAMES and values are the new values.
// It only updates columns provided in the 'data' map.
// Returns a Result object. Check Result.Error and Result.RowsAffected.
// RowsAffected == 0 typically means the record was not found with the given PK.
func (db *DB) Updates(ctx context.Context, modelWithValue any, data map[string]any) *Result {
	result := &Result{}

	// 1. Validate input model & Get Reflect Value/Type
	reflectValue := reflect.ValueOf(modelWithValue)
	if reflectValue.Kind() != reflect.Pointer || reflectValue.IsNil() {
		result.Error = fmt.Errorf("modelWithValue must be a non-nil pointer to a struct, got %T", modelWithValue)
		return result
	}
	structValue := reflectValue.Elem()
	if structValue.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("modelWithValue must be a pointer to a struct, got pointer to %s", structValue.Kind())
		return result
	}
	structType := structValue.Type()

	// 2. Parse Schema
	model, err := db.GetModel(modelWithValue)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse schema for type %s: %w", structType.Name(), err)
		return result
	}

	// 3. Extract Primary Key values for WHERE clause
	if len(model.PrimaryKeys) == 0 {
		result.Error = fmt.Errorf("cannot update: model %s has no primary key defined", model.Name)
		return result
	}
	pkArgs := make([]any, 0, len(model.PrimaryKeys))
	pkWhereClauses := make([]string, 0, len(model.PrimaryKeys))
	dialect := db.source.Dialect()
	for i, pkField := range model.PrimaryKeys {
		pkValueField := structValue.FieldByName(pkField.GoName)
		if !pkValueField.IsValid() {
			result.Error = fmt.Errorf("internal error: primary key field %s not found in struct %s", pkField.GoName, model.Name)
			return result
		}
		if pkValueField.IsZero() {
			result.Error = fmt.Errorf("cannot update: primary key field %s has zero value", pkField.GoName)
			return result
		}
		pkArgs = append(pkArgs, pkValueField.Interface())
		pkWhereClauses = append(pkWhereClauses, fmt.Sprintf("%s = %s", dialect.Quote(pkField.DBName), dialect.BindVar(i+1))) // Placeholders start at 1 for WHERE
	}

	// 4. Build SET clause and collect arguments
	setClauses := []string{}
	setArgs := []any{}
	placeholderOffset := len(pkArgs) // Placeholders for SET start after PK args

	for dbColName, value := range data {
		// Validate column name exists in model and is updatable
		field, ok := model.GetFieldByDBName(dbColName)
		if !ok {
			result.Error = fmt.Errorf("invalid column name '%s' provided in update data for model %s", dbColName, model.Name)
			return result
		}
		if field.IsIgnored || field.IsPrimaryKey { // Don't allow updating PKs or ignored fields this way
			fmt.Printf("Warning: Skipping update for primary key or ignored field '%s'\n", dbColName)
			continue
		}
		// TODO: Add check for read-only fields (like CreatedAt) if needed

		setClauses = append(setClauses, fmt.Sprintf("%s = %s", dialect.Quote(dbColName), dialect.BindVar(placeholderOffset+len(setArgs)+1)))
		setArgs = append(setArgs, value)
	}

	// Check if there's anything to update
	if len(setClauses) == 0 {
		result.Error = fmt.Errorf("no valid fields provided for update")
		// Or, arguably, return success with 0 rows affected? Let's return error for now.
		return result
	}

	// 5. Build Full UPDATE SQL
	tableNameQuoted := dialect.Quote(model.TableName)
	sqlQuery := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		tableNameQuoted,
		strings.Join(setClauses, ", "),
		strings.Join(pkWhereClauses, " AND "),
	)

	// Combine SET arguments and WHERE arguments
	allArgs := append(setArgs, pkArgs...)

	// 6. Execute SQL
	fmt.Printf("Executing SQL: %s | Args: %v\n", sqlQuery, allArgs) // Debug log
	sqlResult, err := db.source.Exec(ctx, sqlQuery, allArgs...)
	if err != nil {
		result.Error = fmt.Errorf("failed to execute update for %s: %w", model.Name, err)
		return result
	}

	// 7. Populate Result
	affected, err := sqlResult.RowsAffected()
	if err != nil {
		fmt.Printf("Warning: could not get RowsAffected after update: %v\n", err)
	}
	result.RowsAffected = affected

	if affected == 0 {
		fmt.Printf("Warning: Update executed but no rows affected (record with PK might not exist or values were the same).\n")
	} else {
		fmt.Printf("Successfully updated %d record(s) for %s.\n", affected, model.Name)
		// TODO: Optionally re-fetch the record to update the input modelWithValue?
		// Similar logic to the re-fetch in Create.
	}

	return result // Error will be nil if execution succeeded
}

// --- NEW: Find Method ---

// Find retrieves a slice of records matching the given conditions and scans them into dest.
// 'dest' must be a pointer to a slice of structs (e.g., &[]User{}).
// 'conds' are the query conditions (struct pointer or map[string]any).
// Returns a Result object. Result.Error contains database/scan errors, but NOT sql.ErrNoRows.
func (db *DB) Find(ctx context.Context, dest any, conds ...any) *Result {
	result := &Result{}

	// 1. Validate dest input - MUST be pointer to slice
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		result.Error = fmt.Errorf("destination must be a non-nil pointer to a slice, got %T", dest)
		return result
	}
	sliceValue := destValue.Elem() // The slice variable itself
	if sliceValue.Kind() != reflect.Slice {
		result.Error = fmt.Errorf("destination must be a pointer to a slice, got pointer to %s", sliceValue.Kind())
		return result
	}

	// 2. Get Slice Element Type and Parse Schema
	elementType := sliceValue.Type().Elem() // Type of elements IN the slice (e.g., User or *User)
	elementIsPointer := (elementType.Kind() == reflect.Pointer)
	schemaType := elementType
	if schemaType.Kind() == reflect.Pointer {
		schemaType = schemaType.Elem() // Get the type the pointer points to (e.g., User from *User)
	}
	// Ensure the type we are parsing is actually a struct
	if schemaType.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("destination slice elements must be structs or pointers to structs, underlying type is %s", schemaType.Kind())
		return result
	}
	// Parse schema based on the underlying element struct type
	// Pass a pointer to a zero value of the struct type (e.g., &User{})
	model, err := db.GetModel(reflect.New(schemaType).Interface()) // Use schemaType
	if err != nil {
		// Use elementType in error message for clarity to user
		result.Error = fmt.Errorf("failed to parse schema for slice element type %s: %w", elementType.String(), err)
		return result
	}
	// 3. Build WHERE clause and arguments (reuse helper)
	dialect := db.source.Dialect()
	whereClauses, whereArgs, err := buildWhereClause(dialect, model, conds...)
	if err != nil {
		result.Error = err // Error from buildWhereClause
		return result
	}

	// 4. Build SELECT SQL (without LIMIT 1)
	selectCols := []string{}
	scanFields := []*schema.Field{} // Keep track of fields in SELECT order
	for _, field := range model.Fields {
		if !field.IsIgnored {
			selectCols = append(selectCols, dialect.Quote(field.DBName))
			scanFields = append(scanFields, field)
		}
	}
	if len(selectCols) == 0 {
		result.Error = fmt.Errorf("no selectable columns found for model %s", model.Name)
		return result
	}

	tableNameQuoted := dialect.Quote(model.TableName)
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString("SELECT ")
	queryBuilder.WriteString(strings.Join(selectCols, ", "))
	queryBuilder.WriteString(" FROM ")
	queryBuilder.WriteString(tableNameQuoted)
	if len(whereClauses) > 0 {
		queryBuilder.WriteString(" WHERE ")
		queryBuilder.WriteString(strings.Join(whereClauses, " AND "))
	}
	// TODO: Add ORDER BY clause later?
	sqlQuery := queryBuilder.String()

	// 5. Execute Query using Query()
	fmt.Printf("Executing SQL: %s | Args: %v\n", sqlQuery, whereArgs) // Debug log
	rows, err := db.source.Query(ctx, sqlQuery, whereArgs...)
	if err != nil {
		result.Error = fmt.Errorf("failed to execute find query for %s: %w", model.Name, err)
		return result
	}
	defer rows.Close() // Ensure rows are closed

	// 6. Iterate and Scan Rows into Slice
	// Reset the destination slice to empty before appending
	sliceValue.Set(reflect.MakeSlice(sliceValue.Type(), 0, 0))
	rowCount := 0

	for rows.Next() {
		rowCount++
		newElemInstance := reflect.New(schemaType).Elem() // Create User, not *User
		scanDest := make([]any, len(scanFields))
		// Prepare scan destinations pointing to fields of the new struct instance
		for i, field := range scanFields {
			fieldValue := newElemInstance.FieldByName(field.GoName) // Use FieldByName on the struct instance
			if !fieldValue.IsValid() {
				result.Error = fmt.Errorf("internal error: struct field %s not found in new element", field.GoName)
				return result
			}
			if !fieldValue.CanAddr() {
				result.Error = fmt.Errorf("internal error: struct field %s is not addressable", field.GoName)
				return result
			}
			scanDest[i] = fieldValue.Addr().Interface()
		}
		// Scan the current row into the new element instance
		if err := rows.Scan(scanDest...); err != nil {
			result.Error = fmt.Errorf("failed to scan row for model %s: %w", model.Name, err)
			return result
		}

		if elementIsPointer {
			// If dest is *[]*User, append the pointer newElemInstance.Addr()
			sliceValue.Set(reflect.Append(sliceValue, newElemInstance.Addr()))
		} else {
			// If dest is *[]User, append the value newElemInstance
			sliceValue.Set(reflect.Append(sliceValue, newElemInstance))
		}
	} // End rows.Next() loop

	// Check for errors during row iteration
	if err := rows.Err(); err != nil {
		result.Error = fmt.Errorf("error iterating query results for %s: %w", model.Name, err)
		return result
	}

	result.RowsAffected = int64(rowCount) // Store how many rows were found/scanned
	fmt.Printf("Successfully found and scanned %d record(s) into slice of %s\n", rowCount, elementType.Name())
	return result // Error is nil if query and scan succeeded
}

// --- NEW: Begin Method ---

// Begin starts a new database transaction.
// The provided context is used until the transaction is committed or rolled back.
// If the context is canceled, the sql package will roll back the transaction.
// The TxOptions provides control over isolation level and read-only status.
// If opts is nil, default transaction options will be used.
func (db *DB) Begin(ctx context.Context, opts ...*sql.TxOptions) (*Tx, error) {
	if db.source == nil {
		return nil, fmt.Errorf("db source is nil, cannot begin transaction")
	}

	var txOpt sql.TxOptions // Default options
	if len(opts) > 0 && opts[0] != nil {
		txOpt = *opts[0] // Use provided options if not nil
	}

	fmt.Println("Beginning transaction...")
	// Call the underlying DataSource's BeginTx method
	commonTx, err := db.source.BeginTx(ctx, txOpt) // Pass options as 'any'
	if err != nil {
		fmt.Printf("Failed to begin transaction: %v\n", err)
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	fmt.Println("Transaction begun successfully.")

	// Wrap the common.Tx in our typegorm.Tx struct
	tx := &Tx{
		source:  commonTx,
		parser:  db.parser,           // Share the parser
		dialect: db.source.Dialect(), // Get dialect from the source
	}
	return tx, nil
}

// --- Helper: buildWhereClause (extracted from FindFirst) ---

// buildWhereClause constructs the WHERE clause parts based on conditions.
// Returns slice of clauses, slice of args, and error.
// buildWhereClause constructs the WHERE clause parts based on conditions.
// Moved outside DB/Tx struct to be reusable.
func buildWhereClause(dialect common.Dialect, model *schema.Model, conds ...any) ([]string, []any, error) {
	whereClauses := []string{}
	whereArgs := []any{}

	if len(conds) > 0 {
		queryCond := conds[0]
		queryValue := reflect.ValueOf(queryCond)

		if queryValue.Kind() == reflect.Pointer && queryValue.Elem().Kind() == reflect.Struct {
			queryStruct := queryValue.Elem()
			for i := 0; i < queryStruct.NumField(); i++ {
				fieldValue := queryStruct.Field(i)
				if fieldValue.IsValid() && !fieldValue.IsZero() {
					goFieldName := queryStruct.Type().Field(i).Name
					schemaField, ok := model.GetField(goFieldName)
					if !ok || schemaField.IsIgnored {
						continue
					}
					whereClauses = append(whereClauses, fmt.Sprintf("%s = %s", dialect.Quote(schemaField.DBName), dialect.BindVar(len(whereArgs)+1)))
					whereArgs = append(whereArgs, fieldValue.Interface())
				}
			}
		} else if queryValue.Kind() == reflect.Map {
			iter := queryValue.MapRange()
			for iter.Next() {
				key := iter.Key()
				value := iter.Value()
				if key.Kind() != reflect.String {
					return nil, nil, fmt.Errorf("map condition keys must be strings (column names), got %s", key.Kind())
				}
				dbColName := key.String()
				schemaField, ok := model.GetFieldByDBName(dbColName)
				if !ok {
					return nil, nil, fmt.Errorf("invalid column name '%s' in map condition for model %s", dbColName, model.Name)
				}
				if schemaField.IsIgnored {
					continue
				}
				whereClauses = append(whereClauses, fmt.Sprintf("%s = %s", dialect.Quote(dbColName), dialect.BindVar(len(whereArgs)+1)))
				whereArgs = append(whereArgs, value.Interface())
			}
		} else {
			return nil, nil, fmt.Errorf("unsupported condition type: %T. Expecting struct pointer or map[string]any", queryCond)
		}
	}
	return whereClauses, whereArgs, nil
}
