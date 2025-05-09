package typegorm

import (
	"context"
	"database/sql" // Need sql for TxOptions
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/chmenegatti/typegorm/pkg/dialects/common"
	"github.com/chmenegatti/typegorm/pkg/hooks"
	"github.com/chmenegatti/typegorm/pkg/schema"
)

// Tx represents an active database transaction.
// It provides ORM methods that operate within this transaction.
type Tx struct {
	source  common.Tx      // The underlying transaction object from the DataSource
	parser  *schema.Parser // Schema parser (inherited from DB)
	dialect common.Dialect // Dialect (inherited from DB)
	// We might need context or config here later?
}

// Commit commits the transaction.
func (tx *Tx) Commit() error {
	if tx.source == nil {
		return fmt.Errorf("transaction source is nil, cannot commit")
	}
	fmt.Println("Committing transaction...")
	err := tx.source.Commit()
	if err == nil {
		fmt.Println("Transaction committed successfully.")
	} else {
		fmt.Printf("Transaction commit failed: %v\n", err)
	}
	return err
}

// Rollback aborts the transaction.
func (tx *Tx) Rollback() error {
	if tx.source == nil {
		return fmt.Errorf("transaction source is nil, cannot rollback")
	}
	fmt.Println("Rolling back transaction...")
	err := tx.source.Rollback()
	// According to database/sql docs, Rollback error should be checked but often
	// indicates the tx was already rolled back or committed.
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		fmt.Printf("Transaction rollback failed: %v\n", err)
		return err // Return significant errors
	}
	if err == nil {
		fmt.Println("Transaction rolled back successfully.")
	} else {
		fmt.Printf("Transaction rollback finished (original error: %v).\n", err)
	}
	return nil // Typically return nil unless Rollback itself caused a new error
}

// Helper function to call hook methods using reflection
// Handles both value and pointer receivers.
func callHook(ctx context.Context, dbContext hooks.ContextDB, methodValue reflect.Value, instanceValue reflect.Value) error {

	// Check if method expects pointer receiver and instance is not addressable
	// This check might be overly complex depending on how Implements was checked.
	// If Implements checked both value and pointer, we might just need to ensure we call on the right one.
	// Let's try calling on Addr() first if possible, then on value.

	var callArgs = []reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(dbContext)}
	var results []reflect.Value

	// Try calling on pointer receiver first if possible
	if instanceValue.CanAddr() {
		instancePtr := instanceValue.Addr()
		methodOnPtr := instancePtr.MethodByName(methodValue.Type().Name()) // Get method by name on pointer
		if methodOnPtr.IsValid() && methodOnPtr.Type().NumIn() == 2 {      // Check if method exists on pointer and takes correct args
			fmt.Printf("Calling hook %s on pointer receiver\n", methodValue.Type().Name())
			results = methodOnPtr.Call(callArgs)
			if len(results) > 0 && !results[0].IsNil() {
				if err, ok := results[0].Interface().(error); ok {
					return err // Return error from hook
				}
			}
			return nil // Hook succeeded or returned nil error
		}
	}

	// If pointer call didn't work or wasn't possible, try on value receiver
	methodOnValue := instanceValue.MethodByName(methodValue.Type().Name())
	if methodOnValue.IsValid() && methodOnValue.Type().NumIn() == 2 {
		fmt.Printf("Calling hook %s on value receiver\n", methodValue.Type().Name())
		results = methodOnValue.Call(callArgs)
		if len(results) > 0 && !results[0].IsNil() {
			if err, ok := results[0].Interface().(error); ok {
				return err // Return error from hook
			}
		}
		return nil // Hook succeeded or returned nil error
	}

	// This shouldn't happen if HasX flag was true, indicates inconsistency
	// fmt.Printf("Warning: Hook method %s found by parser but not callable via reflection.\n", methodValue.Type().Name())
	return nil // Or return an internal error?
}

