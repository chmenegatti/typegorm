// pkg/dialects/mysql/mysql.go
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	// Import blank the underlying driver for its side effects (registration in database/sql)
	_ "github.com/go-sql-driver/mysql"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/dialects" // For registration
	"github.com/chmenegatti/typegorm/pkg/dialects/common"
	"github.com/chmenegatti/typegorm/pkg/schema"
)

// --- Dialect Implementation ---

// mysqlDialect implements the common.Dialect interface for MySQL/MariaDB.
type mysqlDialect struct{}

func (d *mysqlDialect) Name() string {
	return "mysql" // Name used in configuration and registration
}

func (d *mysqlDialect) Quote(identifier string) string {
	// MySQL uses backticks for quoting identifiers
	return "`" + identifier + "`"
}

func (d *mysqlDialect) BindVar(i int) string {
	// MySQL uses '?' as the placeholder for prepared statements
	return "?"
}

// GetDataType maps a schema.Field definition to a MySQL data type string.
// This is a basic implementation; more types and constraints will be added later.
func (d *mysqlDialect) GetDataType(field *schema.Field) (string, error) {
	sqlType := ""
	switch field.GoType.Kind() {
	case reflect.String:
		if field.Size > 0 && field.Size < 65535 { // Approximate limit for VARCHAR
			sqlType = fmt.Sprintf("VARCHAR(%d)", field.Size)
		} else if field.Size >= 65535 {
			sqlType = "TEXT" // Or MEDIUMTEXT, LONGTEXT depending on size - needs refinement
		} else {
			sqlType = "VARCHAR(255)" // Default size
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		sqlType = "INT"
	case reflect.Int64, reflect.Uint64:
		sqlType = "BIGINT"
		// TODO: Add UNSIGNED if Go type is uint*? Driver often handles this.
	case reflect.Bool:
		sqlType = "TINYINT(1)"
	case reflect.Float32:
		sqlType = "FLOAT"
	case reflect.Float64:
		sqlType = "DOUBLE"
	case reflect.Struct:
		if field.GoType == reflect.TypeOf(time.Time{}) {
			sqlType = "DATETIME" // Or TIMESTAMP depending on requirements
		} // Add other struct types like NullString, NullInt64 etc.
	case reflect.Slice:
		if field.GoType.Elem().Kind() == reflect.Uint8 {
			sqlType = "BLOB" // Or VARBINARY, MEDIUMBLOB, LONGBLOB based on size
		}
	}

	if sqlType == "" {
		return "", fmt.Errorf("unsupported data type for MySQL: %s", field.GoType.Kind())
	}

	// Add constraints based on schema.Field metadata (basic example)
	var constraints []string
	if field.IsPrimary {
		constraints = append(constraints, "PRIMARY KEY")
		// TODO: Add AUTO_INCREMENT based on a tag/convention
		// if field.IsAutoIncrement { constraints = append(constraints, "AUTO_INCREMENT") }
	}
	if field.IsRequired {
		constraints = append(constraints, "NOT NULL")
	}
	if field.DefaultValue != nil {
		constraints = append(constraints, fmt.Sprintf("DEFAULT %s", *field.DefaultValue)) // Ensure DefaultValue is properly quoted/formatted
	}
	// TODO: Add UNIQUE, INDEX etc. based on tags

	return strings.TrimSpace(sqlType + " " + strings.Join(constraints, " ")), nil
}

// --- DataSource Implementation ---

// mysqlDataSource implements common.DataSource using the standard database/sql package.
type mysqlDataSource struct {
	db      *sql.DB        // Connection pool
	dialect common.Dialect // Instance of mysqlDialect
}

// Connect establishes the database connection pool.
func (ds *mysqlDataSource) Connect(cfg config.DatabaseConfig) error {
	if ds.db != nil {
		return fmt.Errorf("datasource already connected")
	}
	if cfg.Dialect != ds.dialect.Name() {
		return fmt.Errorf("configuration dialect '%s' does not match datasource dialect '%s'", cfg.Dialect, ds.dialect.Name())
	}

	// DSN format for go-sql-driver/mysql:
	// [user[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
	// Example: "user:password@tcp(localhost:3306)/mydatabase?parseTime=true"
	db, err := sql.Open(ds.dialect.Name(), cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open mysql connection: %w", err)
	}

	// Apply connection pool settings
	if cfg.Pool.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.Pool.MaxIdleConns)
	}
	if cfg.Pool.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.Pool.MaxOpenConns)
	}
	if cfg.Pool.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.Pool.ConnMaxLifetime)
	}

	// Verify connection is working
	// Use a reasonable timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close() // Close the pool if ping fails
		return fmt.Errorf("failed to ping mysql database: %w", err)
	}

	ds.db = db
	return nil
}

// Close closes the underlying database connection pool.
func (ds *mysqlDataSource) Close() error {
	if ds.db == nil {
		return fmt.Errorf("datasource is not connected")
	}
	err := ds.db.Close()
	ds.db = nil // Mark as closed
	return err
}

