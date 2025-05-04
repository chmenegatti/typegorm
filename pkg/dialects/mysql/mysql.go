// pkg/dialects/mysql/mysql.go
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // Register driver

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/dialects"
	"github.com/chmenegatti/typegorm/pkg/dialects/common"
	"github.com/chmenegatti/typegorm/pkg/schema"
)

// --- Dialect Implementation ---

// --- Driver Registration ---

func init() {
	// Register this dialect's DataSource factory with the global registry
	dialects.Register("mysql", func() common.DataSource {
		// The factory returns a new DataSource instance with its specific dialect.
		// The Connect method will be called on this instance later by the application.
		return &mysqlDataSource{
			dialect: &mysqlDialect{}, // Assign the dialect implementation
		}
	})
	fmt.Println("MySQL dialect registered.") // Add log to confirm registration
}

// mysqlDialect implements the common.Dialect interface for MySQL/MariaDB.
type mysqlDialect struct{}

// (Keep existing Name, Quote, BindVar, GetDataType methods as they are)
func (d *mysqlDialect) Name() string {
	return "mysql"
}

func (d *mysqlDialect) Quote(identifier string) string {
	// Consider replacing internal backticks if necessary, but this is usually sufficient
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func (d *mysqlDialect) BindVar(i int) string {
	return "?"
}

func (d mysqlDialect) GetDataType(field *schema.Field) (string, error) {
	// 1. Check for explicit SQL type override from tag
	if field.SQLType != "" {
		// User specified the exact type (e.g., "VARCHAR(150)", "DECIMAL(10,2)")
		// We might still need to add constraints like NOT NULL, DEFAULT etc.
		sqlType := field.SQLType
		var constraints []string
		if field.IsRequired {
			constraints = append(constraints, "NOT NULL")
		}
		// Note: DEFAULT, PRIMARY KEY, AUTO_INCREMENT might conflict if already part of SQLType tag.
		// Let's assume user knows what they are doing if they provide full type.
		// We *could* try parsing SQLType to separate base type from constraints, but keep simple for now.
		if field.DefaultValue != nil {
			// TODO: Improve default value quoting/formatting for different types
			constraints = append(constraints, fmt.Sprintf("DEFAULT %s", formatDefaultValue(*field.DefaultValue)))
		}
		if field.IsPrimaryKey {
			constraints = append(constraints, "PRIMARY KEY")
		}
		if field.AutoIncrement {
			constraints = append(constraints, "AUTO_INCREMENT")
		}
		// Consider adding UNIQUE here too? field.Unique

		return strings.TrimSpace(sqlType + " " + strings.Join(constraints, " ")), nil
	}

	// 2. Infer from Go type
	var baseType string
	goType := field.GoType // Use the type directly from the field

	// Determine the kind, handling pointers specifically for the switch
	kind := goType.Kind()
	underlyingKind := kind
	underlyingType := goType
	if kind == reflect.Pointer {
		underlyingType = goType.Elem()
		underlyingKind = underlyingType.Kind()
	}

	switch underlyingKind {
	case reflect.String:
		if field.Size > 0 && field.Size < 65535 {
			baseType = fmt.Sprintf("VARCHAR(%d)", field.Size)
		} else if field.Size >= 65535 {
			baseType = "TEXT" // Or MEDIUMTEXT, LONGTEXT based on size
		} else {
			// Check if it resembles a UUID based on name? Or require explicit type:text/varchar?
			// Defaulting to TEXT might be safer than VARCHAR(255) if size is unknown.
			// Let's default to TEXT if size tag is absent.
			baseType = "TEXT"
		}
	case reflect.Int, reflect.Int32, reflect.Uint, reflect.Uint32, reflect.Int16, reflect.Uint16, reflect.Int8, reflect.Uint8:
		// Use INT for standard integers unless PK+AutoIncrement suggests BIGINT might be safer?
		// Let's stick to INT unless it's a PK, maybe. GORM uses INT for uint32 too.
		baseType = "INT"
		if field.GoType.Kind() == reflect.Uint || field.GoType.Kind() == reflect.Uint32 || field.GoType.Kind() == reflect.Uint16 || field.GoType.Kind() == reflect.Uint8 {
			baseType += " UNSIGNED"
		}
	case reflect.Int64, reflect.Uint64:
		baseType = "BIGINT"
		if field.GoType.Kind() == reflect.Uint64 {
			baseType += " UNSIGNED"
		}
	case reflect.Bool:
		baseType = "BOOLEAN" // BOOLEAN is an alias for TINYINT(1) in MySQL
	case reflect.Float32:
		baseType = "FLOAT"
	case reflect.Float64:
		baseType = "DOUBLE"
	case reflect.Struct:
		// *** NEW CHECK: Use underlyingType ***
		var timeType = reflect.TypeOf(time.Time{})
		// Check if the underlying type (after pointer dereference) is time.Time
		if underlyingType == timeType {
			baseType = "DATETIME(6)"
		} else {
			// TODO: Handle sql.Null* types (e.g., check underlyingType.PkgPath() and .Name())
			return "", fmt.Errorf("unsupported struct type for mysql: %s", goType.String())
		}
	case reflect.Slice:
		// Assume []byte for BLOB/BINARY types
		if field.GoType.Elem().Kind() == reflect.Uint8 {
			if field.Size > 0 && field.Size < 65535 {
				baseType = fmt.Sprintf("VARBINARY(%d)", field.Size)
			} else {
				// Default to BLOB, could refine to MEDIUMBLOB/LONGBLOB based on Size tag
				baseType = "BLOB"
			}
		} else {
			return "", fmt.Errorf("unsupported slice type for mysql: %s", field.GoType.String())
		}
	default:
		return "", fmt.Errorf("unsupported go type kind for mysql: %s", underlyingKind)
	}

	// 3. Add constraints
	var timeType = reflect.TypeOf(time.Time{})
	var constraints []string
	hasDefault := false
	if field.DefaultValue != nil {
		constraints = append(constraints, fmt.Sprintf("DEFAULT %s", formatDefaultValue(*field.DefaultValue)))
		hasDefault = true
	}
	if field.IsRequired {
		constraints = append(constraints, "NOT NULL")
	}
	if field.IsPrimaryKey {
		constraints = append(constraints, "PRIMARY KEY")
	}
	if field.AutoIncrement {
		constraints = append(constraints, "AUTO_INCREMENT")
	}
	if field.Unique {
		constraints = append(constraints, "UNIQUE")
	} // Simple column unique constraint

	isTimeField := (underlyingType == timeType)

	if isTimeField && !hasDefault {
		if field.GoName == "CreatedAt" {
			constraints = append(constraints, "DEFAULT CURRENT_TIMESTAMP(6)")
			// Add NOT NULL if it's not already required and underlying Go type wasn't a pointer
			if !field.IsRequired && goType.Kind() != reflect.Pointer {
				constraints = append(constraints, "NOT NULL")
			}
			hasDefault = true // Ensure we don't add another default later
		} else if field.GoName == "UpdatedAt" {
			// Handle UpdatedAt with ON UPDATE clause
			// Default to NULL unless required, updates automatically
			constraints = append(constraints, "DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP(6)")
			// No need to add NOT NULL unless explicitly required by tag
			hasDefault = true
		}

	}

	return strings.TrimSpace(baseType + " " + strings.Join(constraints, " ")), nil
}

// formatDefaultValue attempts to format a default value string as an SQL literal.
// WARNING: This is a basic attempt and may not cover all edge cases or types correctly.
// Databases differ in how defaults (especially functions like NOW()) are specified.
func formatDefaultValue(value string) string {
	// Keep common function calls unquoted
	upperVal := strings.ToUpper(value)
	if upperVal == "CURRENT_TIMESTAMP" || upperVal == "NOW()" || upperVal == "NULL" {
		return value // Assume it's a function or keyword
	}
	// Try to detect if it's purely numeric (int or float)
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return value // Assume it's a number, don't quote
	}
	// Otherwise, assume it's a string and quote it
	// Replace single quotes with escaped single quotes for SQL
	escapedValue := strings.ReplaceAll(value, "'", "''")
	return "'" + escapedValue + "'"
}