// Helper function to call hook methods that modify data (e.g., BeforeUpdate)
func callHookWithData(ctx context.Context, dbContext hooks.ContextDB, methodValue reflect.Value, instanceValue reflect.Value, data map[string]any) error {

	var callArgs = []reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(dbContext),
		reflect.ValueOf(data), // Pass the data map
	}
	var results []reflect.Value

	// Try pointer receiver first
	if instanceValue.CanAddr() {
		instancePtr := instanceValue.Addr()
		methodOnPtr := instancePtr.MethodByName(methodValue.Type().Name())
		if methodOnPtr.IsValid() && methodOnPtr.Type().NumIn() == 3 {
			fmt.Printf("Calling hook %s on pointer receiver with data\n", methodValue.Type().Name())
			results = methodOnPtr.Call(callArgs)
			if len(results) > 0 && !results[0].IsNil() {
				if err, ok := results[0].Interface().(error); ok {
					return err
				}
			}
			return nil
		}
	}

	// Try value receiver
	methodOnValue := instanceValue.MethodByName(methodValue.Type().Name())
	if methodOnValue.IsValid() && methodOnValue.Type().NumIn() == 3 {
		fmt.Printf("Calling hook %s on value receiver with data\n", methodValue.Type().Name())
		results = methodOnValue.Call(callArgs)
		if len(results) > 0 && !results[0].IsNil() {
			if err, ok := results[0].Interface().(error); ok {
				return err
			}
		}
		return nil
	}
	return nil
}

// Create inserts a new record within the transaction.
func (tx *Tx) Create(ctx context.Context, value any) *Result {
	result := &Result{}
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
	model, err := tx.parser.Parse(value)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to parse schema for type %s: %w", structType.Name(), err)
		return result
	}

	// --- Call BeforeCreate Hook ---
	if model.HasBeforeCreate {
		hookMethod := reflect.ValueOf(value).MethodByName("BeforeCreate") // Get method value
		if err := callHook(ctx, tx, hookMethod, structValue); err != nil {
			result.Error = fmt.Errorf("BeforeCreate hook failed: %w", err)
			return result
		}
	}
	// --- End Hook Call ---

	var columns []string
	var placeholders []string
	var args []any
	tableName := model.TableName
	dialect := tx.dialect // Use tx.dialect
	for _, field := range model.Fields {
		if field.IsIgnored {
			continue
		}
		fieldValue := structValue.FieldByName(field.GoName)
		if !fieldValue.IsValid() {
			continue
		}
		if field.IsPrimaryKey && field.AutoIncrement && fieldValue.IsZero() {
			continue
		}
		if field.GoName == "CreatedAt" || field.GoName == "UpdatedAt" {
			isZeroTime := false
			if fieldValue.Kind() == reflect.Struct && fieldValue.Type() == reflect.TypeOf(time.Time{}) {
				isZeroTime = fieldValue.Interface().(time.Time).IsZero()
			} else if fieldValue.Kind() == reflect.Pointer && fieldValue.Type().Elem() == reflect.TypeOf(time.Time{}) {
				isZeroTime = fieldValue.IsNil()
				if !isZeroTime {
					if tPtr, ok := fieldValue.Interface().(*time.Time); ok && tPtr != nil && tPtr.IsZero() {
						isZeroTime = true
					}
				}
			}
			if isZeroTime {
				continue
			}
		}
		columns = append(columns, dialect.Quote(field.DBName))
		placeholders = append(placeholders, dialect.BindVar(len(args)+1))
		args = append(args, fieldValue.Interface())
	}
	if len(columns) == 0 {
		result.Error = fmt.Errorf("tx: no columns available for insert in type %s", structType.Name())
		return result
	}
	sqlQuery := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", dialect.Quote(tableName), strings.Join(columns, ", "), strings.Join(placeholders, ", "))
	fmt.Printf("TX Executing SQL: %s | Args: %v\n", sqlQuery, args)
	// *** Use tx.source.Exec ***
	sqlResult, err := tx.source.Exec(ctx, sqlQuery, args...)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to execute insert for %s: %w", structType.Name(), err)
		return result
	}
	if affected, errAff := sqlResult.RowsAffected(); errAff == nil {
		result.RowsAffected = affected
	} else {
		fmt.Printf("tx Warning: could not get RowsAffected after insert: %v\n", errAff)
	}
	var pkField *schema.Field = nil
	if len(model.PrimaryKeys) == 1 && model.PrimaryKeys[0].AutoIncrement {
		pkField = model.PrimaryKeys[0]
		if lastID, errID := sqlResult.LastInsertId(); errID == nil {
			result.LastInsertID = lastID
			pkValueField := structValue.FieldByName(pkField.GoName)
			if pkValueField.IsValid() && pkValueField.CanSet() {
				targetType := pkValueField.Type()
				targetValue := reflect.ValueOf(lastID)
				if targetType.Kind() != reflect.Int64 && targetValue.CanConvert(targetType) {
					pkValueField.Set(targetValue.Convert(targetType))
				} else if targetType.Kind() == reflect.Int64 {
					pkValueField.SetInt(lastID)
				} else {
					fmt.Printf("tx Warning: Cannot set auto-increment ID back on PK field %s (type mismatch: %s vs %s)\n", pkField.GoName, targetType, targetValue.Type())
				}
			} else {
				fmt.Printf("tx Warning: Cannot set auto-increment ID back on PK field %s (invalid or not settable)\n", pkField.GoName)
			}
		} else {
			fmt.Printf("tx Warning: could not get LastInsertId after insert (driver/DB may not support it): %v\n", errID)
		}
	}
	// Re-fetch logic (using tx.source) - Optional within Tx Create, as user might query later before commit.
	// For simplicity, we might omit the automatic re-fetch in the Tx version,
	// or make it optional, as the state isn't final until commit.
	// Let's omit re-fetch for Tx.Create for now. The user can tx.FindByID if needed.

	// --- Call AfterCreate Hook ---
	if model.HasAfterCreate {
		hookMethod := reflect.ValueOf(value).MethodByName("AfterCreate")
		if err := callHook(ctx, tx, hookMethod, structValue); err != nil {
			// Log error but don't fail the main operation
			fmt.Printf("tx Warning: AfterCreate hook failed: %v\n", err)
		}
	}
	// --- End Hook Call ---
	return result
}