// Ping checks the database connectivity.
func (ds *mysqlDataSource) Ping(ctx context.Context) error {
	if ds.db == nil {
		return fmt.Errorf("datasource is not connected")
	}
	return ds.db.PingContext(ctx)
}

// Dialect returns the associated dialect implementation.
func (ds *mysqlDataSource) Dialect() common.Dialect {
	return ds.dialect
}

// BeginTx starts a new transaction.
func (ds *mysqlDataSource) BeginTx(ctx context.Context, opts any) (common.Tx, error) {
	if ds.db == nil {
		return nil, fmt.Errorf("datasource is not connected")
	}

	var txOptions *sql.TxOptions
	// Check if opts is sql.TxOptions or compatible
	if sqlOpts, ok := opts.(sql.TxOptions); ok {
		txOptions = &sqlOpts
	} else if opts != nil {
		// Handle incompatible options if necessary, or return error
		return nil, fmt.Errorf("unsupported transaction options type: %T", opts)
	}

	sqlTx, err := ds.db.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to begin mysql transaction: %w", err)
	}
	return &mysqlTx{tx: sqlTx}, nil // Wrap the standard sql.Tx
}

// Exec executes a query without returning rows.
func (ds *mysqlDataSource) Exec(ctx context.Context, query string, args ...any) (common.Result, error) {
	if ds.db == nil {
		return nil, fmt.Errorf("datasource is not connected")
	}
	res, err := ds.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err // Return underlying error directly
	}
	return &mysqlResult{result: res}, nil // Wrap the standard sql.Result
}

// QueryRow executes a query expected to return at most one row.
func (ds *mysqlDataSource) QueryRow(ctx context.Context, query string, args ...any) common.RowScanner {
	if ds.db == nil {
		// How to return an error here? The interface returns RowScanner directly.
		// Option 1: Return a RowScanner that always returns an error on Scan.
		// Option 2: Change interface (breaking change).
		// Let's go with option 1 for now.
		return &errorRowScanner{err: fmt.Errorf("datasource is not connected")}
	}
	row := ds.db.QueryRowContext(ctx, query, args...)
	return &mysqlRowScanner{row: row} // Wrap the standard sql.Row
}

// Query executes a query returning multiple rows.
func (ds *mysqlDataSource) Query(ctx context.Context, query string, args ...any) (common.Rows, error) {
	if ds.db == nil {
		return nil, fmt.Errorf("datasource is not connected")
	}
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err // Return underlying error directly
	}
	return &mysqlRows{rows: rows}, nil // Wrap the standard sql.Rows
}

// --- Tx Implementation ---

// mysqlTx wraps sql.Tx to implement common.Tx.
type mysqlTx struct {
	tx *sql.Tx
}

func (t *mysqlTx) Commit() error {
	return t.tx.Commit()
}

func (t *mysqlTx) Rollback() error {
	return t.tx.Rollback()
}

func (t *mysqlTx) Exec(ctx context.Context, query string, args ...any) (common.Result, error) {
	res, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &mysqlResult{result: res}, nil
}

func (t *mysqlTx) QueryRow(ctx context.Context, query string, args ...any) common.RowScanner {
	row := t.tx.QueryRowContext(ctx, query, args...)
	return &mysqlRowScanner{row: row}
}

func (t *mysqlTx) Query(ctx context.Context, query string, args ...any) (common.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &mysqlRows{rows: rows}, nil
}

// --- Result Implementation ---

// mysqlResult wraps sql.Result to implement common.Result.
type mysqlResult struct {
	result sql.Result
}

func (r *mysqlResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

func (r *mysqlResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

// --- Rows Implementation ---

// mysqlRows wraps sql.Rows to implement common.Rows.
type mysqlRows struct {
	rows *sql.Rows
}

func (r *mysqlRows) Close() error {
	return r.rows.Close()
}

func (r *mysqlRows) Next() bool {
	return r.rows.Next()
}

func (r *mysqlRows) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

func (r *mysqlRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *mysqlRows) Err() error {
	return r.rows.Err()
}

// --- RowScanner Implementation ---

// mysqlRowScanner wraps sql.Row to implement common.RowScanner.
type mysqlRowScanner struct {
	row *sql.Row
}

func (rs *mysqlRowScanner) Scan(dest ...any) error {
	return rs.row.Scan(dest...)
}

// errorRowScanner is a RowScanner that always returns an error.
// Used when QueryRow is called on a disconnected DataSource.
type errorRowScanner struct {
	err error
}

func (ers *errorRowScanner) Scan(dest ...any) error {
	return ers.err
}

// --- Driver Registration ---

func init() {
	// Register this dialect implementation with the global registry
	dialects.Register("mysql", func() common.DataSource {
		// The factory returns a new instance, Connect will be called on it later.
		return &mysqlDataSource{
			dialect: &mysqlDialect{}, // Create/assign the dialect instance
		}
	})
}
