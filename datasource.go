// datasource.go
package typegorm

import (
	"context"
	"database/sql"
	"errors"
	// Added for Ping timeout example later
)

// DriverType represents the type of the database driver.
type DriverType string

const (
	MySQL    DriverType = "mysql"
	Postgres DriverType = "postgres"
	SQLite   DriverType = "sqlite"
	Oracle   DriverType = "oracle"
	Mongo    DriverType = "mongo"
	Redis    DriverType = "redis"
	// Add other driver types as needed
)

// Config is an empty interface representing driver-specific configuration structs.
// Each driver package will define its own concrete config struct.
type Config interface{}

// DataSource represents an active and configured connection to a database.
// This is the core interface the rest of TypeGorm will use to interact
// with the database, abstracting away driver-specific details.
type DataSource interface {
	// Connect establishes the actual connection to the database using the provided config.
	// Typically called internally by a higher-level function like typegorm.Connect().
	Connect(cfg Config) error

	// Close terminates the connection to the database and releases resources.
	Close() error

	// Ping verifies that the connection to the database is still alive.
	Ping(ctx context.Context) error

	// GetDriverType returns the driver type for this DataSource.
	GetDriverType() DriverType

	// GetDB returns the underlying *sql.DB object if this is an SQL database.
	// Returns nil or an error if not applicable (e.g., for MongoDB, Redis).
	// This allows using specific features of `database/sql` if needed.
	GetDB() (*sql.DB, error)

	// GetNativeConnection returns the underlying native driver connection/client
	// (e.g., *mongo.Client, *redis.Client). Returns an error if not applicable.
	// Useful for very specific operations not covered by the ORM.
	GetNativeConnection() (interface{}, error)

	// TODO: Add methods for query execution, transaction management, etc.
	// BeginTransaction(ctx context.Context) (Transaction, error)
	// Exec(ctx context.Context, query string, args ...interface{}) (Result, error)
	// QueryRow(ctx context.Context, query string, args ...interface{}) RowScanner
	// Query(ctx context.Context, query string, args ...interface{}) (Rows, error)
}

// --- Factory Functions (to be fully implemented later) ---

// Connect is the high-level function users will call to establish a connection.
// It will determine the driver type from the config and return the appropriate
// DataSource implementation using a driver registry.
func Connect(cfg Config) (DataSource, error) {
	// The actual implementation will use a driver registry.
	return nil, errors.New("typegorm.Connect: factory function not yet implemented")
}

// DriverFactory defines the signature for a function that creates a new instance
// of a specific DataSource implementation.
type DriverFactory func() DataSource

// driverRegistry holds the factories for each registered driver type.
// var driverRegistry = make(map[DriverType]DriverFactory) // Access needs to be synchronized

// RegisterDriver registers a DriverFactory for a given driver type.
// This function will be called by driver packages during their initialization (`init`).
// func RegisterDriver(name DriverType, factory DriverFactory) {
//     // Implementation needs locking for concurrent safety
//     // if driverRegistry == nil { driverRegistry = make(map[DriverType]DriverFactory) }
//     // driverRegistry[name] = factory
// }