// FindByID finds a record by primary key within the transaction.
func (tx *Tx) FindByID(ctx context.Context, dest any, id any) *Result {
	result := &Result{}
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		result.Error = fmt.Errorf("tx: destination must be a non-nil pointer to a struct, got %T", dest)
		return result
	}
	destElem := destValue.Elem()
	if destElem.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("tx: destination must be a pointer to a struct, got pointer to %s", destElem.Kind())
		return result
	}
	destType := destElem.Type()
	model, err := tx.parser.Parse(dest)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to parse schema for type %s: %w", destType.Name(), err)
		return result
	}
	if len(model.PrimaryKeys) != 1 {
		result.Error = fmt.Errorf("tx: FindByID currently supports models with exactly one primary key, found %d for %s", len(model.PrimaryKeys), model.Name)
		return result
	}
	pkField := model.PrimaryKeys[0]
	dialect := tx.dialect
	selectCols := []string{}
	scanFields := []*schema.Field{}
	for _, field := range model.Fields {
		if !field.IsIgnored {
			selectCols = append(selectCols, dialect.Quote(field.DBName))
			scanFields = append(scanFields, field)
		}
	}
	if len(selectCols) == 0 {
		result.Error = fmt.Errorf("tx: no selectable columns found for model %s", model.Name)
		return result
	}
	tableNameQuoted := dialect.Quote(model.TableName)
	pkColNameQuoted := dialect.Quote(pkField.DBName)
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s LIMIT 1", strings.Join(selectCols, ", "), tableNameQuoted, pkColNameQuoted, dialect.BindVar(1))
	fmt.Printf("TX Executing SQL: %s | Args: [%v]\n", query, id)
	// *** Use tx.source.QueryRow ***
	rowScanner := tx.source.QueryRow(ctx, query, id)
	scanDest := make([]any, len(scanFields))
	for i, field := range scanFields {
		fieldValue := destElem.FieldByName(field.GoName)
		if !fieldValue.IsValid() {
			result.Error = fmt.Errorf("tx internal error: struct field %s not found in destination", field.GoName)
			return result
		}
		if !fieldValue.CanAddr() {
			result.Error = fmt.Errorf("tx internal error: struct field %s is not addressable", field.GoName)
			return result
		}
		scanDest[i] = fieldValue.Addr().Interface()
	}
	err = rowScanner.Scan(scanDest...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			result.Error = sql.ErrNoRows
		} else {
			result.Error = fmt.Errorf("tx: failed to scan result for model %s: %w", model.Name, err)
		}
		return result
	}
	result.RowsAffected = 1

	// --- Call AfterFind Hook ---
	if model.HasAfterFind {
		hookMethod := destValue.MethodByName("AfterFind") // Call on the pointer receiver 'dest'
		if err := callHook(ctx, tx, hookMethod, destElem); err != nil {
			fmt.Printf("tx Warning: AfterFind hook failed for ID %v: %v\n", id, err)
		}
	}
	// --- End Hook Call ---

	return result
}