// --- NEW: Migration History Table SQL Generation Methods ---

// CreateSchemaMigrationsTableSQL returns the SQL for creating the migrations table in MySQL.
func (d *mysqlDialect) CreateSchemaMigrationsTableSQL(tableName string) string {
	// Use the dialect's Quote method for the table name.
	// Use DATETIME(6) for applied_at to store microsecond precision.
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
    id VARCHAR(255) NOT NULL PRIMARY KEY COMMENT 'Migration identifier (e.g., timestamp_name)',
    applied_at DATETIME(6) NOT NULL COMMENT 'Timestamp when the migration was applied UTC'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Tracks applied schema migrations';`,
		d.Quote(tableName),
	)
}

// GetAppliedMigrationsSQL returns the SQL to get applied migration IDs and timestamps from MySQL.
func (d *mysqlDialect) GetAppliedMigrationsSQL(tableName string) string {
	// Order by ID ASC for consistent processing.
	return fmt.Sprintf("SELECT id, applied_at FROM %s ORDER BY id ASC;", d.Quote(tableName))
}

// InsertMigrationSQL returns the SQL for inserting a migration record in MySQL.
func (d *mysqlDialect) InsertMigrationSQL(tableName string) string {
	// Use the dialect's BindVar for placeholders. Expects parameters: id (string), applied_at (time.Time)
	return fmt.Sprintf("INSERT INTO %s (id, applied_at) VALUES (%s, %s);",
		d.Quote(tableName),
		d.BindVar(1), // Placeholder for id
		d.BindVar(2), // Placeholder for applied_at (should be UTC)
	)
}

