// metadata/parser.go
package metadata

import (
	"fmt"
	"reflect"
	"strconv" // <-- Adicionar import para Atoi
	"strings"
	"sync"

	"github.com/iancoleman/strcase"
)

var (
	metadataCache = make(map[reflect.Type]*EntityMetadata)
	cacheMutex    sync.RWMutex
)

// Parse analisa uma struct Go e retorna seus metadados mapeados, usando um cache.
func Parse(target interface{}) (*EntityMetadata, error) {
	if target == nil {
		return nil, fmt.Errorf("metadata.Parse: target não pode ser nil")
	}

	structType := reflect.TypeOf(target)
	isPointer := false
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
		isPointer = true
	}

	if structType.Kind() != reflect.Struct {
		originalKind := reflect.TypeOf(target).Kind()
		return nil, fmt.Errorf("metadata.Parse: target deve ser uma struct ou ponteiro para struct, mas obteve %s", originalKind)
	}

	// --- Cache Check (Leitura) ---
	cacheMutex.RLock()
	meta, found := metadataCache[structType]
	cacheMutex.RUnlock()
	if found {
		fmt.Printf("[LOG-Metadata][Cache] HIT para o tipo: %s\n", structType.Name())
		return meta, nil
	}

	// --- Cache Miss - Prepara para Escrita ---
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	// --- Double-Check no Cache ---
	meta, found = metadataCache[structType]
	if found {
		fmt.Printf("[LOG-Metadata][Cache] HIT (double-check) para o tipo: %s\n", structType.Name())
		return meta, nil
	}

	// --- Parsing Real ---
	fmt.Printf("[LOG-Metadata][Cache] MISS. Iniciando parsing para o tipo: %s (Ponteiro original: %v)\n", structType.Name(), isPointer)

	entityMeta := &EntityMetadata{
		Name:            structType.Name(),
		Type:            structType,
		Columns:         make([]*ColumnMetadata, 0),
		ColumnsByName:   make(map[string]*ColumnMetadata),
		ColumnsByDBName: make(map[string]*ColumnMetadata),
	}

	// Inferir TableName (Convenção: snake_case + "s")
	entityMeta.TableName = strcase.ToSnake(entityMeta.Name) + "s"
	fmt.Printf("[LOG-Metadata] Nome da tabela inferido: %s\n", entityMeta.TableName)

	var firstParseError error // Guarda o primeiro erro de parsing encontrado

	// Itera sobre os campos
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		if !field.IsExported() {
			continue
		}
		tagValue := field.Tag.Get("typegorm")
		if tagValue == "-" {
			continue
		}

		fmt.Printf("[LOG-Metadata] Processando Campo: %s (Tag: '%s')\n", field.Name, tagValue)

		colMeta := &ColumnMetadata{
			Entity:     entityMeta,
			FieldName:  field.Name,
			FieldType:  field.Type,
			FieldIndex: i,
			GoType:     field.Type.String(),
			ColumnName: strcase.ToSnake(field.Name),
			IsNullable: isTypeNullable(field.Type),
		}

		// Parsing da Tag
		options := strings.Split(tagValue, ";")
		for _, opt := range options {
			opt = strings.TrimSpace(opt)
			if opt == "" {
				continue
			}

			var key, value string
			parts := strings.SplitN(opt, ":", 2)
			key = strings.ToLower(strings.TrimSpace(parts[0]))
			if len(parts) == 2 {
				value = strings.TrimSpace(parts[1])
			}

			switch key {
			case "column":
				colMeta.ColumnName = value
			case "type":
				colMeta.DBType = value
			case "size":
				size, err := strconv.Atoi(value)
				if err != nil {
					parseErr := fmt.Errorf("erro ao parsear 'size' (%s) no campo %s: %w", value, field.Name, err)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					if firstParseError == nil {
						firstParseError = parseErr
					} // Guarda apenas o primeiro erro
				} else {
					colMeta.Size = size
				}
			case "precision":
				precision, err := strconv.Atoi(value)
				if err != nil {
					parseErr := fmt.Errorf("erro ao parsear 'precision' (%s) no campo %s: %w", value, field.Name, err)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					if firstParseError == nil {
						firstParseError = parseErr
					}
				} else {
					colMeta.Precision = precision
				}
			case "scale":
				scale, err := strconv.Atoi(value)
				if err != nil {
					parseErr := fmt.Errorf("erro ao parsear 'scale' (%s) no campo %s: %w", value, field.Name, err)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					if firstParseError == nil {
						firstParseError = parseErr
					}
				} else {
					colMeta.Scale = scale
				}
			case "primarykey", "pk":
				colMeta.IsPrimaryKey = true
				colMeta.IsNullable = false
			case "autoincrement", "auto_increment", "serial":
				colMeta.IsAutoIncrement = true
			case "notnull", "not_null":
				colMeta.IsNullable = false
			case "nullable":
				colMeta.IsNullable = true
			case "unique":
				colMeta.IsUnique = true
			case "default":
				colMeta.DefaultValue = value
			case "index":
				if value != "" {
					colMeta.IndexName = value
				} else {
					colMeta.IndexName = fmt.Sprintf("idx_%s_%s", entityMeta.TableName, colMeta.ColumnName)
				}
			case "uniqueindex":
				colMeta.IsUnique = true
				if value != "" {
					colMeta.UniqueIndexName = value
				} else {
					colMeta.UniqueIndexName = fmt.Sprintf("uidx_%s_%s", entityMeta.TableName, colMeta.ColumnName)
				}
			case "createdat", "created_at":
				colMeta.IsCreatedAt = true
			case "updatedat", "updated_at":
				colMeta.IsUpdatedAt = true
			case "deletedat", "deleted_at":
				colMeta.IsDeletedAt = true
				colMeta.IsNullable = true
			// Relações (relation, joinColumn, etc.) adiadas
			default:
				fmt.Printf("[WARN-Metadata] Tag desconhecida ou não implementada: '%s' no campo %s\n", opt, field.Name)
			}
		} // Fim loop de opções

		// Adiciona metadados da coluna
		entityMeta.Columns = append(entityMeta.Columns, colMeta)
		entityMeta.ColumnsByName[colMeta.FieldName] = colMeta
		if _, exists := entityMeta.ColumnsByDBName[colMeta.ColumnName]; exists {
			fmt.Printf("[WARN-Metadata] Nome de coluna duplicado '%s' detectado para o campo %s.\n", colMeta.ColumnName, colMeta.FieldName)
		}
		entityMeta.ColumnsByDBName[colMeta.ColumnName] = colMeta
		if colMeta.IsPrimaryKey {
			entityMeta.PrimaryKeyColumns = append(entityMeta.PrimaryKeyColumns, colMeta)
		}
		if colMeta.IsCreatedAt {
			entityMeta.CreatedAtColumn = colMeta
		}
		if colMeta.IsUpdatedAt {
			entityMeta.UpdatedAtColumn = colMeta
		}
		if colMeta.IsDeletedAt {
			entityMeta.DeletedAtColumn = colMeta
		}

	} // Fim loop de campos

	// Se ocorreu algum erro durante o parsing das tags, retorna agora
	if firstParseError != nil {
		// O defer Unlock() vai liberar o Lock de escrita
		return nil, fmt.Errorf("metadata.Parse: erro durante o parsing das tags para %s: %w", structType.Name(), firstParseError)
	}

	// --- Armazena no Cache ---
	metadataCache[structType] = entityMeta
	fmt.Printf("[LOG-Metadata] Metadados para %s armazenados no cache.\n", structType.Name())

	// O defer Unlock() libera o Lock de escrita aqui

	return entityMeta, nil
}

// isTypeNullable permanece igual...
func isTypeNullable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return true
	default:
		switch t {
		case nullStringType, nullInt64Type, nullFloat64Type, nullBoolType, nullTimeType:
			return true
		default:
			return false
		}
	}
}

// ClearMetadataCache limpa o cache de metadados. Útil para testes.
// É seguro para uso concorrente.
func ClearMetadataCache() {
	cacheMutex.Lock() // Precisa de Lock de escrita para limpar o mapa
	defer cacheMutex.Unlock()
	metadataCache = make(map[reflect.Type]*EntityMetadata) // Recria o mapa vazio
	fmt.Println("[LOG-Metadata] Cache de metadados limpo.")
}