// Delete deletes a record by primary key within the transaction.
func (tx *Tx) Delete(ctx context.Context, value any) *Result {
	result := &Result{}
	reflectValue := reflect.ValueOf(value)
	if reflectValue.Kind() != reflect.Pointer || reflectValue.IsNil() {
		result.Error = fmt.Errorf("tx: input value must be a non-nil pointer to a struct, got %T", value)
		return result
	}
	structValue := reflectValue.Elem()
	if structValue.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("tx: input value must be a pointer to a struct, got pointer to %s", structValue.Kind())
		return result
	}

	structType := structValue.Type()
	model, err := tx.parser.Parse(value)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to parse schema for type %s: %w", structType.Name(), err)
		return result
	}

	// --- Call BeforeDelete Hook ---
	if model.HasBeforeDelete {
		hookMethod := reflectValue.MethodByName("BeforeDelete")
		if err := callHook(ctx, tx, hookMethod, structValue); err != nil {
			result.Error = fmt.Errorf("BeforeDelete hook failed: %w", err)
			return result
		}
	}
	// --- End Hook Call ---

	if len(model.PrimaryKeys) == 0 {
		result.Error = fmt.Errorf("tx: cannot delete: model %s has no primary key defined", model.Name)
		return result
	}
	pkArgs := make([]any, 0, len(model.PrimaryKeys))
	pkWhereClauses := make([]string, 0, len(model.PrimaryKeys))
	dialect := tx.dialect
	for i, pkField := range model.PrimaryKeys {
		pkValueField := structValue.FieldByName(pkField.GoName)
		if !pkValueField.IsValid() {
			result.Error = fmt.Errorf("tx internal error: primary key field %s not found in struct %s", pkField.GoName, model.Name)
			return result
		}
		if pkValueField.IsZero() {
			result.Error = fmt.Errorf("tx: cannot delete: primary key field %s has zero value", pkField.GoName)
			return result
		}
		pkArgs = append(pkArgs, pkValueField.Interface())
		pkWhereClauses = append(pkWhereClauses, fmt.Sprintf("%s = %s", dialect.Quote(pkField.DBName), dialect.BindVar(i+1)))
	}
	tableNameQuoted := dialect.Quote(model.TableName)
	sqlQuery := fmt.Sprintf("DELETE FROM %s WHERE %s", tableNameQuoted, strings.Join(pkWhereClauses, " AND "))
	fmt.Printf("TX Executing SQL: %s | Args: %v\n", sqlQuery, pkArgs)
	// *** Use tx.source.Exec ***
	sqlResult, err := tx.source.Exec(ctx, sqlQuery, pkArgs...)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to execute delete for %s: %w", model.Name, err)
		return result
	}
	affected, err := sqlResult.RowsAffected()
	if err != nil {
		fmt.Printf("tx Warning: could not get RowsAffected after delete: %v\n", err)
	}
	result.RowsAffected = affected
	if affected == 0 {
		fmt.Printf("tx Warning: Delete executed but no rows affected (record with PK probably didn't exist).\n")
	}

	// --- Call AfterDelete Hook ---
	if model.HasAfterDelete && affected > 0 { // Only call if delete likely succeeded
		hookMethod := reflectValue.MethodByName("AfterDelete")
		if err := callHook(ctx, tx, hookMethod, structValue); err != nil {
			fmt.Printf("tx Warning: AfterDelete hook failed: %v\n", err)
		}
	}
	// --- End Hook Call ---

	return result
}

