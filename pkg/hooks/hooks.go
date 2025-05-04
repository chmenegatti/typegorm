// pkg/hooks/hooks.go
package hooks

import (
	"context"
	// We might need schema later if hooks need access to the model definition
	// "github.com/chmenegatti/typegorm/pkg/schema"
)

// ContextDB represents the database or transaction context passed to hooks.
// It allows hooks to perform further database operations if needed.
// Define methods here that both *DB and *Tx will implement.
// Start simple, add methods as required by hook implementations.
type ContextDB interface {
	// Example (add later if needed):
	// GetModel(value any) (*schema.Model, error)
	// FindFirst(ctx context.Context, dest any, conds ...any) error // Need to return error, not *Result
}

// --- Create Hooks ---

type BeforeCreator interface {
	BeforeCreate(ctx context.Context, db ContextDB) error
}

type AfterCreator interface {
	AfterCreate(ctx context.Context, db ContextDB) error
}

// --- Update Hooks ---

type BeforeUpdater interface {
	// data map contains DB column names and values
	BeforeUpdate(ctx context.Context, db ContextDB, data map[string]any) error
}

type AfterUpdater interface {
	AfterUpdate(ctx context.Context, db ContextDB) error
}

// --- Delete Hooks ---

type BeforeDeleter interface {
	BeforeDelete(ctx context.Context, db ContextDB) error
}

type AfterDeleter interface {
	AfterDelete(ctx context.Context, db ContextDB) error
}

// --- Find Hooks ---

type AfterFinder interface {
	// Called after finding and scanning a single record or each record in a slice.
	AfterFind(ctx context.Context, db ContextDB) error
}