// DeleteMigrationSQL returns the SQL for deleting a migration record in MySQL by ID.
func (d *mysqlDialect) DeleteMigrationSQL(tableName string) string {
	// Use the dialect's BindVar for the placeholder. Expects parameter: id (string)
	return fmt.Sprintf("DELETE FROM %s WHERE id = %s;",
		d.Quote(tableName),
		d.BindVar(1), // Placeholder for id
	)
}

// --- End of Migration Specific Methods ---

// --- DataSource Implementation (mysqlDataSource) ---
// (Keep your existing mysqlDataSource struct and its methods: Connect, Close, Ping, Dialect, BeginTx, Exec, QueryRow, Query)
// ... (Your existing DataSource code here) ...

type mysqlDataSource struct {
	db      *sql.DB        // Connection pool
	dialect common.Dialect // Instance of mysqlDialect
}

// Connect establishes the database connection pool.
func (ds *mysqlDataSource) Connect(cfg config.DatabaseConfig) error {
	if ds.db != nil {
		// Changed error message slightly for clarity
		return fmt.Errorf("mysql datasource is already connected")
	}
	if cfg.Dialect != ds.dialect.Name() {
		return fmt.Errorf("configuration dialect '%s' does not match datasource dialect '%s'", cfg.Dialect, ds.dialect.Name())
	}
	if cfg.DSN == "" {
		return fmt.Errorf("database DSN is required in configuration")
	}

	// Add parseTime=true automatically if not present, crucial for scanning DATETIME/TIMESTAMP into time.Time
	dsn := cfg.DSN
	if !strings.Contains(dsn, "parseTime=true") {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		dsn = fmt.Sprintf("%s%sparseTime=true", dsn, separator)
	}
	// Consider adding multiStatements=true if needed for running migration scripts directly,
	// but be aware of SQL injection risks if not handled carefully.

	db, err := sql.Open(ds.dialect.Name(), dsn)
	if err != nil {
		return fmt.Errorf("failed to open mysql connection using driver '%s': %w", ds.dialect.Name(), err)
	}

	// Apply connection pool settings from config (ensure Pool struct exists in config.DatabaseConfig)
	// Check if Pool is non-nil before accessing members if it's a pointer
	// Assuming Pool is a struct value based on previous context:
	if cfg.Pool.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.Pool.MaxIdleConns)
	} else {
		// Set a reasonable default if not specified? e.g., 2
		db.SetMaxIdleConns(2)
	}
	if cfg.Pool.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.Pool.MaxOpenConns)
	}
	if cfg.Pool.ConnMaxIdleTime > 0 { // Use ConnMaxIdleTime introduced in Go 1.15+
		db.SetConnMaxIdleTime(cfg.Pool.ConnMaxIdleTime)
	}
	if cfg.Pool.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.Pool.ConnMaxLifetime)
	}

	// Verify connection is working
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Sensible default timeout
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close() // Close the pool if ping fails
		return fmt.Errorf("failed to ping mysql database: %w", err)
	}

	ds.db = db
	fmt.Printf("Successfully connected to MySQL database using DSN: %s\n", dsn) // Informative log
	return nil
}

func (ds *mysqlDataSource) Close() error {
	if ds.db == nil {
		return fmt.Errorf("mysql datasource is not connected")
	}
	err := ds.db.Close()
	ds.db = nil // Mark as closed
	if err == nil {
		fmt.Println("MySQL database connection closed.")
	}
	return err
}