// FindFirst finds the first record matching conditions within the transaction.
func (tx *Tx) FindFirst(ctx context.Context, dest any, conds ...any) *Result {
	result := &Result{}
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		result.Error = fmt.Errorf("tx: destination must be a non-nil pointer to a struct, got %T", dest)
		return result
	}
	destElem := destValue.Elem()
	if destElem.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("tx: destination must be a pointer to a struct, got pointer to %s", destElem.Kind())
		return result
	}
	destType := destElem.Type()
	model, err := tx.parser.Parse(dest)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to parse schema for type %s: %w", destType.Name(), err)
		return result
	}
	dialect := tx.dialect
	condition, _, err := processFindArgs(conds...) // Use helper from query_options.go
	if err != nil {
		result.Error = err
		return result
	}
	whereClauses, whereArgs, err := buildWhereClause(dialect, model, condition)
	if err != nil {
		result.Error = err
		return result
	} // Use helper
	selectCols := []string{}
	scanFields := []*schema.Field{}
	for _, field := range model.Fields {
		if !field.IsIgnored {
			selectCols = append(selectCols, dialect.Quote(field.DBName))
			scanFields = append(scanFields, field)
		}
	}
	if len(selectCols) == 0 {
		result.Error = fmt.Errorf("tx: no selectable columns found for model %s", model.Name)
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
	queryBuilder.WriteString(" LIMIT 1")
	sqlQuery := queryBuilder.String()
	fmt.Printf("TX Executing SQL: %s | Args: %v\n", sqlQuery, whereArgs)
	rowScanner := tx.source.QueryRow(ctx, sqlQuery, whereArgs...)
	scanDest := make([]any, len(scanFields))
	for i, field := range scanFields {
		fieldValue := destElem.FieldByName(field.GoName)
		if !fieldValue.IsValid() {
			result.Error = fmt.Errorf("tx internal error: struct field %s not found in destination", field.GoName)
			return result
		}
		if !fieldValue.CanAddr() {
			result.Error = fmt.Errorf("tx internal error: struct field %s is not addressable", field.GoName)
			return result
		}
		scanDest[i] = fieldValue.Addr().Interface()
	}
	err = rowScanner.Scan(scanDest...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			result.Error = sql.ErrNoRows
		} else {
			result.Error = fmt.Errorf("tx: failed to scan result for model %s: %w", model.Name, err)
		}
		return result
	}
	result.RowsAffected = 1

	// --- Call AfterFind Hook ---
	if model.HasAfterFind {
		hookMethod := destValue.MethodByName("AfterFind") // Call on the pointer receiver 'dest'
		if err := callHook(ctx, tx, hookMethod, destElem); err != nil {
			fmt.Printf("tx Warning: AfterFind hook failed for FindFirst: %v\n", err)
		}
	}
	// --- End Hook Call ---

	return result
}

