// pkg/schema/parser.go
package schema

import (
	// Need this for sql.Null* types check
	"fmt"
	"reflect"
	"sort"
	"strconv" // For parsing size, precision, scale
	"strings"
	"sync"
	"time" // Need this for time.Time check
)

// --- Parser Implementation ---

// Parser handles parsing Go structs into schema.Model.
// Includes caching to avoid redundant parsing.
type Parser struct {
	cache          sync.Map // Cache[reflect.Type]*Model
	namingStrategy NamingStrategy
}

// NewParser creates a new schema parser with the given naming strategy.
// If namingStrategy is nil, DefaultNamingStrategy (snake_case) is used.
func NewParser(namingStrategy NamingStrategy) *Parser {
	if namingStrategy == nil {
		namingStrategy = defaultNamingStrategy
	}
	return &Parser{
		namingStrategy: namingStrategy,
	}
}

// Parse analyzes a struct value or type and returns its ORM schema representation (Model).
// It uses caching for efficiency. Pass a pointer to a struct instance (e.g., &User{}).
func (p *Parser) Parse(value any) (*Model, error) {
	if value == nil {
		return nil, fmt.Errorf("cannot parse nil value")
	}

	reflectValue := reflect.ValueOf(value)
	// If it's a pointer, get the element it points to.
	if reflectValue.Kind() == reflect.Pointer {
		reflectValue = reflectValue.Elem()
	}
	// Ensure we have a struct value.
	if reflectValue.Kind() != reflect.Struct {
		// Handle case where user passes the type directly (e.g., Parse(reflect.TypeOf(User{})))
		if rt, ok := value.(reflect.Type); ok && rt.Kind() == reflect.Struct {
			// Fall through to use the type directly
			reflectValue = reflect.New(rt).Elem() // Create a zero value for type info
		} else {
			return nil, fmt.Errorf("input must be a struct instance or pointer to struct, got %T", value)
		}
	}
	structType := reflectValue.Type()

	// Check cache first
	if cachedModel, ok := p.cache.Load(structType); ok {
		// fmt.Printf("Cache hit for %s\n", structType.Name()) // Debug cache
		return cachedModel.(*Model), nil
	}
	// fmt.Printf("Cache miss for %s, parsing...\n", structType.Name()) // Debug cache

	// Not in cache, parse it
	model := &Model{
		Name:           structType.Name(),
		Type:           structType,
		Fields:         make([]*Field, 0, structType.NumField()),
		FieldsByName:   make(map[string]*Field),
		FieldsByDBName: make(map[string]*Field),
		PrimaryKeys:    make([]*Field, 0),
		Indexes:        make([]*Index, 0),
		instance:       reflect.New(structType).Interface(),
		NamingStrategy: p.namingStrategy,
	}
	model.TableName = p.namingStrategy.TableName(model.Name)

	// Temporary maps to build indexes before creating Index structs
	indexesByName := make(map[string][]*Field)       // map[index_name][]Field
	uniqueIndexesByName := make(map[string][]*Field) // map[unique_index_name][]Field

	// Iterate through struct fields using NumField() and Field() from reflect.Type
	for i := 0; i < structType.NumField(); i++ {
		structField := structType.Field(i)

		// Skip unexported fields (like fields starting with lowercase letter)
		if !structField.IsExported() {
			continue
		}

		// TODO: Handle embedded structs later.
		// This requires recursive parsing or flattening fields.
		// if structField.Anonymous { ... }

		field := &Field{
			StructField: structField,
			GoName:      structField.Name,
			GoType:      structField.Type,
			Tags:        make(map[string]string), // Initialize tag map
		}

		// Initial Nullability Check based on Go Type
		kind := field.GoType.Kind()
		field.Nullable = (kind == reflect.Pointer || kind == reflect.Interface || kind == reflect.Map || kind == reflect.Slice)
		// Check for sql.Null* types
		if field.GoType.PkgPath() == "database/sql" && strings.HasPrefix(field.GoType.Name(), "Null") {
			field.Nullable = true
		}
		// Check for time.Time (common case, usually not nullable by default)
		if field.GoType == reflect.TypeOf(time.Time{}) || field.GoType == reflect.TypeOf((*time.Time)(nil)).Elem() {
			// Nullability depends on whether it's *time.Time (pointer) or time.Time (value)
			field.Nullable = (kind == reflect.Pointer)
		}

		// Parse the 'typegorm' tag
		tag := structField.Tag.Get("typegorm")
		if err := p.parseTag(field, tag); err != nil {
			return nil, fmt.Errorf("error parsing tag for field %s.%s: %w", model.Name, field.GoName, err)
		}

		// Skip ignored fields after tag parsing
		if field.IsIgnored {
			continue
		}

		// Determine final DB column name
		if field.DBName == "" { // If not overridden by tag "column:..."
			field.DBName = p.namingStrategy.ColumnName(field.GoName)
		}

		// Finalize Nullability: "not null" tag forces non-nullable.
		if field.IsRequired { // IsRequired comes from "not null" tag
			field.Nullable = false
		}

		// Add field to model collections
		model.Fields = append(model.Fields, field)
		if _, exists := model.FieldsByName[field.GoName]; exists {
			return nil, fmt.Errorf("duplicate Go field name detected: %s in struct %s", field.GoName, model.Name)
		}
		model.FieldsByName[field.GoName] = field

		// Check for DB name collision *before* adding
		if existingField, exists := model.FieldsByDBName[field.DBName]; exists {
			return nil, fmt.Errorf("duplicate DB column name '%s' detected (from fields %s and %s) in struct %s",
				field.DBName, existingField.GoName, field.GoName, model.Name)
		}
		model.FieldsByDBName[field.DBName] = field

		// Collect primary keys
		if field.IsPrimaryKey {
			field.IsRequired = true
			field.Nullable = false // Ensure Nullable is false for PKs
			model.PrimaryKeys = append(model.PrimaryKeys, field)
		} else {
			// 2. For non-PK fields, respect the "not null" tag first.
			if field.IsRequired { // Was set by "not null" tag
				field.Nullable = false
			}
			// 3. Then, respect the "null" tag (explicitly allowing null).
			if field.Nullable { // Set by "null" tag OR inferred from pointer type
				field.IsRequired = false // Explicit "null" overrides any default required status
			}
			// 4. Default for non-PK, non-pointer/nullable-type fields without tags:
			// If field.Nullable is still false (e.g., int, string, bool, time.Time)
			// and field.IsRequired is false (no "not null" tag), we imply NOT NULL.
			if !field.Nullable && !field.IsRequired {
				field.IsRequired = true // Default basic value types to NOT NULL
			}
		}

		// Collect index information temporarily
		for _, indexName := range field.IndexNames {
			indexesByName[indexName] = append(indexesByName[indexName], field)
		}
		for _, uniqueIndexName := range field.UniqueIndexNames {
			uniqueIndexesByName[uniqueIndexName] = append(uniqueIndexesByName[uniqueIndexName], field)
		}
	} // End field loop

	// --- Post-processing ---

	indexesMap := make(map[string]*Index) // Temporary map: map[index_name]*Index

	for _, field := range model.Fields {
		// Process NAMED non-unique indexes first
		for _, indexName := range field.IndexNames {
			if idx, ok := indexesMap[indexName]; ok {
				if idx.IsUnique {
					return nil, fmt.Errorf("index name '%s' used for both unique and non-unique indexes", indexName)
				}
				idx.Fields = append(idx.Fields, field)
			} else {
				indexesMap[indexName] = &Index{Name: indexName, IsUnique: false, Fields: []*Field{field}}
			}
			indexesByName[indexName] = append(indexesByName[indexName], field)
		}
		// Process NAMED unique indexes
		for _, uniqueIndexName := range field.UniqueIndexNames {
			field.Unique = true // Ensure column-level unique is also true
			if idx, ok := indexesMap[uniqueIndexName]; ok {
				if !idx.IsUnique {
					return nil, fmt.Errorf("index name '%s' used for both unique and non-unique indexes", uniqueIndexName)
				}
				idx.Fields = append(idx.Fields, field)
			} else {
				indexesMap[uniqueIndexName] = &Index{Name: uniqueIndexName, IsUnique: true, Fields: []*Field{field}}
			}
		}

		// Process simple 'unique' tag (only if not already part of a NAMED unique index)
		if field.Unique && len(field.UniqueIndexNames) == 0 {
			// *** FIXED: Call generateDefaultIndexName ***
			defaultUniqueName := p.generateDefaultIndexName(model, field, true)
			if idx, ok := indexesMap[defaultUniqueName]; !ok {
				indexesMap[defaultUniqueName] = &Index{Name: defaultUniqueName, IsUnique: true, Fields: []*Field{field}}
			} else {
				if !idx.IsUnique {
					return nil, fmt.Errorf("index name '%s' used for both unique and non-unique indexes", defaultUniqueName)
				}
				idx.Fields = append(idx.Fields, field)
			}
		}

		// Process simple 'index' tag (only if not already part of ANY named index)
		if field.IsIndex && len(field.IndexNames) == 0 && len(field.UniqueIndexNames) == 0 {
			// *** FIXED: Call generateDefaultIndexName ***
			defaultIndexName := p.generateDefaultIndexName(model, field, false)
			if idx, ok := indexesMap[defaultIndexName]; !ok {
				indexesMap[defaultIndexName] = &Index{Name: defaultIndexName, IsUnique: false, Fields: []*Field{field}}
			} else {
				if idx.IsUnique {
					return nil, fmt.Errorf("index name '%s' used for both unique and non-unique indexes", defaultIndexName)
				}
				idx.Fields = append(idx.Fields, field)
			}
		}
	} // End field post-processing loop

	// Add indexes from map to the model's slice
	for _, idx := range indexesMap {
		// Sort fields within composite indexes by Go field name for determinism
		sort.Slice(idx.Fields, func(i, j int) bool { return idx.Fields[i].GoName < idx.Fields[j].GoName })
		model.Indexes = append(model.Indexes, idx)
	}
	// Sort the final list of indexes by name
	sort.Slice(model.Indexes, func(i, j int) bool { return model.Indexes[i].Name < model.Indexes[j].Name })

	// Validate primary keys...
	if len(model.PrimaryKeys) == 0 {
		fmt.Printf("Warning: No primary key specified via tags for model %s\n", model.Name)
	}

	// Store in cache
	p.cache.Store(structType, model)
	return model, nil
}

