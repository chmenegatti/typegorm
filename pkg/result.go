// pkg/typegorm/result.go
package typegorm

// Import common for potential reuse

// Result encapsulates the outcome of an ORM operation like Create, Update, Delete.
type Result struct {
	Error        error // Holds any error that occurred during the operation.
	RowsAffected int64 // Number of rows affected (relevant for Update, Delete).
	LastInsertID int64 // Last insert ID (relevant for Create with auto-increment).

	// We might embed common.Result if its interface matches well later,
	// but defining our own gives more flexibility for ORM-specific results.
	// rawResult common.Result
}