// Updates updates specific fields within the transaction.
func (tx *Tx) Updates(ctx context.Context, modelWithValue any, data map[string]any) *Result {
	result := &Result{}
	reflectValue := reflect.ValueOf(modelWithValue)
	if reflectValue.Kind() != reflect.Pointer || reflectValue.IsNil() {
		result.Error = fmt.Errorf("tx: modelWithValue must be a non-nil pointer to a struct, got %T", modelWithValue)
		return result
	}
	structValue := reflectValue.Elem()
	if structValue.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("tx: modelWithValue must be a pointer to a struct, got pointer to %s", structValue.Kind())
		return result
	}
	structType := structValue.Type()
	model, err := tx.parser.Parse(modelWithValue)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to parse schema for type %s: %w", structType.Name(), err)
		return result
	}

	// --- Call BeforeUpdate Hook ---
	if model.HasBeforeUpdate {
		// Pass a copy of the map? Or allow modification? Let's allow modification for now.
		hookMethod := reflectValue.MethodByName("BeforeUpdate")
		if err := callHookWithData(ctx, tx, hookMethod, structValue, data); err != nil {
			result.Error = fmt.Errorf("BeforeUpdate hook failed: %w", err)
			return result
		}
	}
	// --- End Hook Call ---

	if len(model.PrimaryKeys) == 0 {
		result.Error = fmt.Errorf("tx: cannot update: model %s has no primary key defined", model.Name)
		return result
	}
	pkArgs := make([]any, 0, len(model.PrimaryKeys))
	pkWhereClauses := make([]string, 0, len(model.PrimaryKeys))
	dialect := tx.dialect
	for i, pkField := range model.PrimaryKeys {
		pkValueField := structValue.FieldByName(pkField.GoName)
		if !pkValueField.IsValid() {
			result.Error = fmt.Errorf("tx internal error: primary key field %s not found in struct %s", pkField.GoName, model.Name)
			return result
		}
		if pkValueField.IsZero() {
			result.Error = fmt.Errorf("tx: cannot update: primary key field %s has zero value", pkField.GoName)
			return result
		}
		pkArgs = append(pkArgs, pkValueField.Interface())
		pkWhereClauses = append(pkWhereClauses, fmt.Sprintf("%s = %s", dialect.Quote(pkField.DBName), dialect.BindVar(i+1)))
	}
	setClauses := []string{}
	setArgs := []any{}
	placeholderOffset := len(pkArgs)
	for dbColName, value := range data {
		field, ok := model.GetFieldByDBName(dbColName)
		if !ok {
			result.Error = fmt.Errorf("tx: invalid column name '%s' provided in update data for model %s", dbColName, model.Name)
			return result
		}
		if field.IsIgnored || field.IsPrimaryKey {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", dialect.Quote(dbColName), dialect.BindVar(placeholderOffset+len(setArgs)+1)))
		setArgs = append(setArgs, value)
	}
	if len(setClauses) == 0 {
		result.Error = fmt.Errorf("tx: no valid fields provided for update")
		return result
	}
	tableNameQuoted := dialect.Quote(model.TableName)
	sqlQuery := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tableNameQuoted, strings.Join(setClauses, ", "), strings.Join(pkWhereClauses, " AND "))
	allArgs := append(setArgs, pkArgs...)
	fmt.Printf("TX Executing SQL: %s | Args: %v\n", sqlQuery, allArgs)
	// *** Use tx.source.Exec ***
	sqlResult, err := tx.source.Exec(ctx, sqlQuery, allArgs...)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to execute update for %s: %w", model.Name, err)
		return result
	}
	affected, err := sqlResult.RowsAffected()
	if err != nil {
		fmt.Printf("tx Warning: could not get RowsAffected after update: %v\n", err)
	}
	result.RowsAffected = affected
	if affected == 0 {
		fmt.Printf("tx Warning: Update executed but no rows affected (record with PK might not exist or values were the same).\n")
	}

	// --- Call AfterUpdate Hook ---
	if model.HasAfterUpdate && affected > 0 { // Only call if update likely succeeded
		hookMethod := reflectValue.MethodByName("AfterUpdate")
		if err := callHook(ctx, tx, hookMethod, structValue); err != nil {
			fmt.Printf("tx Warning: AfterUpdate hook failed: %v\n", err)
		}
	}
	// --- End Hook Call ---

	return result
}