// parseTag processes the content of the `typegorm` tag string.
func (p *Parser) parseTag(field *Field, tag string) error {
	if tag == "-" {
		field.IsIgnored = true
		return nil
	}
	if tag == "" {
		return nil // No tags to parse
	}

	parts := strings.Split(tag, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kv := strings.SplitN(part, ":", 2)
		key := strings.ToLower(strings.TrimSpace(kv[0])) // Use lowercase key for matching
		var value string
		if len(kv) == 2 {
			value = strings.TrimSpace(kv[1])
			// Keep original case for value (e.g., default:'Active', column:UserID)
		}

		// Store raw tag for potential later use
		field.Tags[key] = value

		// Process known keys
		switch key {
		case "primarykey", "primary_key", "pk":
			field.IsPrimaryKey = true
		case "autoincrement", "auto_increment":
			field.AutoIncrement = true
		case "column", "name":
			if value == "" {
				return fmt.Errorf("tag '%s' requires a value", key)
			}
			field.DBName = value
		case "type":
			if value == "" {
				return fmt.Errorf("tag '%s' requires a value", key)
			}
			field.SQLType = value
		case "size":
			size, err := strconv.Atoi(value)
			if err != nil || size <= 0 {
				return fmt.Errorf("invalid size value '%s' for tag '%s'", value, key)
			}
			field.Size = size
		case "precision":
			precision, err := strconv.Atoi(value)
			if err != nil || precision < 0 {
				return fmt.Errorf("invalid precision value '%s' for tag '%s'", value, key)
			}
			field.Precision = precision
		case "scale":
			scale, err := strconv.Atoi(value)
			if err != nil || scale < 0 {
				return fmt.Errorf("invalid scale value '%s' for tag '%s'", value, key)
			}
			field.Scale = scale
		case "notnull", "not null", "required":
			field.IsRequired = true
		case "null": // Explicitly allow null (overrides Go type non-nullability inference)
			field.Nullable = true
			field.IsRequired = false // Can't be required if explicitly nullable
		case "unique":
			// Simple column-level unique constraint (no value needed)
			field.Unique = true
		case "default":
			// Store raw string value, assumes it's a valid SQL literal or function call
			field.DefaultValue = &value
		case "index":
			field.IsIndex = true // Mark intent
			if value != "" {
				field.IndexNames = append(field.IndexNames, value)
			} // Store explicit name
		case "uniqueindex", "unique_index":
			field.IsUniqueIndex = true // Mark intent
			if value != "" {
				field.UniqueIndexNames = append(field.UniqueIndexNames, value)
			} // Store explicit name
		case "-":
			field.IsIgnored = true
			return nil
		default:
			fmt.Printf("Warning: Unknown tag key '%s' in part '%s' for field %s\n", key, part, field.GoName)
		}
	}

	// Post-tag processing logic moved to main Parse loop after all fields are processed,
	// especially for index generation that might need the final table/column names.
	return nil
}

// generateDefaultIndexName creates a default index name (needs refinement)
// This should ideally take the Model or NamingStrategy as context.
func (p *Parser) generateDefaultIndexName(model *Model, field *Field, unique bool) string {
	prefix := "idx"
	if unique {
		prefix = "uix"
	}
	// Use final determined table and column names from model/field
	name := fmt.Sprintf("%s_%s_%s", prefix, model.TableName, field.DBName)
	maxLen := 60 // Conservative length limit for DB compatibility
	if len(name) > maxLen {
		// Basic truncation, consider hashing for better collision avoidance if needed
		name = name[:maxLen]
	}
	return name
}

// Global parser instance with default settings
var globalParser = NewParser(nil)

// Parse uses the global parser instance. Consider allowing users
// to create parsers with custom NamingStrategy if needed.
func Parse(value any) (*Model, error) {
	return globalParser.Parse(value)
}
