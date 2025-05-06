// pkg/migration/gomigrate.go
package migration

import (
	"context"
	"database/sql" // Use standard sql package for DB access in migrations
	"fmt"
	"sync"
)

// GoMigration defines the interface that Go-based migration files must implement.
type GoMigration interface {
	// Up executes the forward migration logic.
	// It receives the database connection handle.
	// Returning an error will cause the migration runner to stop and rollback (if in transaction).
	Up(ctx context.Context, db *sql.DB) error

	// Down executes the backward migration logic (rollback).
	// It receives the database connection handle.
	// Returning an error will cause the migration runner to stop and rollback (if in transaction).
	Down(ctx context.Context, db *sql.DB) error
}

// goMigrationEntry holds a registered Go migration.

var (
	goMigrationsRegistry = make(map[string]GoMigration)
	goMigrationsMu       sync.RWMutex
)

// RegisterGoMigration registers a Go migration implementation with the runner.
// It should be called from the init() function of a Go migration file.
// The ID must match the timestamp prefix of the migration filename.
// Panics if the ID is already registered.
func RegisterGoMigration(id string, migration GoMigration) {
	if id == "" {
		panic("migration: RegisterGoMigration called with empty ID")
	}
	if migration == nil {
		panic(fmt.Sprintf("migration: RegisterGoMigration called with nil migration for ID %s", id))
	}

	goMigrationsMu.Lock()
	defer goMigrationsMu.Unlock()

	if _, exists := goMigrationsRegistry[id]; exists {
		panic(fmt.Sprintf("migration: RegisterGoMigration called twice for ID %s", id))
	}
	goMigrationsRegistry[id] = migration
	fmt.Printf("Registered Go migration: %s\n", id)
}

// getGoMigration retrieves a registered Go migration by its ID.
// Returns the migration and true if found, otherwise nil and false.
func getGoMigration(id string) (GoMigration, bool) {
	goMigrationsMu.RLock()
	defer goMigrationsMu.RUnlock()
	migration, found := goMigrationsRegistry[id]
	return migration, found
}