// Find retrieves multiple records within the transaction.
func (tx *Tx) Find(ctx context.Context, dest any, condsAndOpts ...any) *Result {
	result := &Result{}

	// 1. Validate dest input
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		result.Error = fmt.Errorf("tx: destination must be a non-nil pointer to a slice, got %T", dest)
		return result
	}
	sliceValue := destValue.Elem()
	if sliceValue.Kind() != reflect.Slice {
		result.Error = fmt.Errorf("tx: destination must be a pointer to a slice, got pointer to %s", sliceValue.Kind())
		return result
	}

	// 2. Get Slice Element Type and Parse Schema
	elementType := sliceValue.Type().Elem()
	elementIsPointer := (elementType.Kind() == reflect.Pointer)
	schemaType := elementType
	if elementIsPointer {
		schemaType = elementType.Elem()
	}
	if schemaType.Kind() != reflect.Struct {
		result.Error = fmt.Errorf("tx: destination slice elements must be structs or pointers to structs, underlying type is %s", schemaType.Kind())
		return result
	}
	model, err := tx.parser.Parse(reflect.New(schemaType).Interface())
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to parse schema for slice element type %s: %w", elementType.String(), err)
		return result
	}

	// *** NEW: Process conditions and options ***
	condition, options, err := processFindArgs(condsAndOpts...)
	if err != nil {
		result.Error = err
		return result
	}

	// 3. Build WHERE clause and arguments
	dialect := tx.dialect
	whereClauses, whereArgs, err := buildWhereClause(dialect, model, condition) // Use helper
	if err != nil {
		result.Error = err
		return result
	}

	// 4. Build SELECT SQL (including ORDER BY, LIMIT, OFFSET)
	selectCols := []string{}
	scanFields := []*schema.Field{}
	for _, field := range model.Fields {
		if !field.IsIgnored {
			selectCols = append(selectCols, dialect.Quote(field.DBName))
			scanFields = append(scanFields, field)
		}
	}
	if len(selectCols) == 0 {
		result.Error = fmt.Errorf("tx: no selectable columns found for model %s", model.Name)
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
	// *** NEW: Append optional clauses ***
	if options.orderBy != "" {
		queryBuilder.WriteString(" ORDER BY ")
		queryBuilder.WriteString(options.orderBy)
	}
	effectiveLimit := options.limit
	if options.offset > 0 && options.limit <= 0 {
		// Set a large default limit if offset is used without limit
		// Use math.MaxInt64 which is suitable for most DB limits
		effectiveLimit = math.MaxInt64
		fmt.Printf("TX Applying default LIMIT %d because OFFSET %d was used without explicit LIMIT.\n", effectiveLimit, options.offset)
	}
	if effectiveLimit > 0 { // Append LIMIT if it's positive (either user-set or default)
		queryBuilder.WriteString(" LIMIT ")
		queryBuilder.WriteString(strconv.FormatInt(int64(effectiveLimit), 10)) // Use FormatInt for safety
	}
	if options.offset > 0 { // Append OFFSET if it's positive
		queryBuilder.WriteString(" OFFSET ")
		queryBuilder.WriteString(strconv.Itoa(options.offset))
	}
	sqlQuery := queryBuilder.String()

	// 5. Execute Query using Query()
	fmt.Printf("TX Executing SQL: %s | Args: %v\n", sqlQuery, whereArgs)
	// *** Use tx.source.Query ***
	rows, err := tx.source.Query(ctx, sqlQuery, whereArgs...)
	if err != nil {
		result.Error = fmt.Errorf("tx: failed to execute find query for %s: %w", model.Name, err)
		return result
	}
	defer rows.Close()

	// 6. Iterate and Scan Rows into Slice (remains the same logic)
	sliceValue.Set(reflect.MakeSlice(sliceValue.Type(), 0, 0))
	rowCount := 0

	var addedElements []reflect.Value
	for rows.Next() {
		rowCount++
		newElemInstance := reflect.New(schemaType).Elem()
		scanDest := make([]any, len(scanFields))
		for i, field := range scanFields {
			fieldValue := newElemInstance.FieldByName(field.GoName)
			if !fieldValue.IsValid() {
				result.Error = fmt.Errorf("tx internal error: struct field %s not found in new element", field.GoName)
				return result
			}
			if !fieldValue.CanAddr() {
				result.Error = fmt.Errorf("tx internal error: struct field %s is not addressable", field.GoName)
				return result
			}
			scanDest[i] = fieldValue.Addr().Interface()
		}
		if err := rows.Scan(scanDest...); err != nil {
			result.Error = fmt.Errorf("tx: failed to scan row for model %s: %w", model.Name, err)
			return result
		}
		if elementIsPointer {
			elemPtr := newElemInstance.Addr()
			sliceValue.Set(reflect.Append(sliceValue, elemPtr))
			addedElements = append(addedElements, elemPtr) // Store pointer
		} else {
			sliceValue.Set(reflect.Append(sliceValue, newElemInstance))
			addedElements = append(addedElements, newElemInstance) // Store value
		}
	}
	if err := rows.Err(); err != nil {
		result.Error = fmt.Errorf("tx: error iterating query results for %s: %w", model.Name, err)
		return result
	}
	result.RowsAffected = int64(rowCount)

	// --- Call AfterFind Hook for each found element ---
	if model.HasAfterFind && rowCount > 0 {
		for _, elemValue := range addedElements {
			instanceValue := elemValue // This is either the struct value or pointer value
			hookMethod := instanceValue.MethodByName("AfterFind")
			if hookMethod.IsValid() { // Check if method exists on the specific value/pointer
				// Need the underlying struct value for callHook if elem is pointer
				structValForHook := instanceValue
				if instanceValue.Kind() == reflect.Pointer {
					structValForHook = instanceValue.Elem()
				}
				if err := callHook(ctx, tx, hookMethod, structValForHook); err != nil {
					fmt.Printf("tx Warning: AfterFind hook failed for element: %v\n", err)
				}
			}
		}
	}
	// --- End Hook Call ---
	return result
}