func (ds *mysqlDataSource) Ping(ctx context.Context) error {
	if ds.db == nil {
		return fmt.Errorf("mysql datasource is not connected")
	}
	return ds.db.PingContext(ctx)
}

func (ds *mysqlDataSource) Dialect() common.Dialect {
	return ds.dialect
}

func (ds *mysqlDataSource) BeginTx(ctx context.Context, opts any) (common.Tx, error) {
	if ds.db == nil {
		return nil, fmt.Errorf("mysql datasource is not connected")
	}

	var txOptions *sql.TxOptions
	if sqlOpts, ok := opts.(sql.TxOptions); ok {
		txOptions = &sqlOpts
	} else if opts != nil {
		// Maybe log a warning instead of erroring? Or support specific option types later.
		return nil, fmt.Errorf("unsupported transaction options type: %T", opts)
	}

	sqlTx, err := ds.db.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to begin mysql transaction: %w", err)
	}
	// Wrap the standard sql.Tx in our common.Tx implementation
	return &mysqlTx{tx: sqlTx}, nil
}

func (ds *mysqlDataSource) Exec(ctx context.Context, query string, args ...any) (common.Result, error) {
	if ds.db == nil {
		return nil, fmt.Errorf("mysql datasource is not connected")
	}
	res, err := ds.db.ExecContext(ctx, query, args...)
	if err != nil {
		// Consider wrapping the error for more context if needed downstream
		return nil, fmt.Errorf("mysql exec failed: %w", err)
	}
	// Wrap the standard sql.Result
	return &mysqlResult{result: res}, nil
}

func (ds *mysqlDataSource) QueryRow(ctx context.Context, query string, args ...any) common.RowScanner {
	if ds.db == nil {
		// Return the error scanner wrapper as implemented
		return &errorRowScanner{err: fmt.Errorf("mysql datasource is not connected")}
	}
	// Wrap the standard sql.Row
	return &mysqlRowScanner{row: ds.db.QueryRowContext(ctx, query, args...)}
}

func (ds *mysqlDataSource) Query(ctx context.Context, query string, args ...any) (common.Rows, error) {
	if ds.db == nil {
		return nil, fmt.Errorf("mysql datasource is not connected")
	}
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		// Consider wrapping error
		return nil, fmt.Errorf("mysql query failed: %w", err)
	}
	// Wrap the standard sql.Rows
	return &mysqlRows{rows: rows}, nil
}

// --- Tx Implementation (mysqlTx) ---
type mysqlTx struct {
	tx *sql.Tx
}

func (t *mysqlTx) Commit() error   { return t.tx.Commit() }
func (t *mysqlTx) Rollback() error { return t.tx.Rollback() }
func (t *mysqlTx) Exec(ctx context.Context, query string, args ...any) (common.Result, error) {
	res, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mysql tx exec failed: %w", err)
	}
	return &mysqlResult{result: res}, nil
}
func (t *mysqlTx) QueryRow(ctx context.Context, query string, args ...any) common.RowScanner {
	return &mysqlRowScanner{row: t.tx.QueryRowContext(ctx, query, args...)}
}
func (t *mysqlTx) Query(ctx context.Context, query string, args ...any) (common.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mysql tx query failed: %w", err)
	}
	return &mysqlRows{rows: rows}, nil
}

// --- Result Implementation (mysqlResult) ---
type mysqlResult struct{ result sql.Result }

func (r *mysqlResult) LastInsertId() (int64, error) { return r.result.LastInsertId() }
func (r *mysqlResult) RowsAffected() (int64, error) { return r.result.RowsAffected() }

// --- Rows Implementation (mysqlRows) ---
type mysqlRows struct{ rows *sql.Rows }

func (r *mysqlRows) Close() error               { return r.rows.Close() }
func (r *mysqlRows) Next() bool                 { return r.rows.Next() }
func (r *mysqlRows) Scan(dest ...any) error     { return r.rows.Scan(dest...) }
func (r *mysqlRows) Columns() ([]string, error) { return r.rows.Columns() }
func (r *mysqlRows) Err() error                 { return r.rows.Err() }

// --- RowScanner Implementation (mysqlRowScanner, errorRowScanner) ---
type mysqlRowScanner struct{ row *sql.Row }

func (rs *mysqlRowScanner) Scan(dest ...any) error { return rs.row.Scan(dest...) }

type errorRowScanner struct{ err error }

func (ers *errorRowScanner) Scan(dest ...any) error { return ers.err }
