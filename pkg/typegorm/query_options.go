package typegorm

import (
	"fmt"
	"strings"
)

// queryOptions holds the optional clauses for a Find query.
type queryOptions struct {
	limit   int    // SQL LIMIT clause
	offset  int    // SQL OFFSET clause
	orderBy string // SQL ORDER BY clause (raw string)
}

// FindOption defines a function type that modifies queryOptions.
type FindOption func(*queryOptions)

// Limit sets the maximum number of records to retrieve.
// Use -1 or 0 to indicate no limit.
func Limit(limit int) FindOption {
	return func(opts *queryOptions) {
		opts.limit = limit
	}
}

// Offset sets the number of records to skip before starting to return records.
// Use 0 for no offset.
func Offset(offset int) FindOption {
	return func(opts *queryOptions) {
		opts.offset = offset
	}
}

// Order specifies the ordering clause for the query.
// Example: Order("user_name ASC, created_at DESC")
// WARNING: The clause is used directly. Ensure column names are correct
// and beware of SQL injection if constructing this from user input.
// Consider adding validation or quoting helpers later.
func Order(clause string) FindOption {
	return func(opts *queryOptions) {
		// Basic validation: prevent obviously malicious content?
		// For now, just trim space. A more robust solution might involve
		// parsing the clause or allowing field names + direction separately.
		trimmedClause := strings.TrimSpace(clause)
		if trimmedClause != "" {
			opts.orderBy = trimmedClause
		}
	}
}

// processFindArgs separates conditions from FindOption functions.
// Returns the condition (if any), the applied options, and an error.
func processFindArgs(args ...any) (any, queryOptions, error) {
	var condition any = nil
	options := queryOptions{limit: -1, offset: 0} // Default: no limit, no offset

	optCount := 0
	condCount := 0

	for _, arg := range args {
		switch v := arg.(type) {
		case FindOption:
			v(&options) // Apply the option function
			optCount++
		default:
			// Assume the first non-option argument is the condition
			if condCount == 0 {
				condition = v // Store the first non-option arg as condition
			}
			condCount++
		}
	}

	// Validate that only one condition argument was provided (if any)
	if condCount > 1 {
		return nil, options, fmt.Errorf("only one condition argument (struct pointer or map) is allowed, got %d", condCount)
	}

	// Validate limit/offset values
	if options.limit < -1 { // Allow -1 for no limit
		options.limit = -1 // Treat negative values other than -1 as no limit
	}
	if options.offset < 0 {
		options.offset = 0 // Treat negative offset as 0
	}

	return condition, options, nil
}
