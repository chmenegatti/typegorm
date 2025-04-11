// metadata/parser.go
package metadata

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/iancoleman/strcase"
)

// metadataCache armazena os resultados do parsing para tipos de struct já processados.
// A chave é o reflect.Type da struct. O acesso é protegido por cacheMutex.
var (
	metadataCache = make(map[reflect.Type]*EntityMetadata)
	cacheMutex    sync.RWMutex // RWMutex permite múltiplas leituras concorrentes, otimizando o acesso ao cache.
)

// Parse analisa uma struct Go (passada como interface{} - ou `any`) e retorna seus metadados
// mapeados para o banco de dados, baseados nas tags 'typegorm'. Utiliza um cache interno
// para evitar re-processamento do mesmo tipo de struct.
// Retorna erro se o input for inválido ou se houver erro irrecuperável no parsing das tags.
// É seguro para uso concorrente.
func Parse(target any) (*EntityMetadata, error) {
	if target == nil {
		return nil, fmt.Errorf("metadata.Parse: target (alvo) não pode ser nil")
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

	// --- 1. Verifica o Cache (sem alterações) ---
	cacheMutex.RLock()
	meta, found := metadataCache[structType]
	cacheMutex.RUnlock()
	if found {
		fmt.Printf("[LOG-Metadata][Cache] HIT para o tipo: %s\n", structType.Name())
		return meta, nil
	}

	// --- 2. Cache Miss - Prepara (sem alterações) ---
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	// --- 3. Double-Check (sem alterações) ---
	meta, found = metadataCache[structType]
	if found {
		fmt.Printf("[LOG-Metadata][Cache] HIT (double-check) para o tipo: %s\n", structType.Name())
		return meta, nil
	}

	// --- 4. Cache Miss Real - Parsing ---
	fmt.Printf("[LOG-Metadata][Cache] MISS. Iniciando parsing para o tipo: %s (Ponteiro original: %v)\n", structType.Name(), isPointer)

	entityMeta := &EntityMetadata{
		Name:            structType.Name(),
		Type:            structType,
		TableName:       strcase.ToSnake(structType.Name()) + "s",
		Columns:         make([]*ColumnMetadata, 0),
		ColumnsByName:   make(map[string]*ColumnMetadata),
		ColumnsByDBName: make(map[string]*ColumnMetadata),
		Relations:       make([]*RelationMetadata, 0),
		RelationsByName: make(map[string]*RelationMetadata),
	}
	fmt.Printf("[LOG-Metadata] Nome da tabela inferido: %s\n", entityMeta.TableName)

	// vvv MUDANÇA 1: Troca firstParseError por slice vvv
	var allParseErrors []error

	// Itera sobre todos os campos definidos na struct Go.
	for i := range structType.NumField() {
		field := structType.Field(i)

		if !field.IsExported() {
			fmt.Printf("[LOG-Metadata] Pulando campo não exportado: %s\n", field.Name)
			continue
		}

		tagValue := field.Tag.Get("typegorm")
		if tagValue == "-" {
			fmt.Printf("[LOG-Metadata] Pulando campo ignorado (tag '-'): %s\n", field.Name)
			continue
		}

		fmt.Printf("[LOG-Metadata] Processando Campo: %s (Tipo Go: %s, Tag: '%s')\n", field.Name, field.Type, tagValue)

		isRelationField := false
		relationData := RelationMetadata{Entity: entityMeta, FieldName: field.Name, FieldType: field.Type}
		columnData := ColumnMetadata{Entity: entityMeta, FieldName: field.Name, FieldType: field.Type, FieldIndex: i, GoType: field.Type.String(), ColumnName: strcase.ToSnake(field.Name), IsNullable: isTypeNullable(field.Type)}

		options := strings.Split(tagValue, ";")
		definedTags := make(map[string]string)

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

			if _, exists := definedTags[key]; exists && key != "" {
				parseErr := fmt.Errorf("tag duplicada '%s' no campo %s.%s", key, entityMeta.Name, field.Name)
				// vvv MUDANÇA 2: Acumula erro vvv
				allParseErrors = append(allParseErrors, parseErr)
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				continue // Pula processamento desta tag duplicada (OK manter)
			}
			if key != "" {
				definedTags[key] = value
			}

			switch key {
			case "column":
				columnData.ColumnName = value
			case "type":
				columnData.DBType = value
			case "size":
				_, err := fmt.Sscan(value, &columnData.Size)
				if err != nil {
					parseErr := fmt.Errorf("parse 'size' (%s) %s.%s: %w", value, entityMeta.Name, field.Name, err)
					// vvv MUDANÇA 3: Acumula erro vvv
					allParseErrors = append(allParseErrors, parseErr)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				}
			case "precision":
				_, err := fmt.Sscan(value, &columnData.Precision)
				if err != nil {
					parseErr := fmt.Errorf("parse 'precision' (%s) %s.%s: %w", value, entityMeta.Name, field.Name, err)
					// vvv MUDANÇA 4: Acumula erro vvv
					allParseErrors = append(allParseErrors, parseErr)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				}
			case "scale":
				_, err := fmt.Sscan(value, &columnData.Scale)
				if err != nil {
					parseErr := fmt.Errorf("parse 'scale' (%s) %s.%s: %w", value, entityMeta.Name, field.Name, err)
					// vvv MUDANÇA 5: Acumula erro vvv
					allParseErrors = append(allParseErrors, parseErr)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				}
			case "primarykey", "pk":
				columnData.IsPrimaryKey = true
				columnData.IsNullable = false
			case "autoincrement", "auto_increment", "serial":
				columnData.IsAutoIncrement = true
			case "notnull", "not_null":
				columnData.IsNullable = false
			case "nullable":
				columnData.IsNullable = true
			case "unique":
				columnData.IsUnique = true
			case "default":
				columnData.DefaultValue = value
			case "index":
				if value != "" {
					columnData.IndexName = value
				} else {
					columnData.IndexName = fmt.Sprintf("idx_%s_%s", entityMeta.TableName, columnData.ColumnName)
				}
			case "uniqueindex":
				columnData.IsUnique = true
				if value != "" {
					columnData.UniqueIndexName = value
				} else {
					columnData.UniqueIndexName = fmt.Sprintf("uidx_%s_%s", entityMeta.TableName, columnData.ColumnName)
				}
			case "createdat", "created_at":
				columnData.IsCreatedAt = true
			case "updatedat", "updated_at":
				columnData.IsUpdatedAt = true
			case "deletedat", "deleted_at":
				columnData.IsDeletedAt = true
				columnData.IsNullable = true

			case "relation":
				isRelationField = true
				relationData.RelationType = RelationType(value)
				switch relationData.RelationType {
				case OneToOne, OneToMany, ManyToOne, ManyToMany:
				default:
					parseErr := fmt.Errorf("tipo de relação inválido '%s' no campo %s.%s", value, entityMeta.Name, field.Name)
					// vvv MUDANÇA 6: Acumula erro vvv
					allParseErrors = append(allParseErrors, parseErr)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					isRelationField = false // Desmarca para não processar como relação
				}
			case "joincolumn", "join_column":
				if relationData.JoinColumns == nil {
					relationData.JoinColumns = make([]*JoinColumnMetadata, 0, 1)
				}
				jcParts := strings.SplitN(value, ":", 2)
				jc := JoinColumnMetadata{ColumnName: strings.TrimSpace(jcParts[0]), ReferencedColumnName: "id"}
				if len(jcParts) == 2 {
					jc.ReferencedColumnName = strings.TrimSpace(jcParts[1])
				}
				if jc.ColumnName == "" {
					parseErr := fmt.Errorf("nome da coluna JoinColumn vazio no campo %s.%s", entityMeta.Name, field.Name)
					// vvv MUDANÇA 7: Acumula erro vvv
					allParseErrors = append(allParseErrors, parseErr)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				} else {
					relationData.JoinColumns = append(relationData.JoinColumns, &jc)
				}
				relationData.IsOwningSide = true
			case "mappedby", "mapped_by":
				relationData.MappedByFieldName = value
				relationData.IsOwningSide = false
			case "jointable", "join_table":
				relationData.JoinTableName = value
				relationData.IsOwningSide = true
				// TODO Futuro: Implementar parsing de tags 'joinColumns' e 'inverseJoinColumns'

			default:
				fmt.Printf("[WARN-Metadata] Tag desconhecida ou não implementada: '%s' no campo %s.%s\n", opt, entityMeta.Name, field.Name)
			}
		} // Fim do loop de opções da tag (;)

		// --- Finaliza Processamento do Campo ---
		if isRelationField {
			// --- Validações de conflito e requisitos ---
			hasFatalRelationError := false // Flag para pular adição da relação

			if len(relationData.JoinColumns) > 0 && relationData.MappedByFieldName != "" {
				parseErr := fmt.Errorf("tags conflitantes 'joinColumn' e 'mappedBy' no campo %s.%s", entityMeta.Name, field.Name)
				// vvv MUDANÇA 8: Acumula erro vvv
				allParseErrors = append(allParseErrors, parseErr)
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				hasFatalRelationError = true // Marca para pular este campo inválido
			}

			if relationData.RelationType == ManyToMany {
				if relationData.MappedByFieldName == "" && relationData.JoinTableName == "" {
					parseErr := fmt.Errorf("lado dono da relação ManyToMany requer a tag 'joinTable' no campo %s.%s", entityMeta.Name, field.Name)
					// vvv MUDANÇA 9: Acumula erro vvv
					allParseErrors = append(allParseErrors, parseErr)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					hasFatalRelationError = true
				}
				if relationData.MappedByFieldName != "" && relationData.JoinTableName != "" {
					parseErr := fmt.Errorf("lado inverso (mappedBy) da relação ManyToMany não deve ter a tag 'joinTable' no campo %s.%s", entityMeta.Name, field.Name)
					// vvv MUDANÇA 10: Acumula erro vvv
					allParseErrors = append(allParseErrors, parseErr)
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					hasFatalRelationError = true
				}
			}

			if relationData.RelationType != ManyToMany && relationData.JoinTableName != "" {
				parseErr := fmt.Errorf("tag 'joinTable' só é válida para ManyToMany no campo %s.%s", entityMeta.Name, field.Name)
				// vvv MUDANÇA 11: Acumula erro vvv
				allParseErrors = append(allParseErrors, parseErr)
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				hasFatalRelationError = true
			}

			// Determina tipo alvo (código original OK)
			targetType := field.Type
			if targetType.Kind() == reflect.Ptr || targetType.Kind() == reflect.Slice {
				targetType = targetType.Elem()
				if targetType.Kind() == reflect.Ptr {
					targetType = targetType.Elem()
				}
			}
			if targetType.Kind() != reflect.Struct {
				parseErr := fmt.Errorf("campo de relação '%s' deve ser struct, ponteiro ou slice de struct/ponteiro, tipo final encontrado foi %s", field.Name, targetType.Kind())
				// vvv MUDANÇA 12: Acumula erro vvv
				allParseErrors = append(allParseErrors, parseErr)
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				hasFatalRelationError = true // Pula se o tipo alvo não for struct
			} else {
				relationData.TargetEntityType = targetType
				relationData.TargetEntityName = targetType.Name()
			}

			// --- Validações Finais da Relação (Combinação Tipo vs Tags) ---
			// Só executa se não houve erro fatal ANTES desta seção
			if !hasFatalRelationError {
				isValidRelation := true // Assume válido até encontrar erro nesta seção
				switch relationData.RelationType {
				case OneToOne:
					if relationData.IsOwningSide {
						if len(relationData.JoinColumns) == 0 {
							parseErr := fmt.Errorf("lado dono da relação OneToOne requer 'joinColumn' no campo %s.%s", entityMeta.Name, field.Name)
							allParseErrors = append(allParseErrors, parseErr) // Acumula
							fmt.Printf("[WARN-Metadata] %v\n", parseErr)
							isValidRelation = false
						}
						if relationData.MappedByFieldName != "" {
							parseErr := fmt.Errorf("lado dono da relação OneToOne não deve ter 'mappedBy' no campo %s.%s", entityMeta.Name, field.Name)
							allParseErrors = append(allParseErrors, parseErr) // Acumula
							fmt.Printf("[WARN-Metadata] %v\n", parseErr)
							isValidRelation = false
						}
						// joinTable já validado antes
					} else { // Lado inverso
						if relationData.MappedByFieldName == "" {
							parseErr := fmt.Errorf("lado inverso da relação OneToOne requer 'mappedBy' no campo %s.%s", entityMeta.Name, field.Name)
							allParseErrors = append(allParseErrors, parseErr) // Acumula
							fmt.Printf("[WARN-Metadata] %v\n", parseErr)
							isValidRelation = false
						}
						// joinColumn (conflito) já validado antes
						// joinTable já validado antes
					}
				case ManyToOne:
					if len(relationData.JoinColumns) == 0 {
						parseErr := fmt.Errorf("relação ManyToOne requer 'joinColumn' no campo %s.%s", entityMeta.Name, field.Name)
						allParseErrors = append(allParseErrors, parseErr) // Acumula
						fmt.Printf("[WARN-Metadata] %v\n", parseErr)
						isValidRelation = false
					}
					if relationData.MappedByFieldName != "" {
						parseErr := fmt.Errorf("relação ManyToOne não deve ter 'mappedBy' no campo %s.%s", entityMeta.Name, field.Name)
						allParseErrors = append(allParseErrors, parseErr) // Acumula
						fmt.Printf("[WARN-Metadata] %v\n", parseErr)
						isValidRelation = false
					}
					// joinTable já validado antes
				case OneToMany:
					if relationData.MappedByFieldName == "" {
						parseErr := fmt.Errorf("relação OneToMany requer 'mappedBy' no campo %s.%s", entityMeta.Name, field.Name)
						allParseErrors = append(allParseErrors, parseErr) // Acumula
						fmt.Printf("[WARN-Metadata] %v\n", parseErr)
						isValidRelation = false
					}
					if len(relationData.JoinColumns) > 0 {
						parseErr := fmt.Errorf("relação OneToMany não deve ter 'joinColumn' no campo %s.%s", entityMeta.Name, field.Name)
						allParseErrors = append(allParseErrors, parseErr) // Acumula
						fmt.Printf("[WARN-Metadata] %v\n", parseErr)
						isValidRelation = false
					}
					// joinTable já validado antes
				case ManyToMany:
					if relationData.IsOwningSide {
						// joinTable e mappedBy já validados antes
					} else { // Lado inverso
						if relationData.MappedByFieldName == "" {
							parseErr := fmt.Errorf("lado inverso da relação ManyToMany requer 'mappedBy' no campo %s.%s", entityMeta.Name, field.Name)
							allParseErrors = append(allParseErrors, parseErr) // Acumula
							fmt.Printf("[WARN-Metadata] %v\n", parseErr)
							isValidRelation = false
						}
						// joinTable e joinColumn já validados antes
						if len(relationData.InverseJoinColumns) > 0 { // Assuming InverseJoinColumns exists or add check for JoinColumns here too
							parseErr := fmt.Errorf("lado inverso da relação ManyToMany não deve ter 'joinColumn'/'inverseJoinColumn' no campo %s.%s", entityMeta.Name, field.Name)
							allParseErrors = append(allParseErrors, parseErr) // Acumula
							fmt.Printf("[WARN-Metadata] %v\n", parseErr)
							isValidRelation = false
						}
					}
				} // Fim do switch RelationType

				// Se alguma validação DESTA seção falhou, marca como erro fatal para pular adição
				if !isValidRelation {
					hasFatalRelationError = true
				}
			} // Fim if !hasFatalRelationError (antes das validações finais)

			// Se houve algum erro fatal (inicial ou final), pula a adição
			if hasFatalRelationError {
				fmt.Printf("[LOG-Metadata] Relação inválida '%s' pulada devido a erro(s) de validação.\n", field.Name)
				continue // Pula para o próximo campo da struct (OK manter)
			}

			// Armazena os metadados da relação na entidade principal.
			entityMeta.Relations = append(entityMeta.Relations, &relationData)
			entityMeta.RelationsByName[field.Name] = &relationData
			fmt.Printf("[LOG-Metadata] Relação '%s' (%s -> %s) adicionada.\n", field.Name, relationData.RelationType, relationData.TargetEntityName)

		} else {
			// --- Processa como Coluna Normal ---
			// Decide se este campo deve ser mapeado como coluna.

			shouldMapColumn := false
			hasAnyProcessedTag := len(definedTags) > 0 // Verifica se alguma tag foi lida para este campo

			// Condição A: Mapeia se tiver alguma tag que NÃO seja 'relation'
			_, hasRelationTag := definedTags["relation"]
			if hasAnyProcessedTag && !hasRelationTag {
				shouldMapColumn = true
			} else if !hasAnyProcessedTag {
				// Condição B: Nenhuma tag definida. Mapear por padrão?
				kind := field.Type.Kind()
				fieldType := field.Type // Guarda o tipo para verificações

				switch kind {
				// Tipos básicos sempre mapeiam por padrão
				case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
					reflect.Float32, reflect.Float64,
					reflect.String:
					shouldMapColumn = true

				case reflect.Struct:
					// Mapear time.Time e sql.Null* por padrão
					// Assumindo que você tem variáveis timeType, nullBoolType, etc. definidas globalmente ou carregadas.
					// Se não, você precisará carregá-las: ex: timeType = reflect.TypeOf(time.Time{})
					if fieldType == timeType ||
						fieldType == nullBoolType || fieldType == nullFloat64Type ||
						fieldType == nullInt64Type || fieldType == nullStringType ||
						fieldType == nullTimeType {
						shouldMapColumn = true
					} else {
						// Outras structs sem tags não são mapeadas por padrão (podem ser embutidas ou relações não marcadas)
						fmt.Printf("[LOG-Metadata] Campo Struct '%s' (tipo %s) pulado (sem tags explícitas de coluna).\n", field.Name, field.Type)
					}

				case reflect.Slice:
					// Mapear apenas []byte por padrão
					if field.Type.Elem().Kind() == reflect.Uint8 { // É []byte?
						shouldMapColumn = true
					} else {
						// Outros slices sem tags não são mapeados (podem ser relações *ToMany não marcadas)
						fmt.Printf("[LOG-Metadata] Campo Slice '%s' (tipo %s) pulado (sem tags explícitas de coluna/relação).\n", field.Name, field.Type)
					}

				case reflect.Ptr:
					// Mapear ponteiros para tipos que mapearíamos por padrão
					elemType := field.Type.Elem() // Tipo para o qual aponta
					elemKind := elemType.Kind()
					switch elemKind {
					case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
						reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
						reflect.Float32, reflect.Float64,
						reflect.String:
						shouldMapColumn = true // Ponteiro para tipo básico
					case reflect.Struct:
						// Mapear ponteiro para time.Time ou sql.Null*
						if elemType == timeType ||
							elemType == nullBoolType || elemType == nullFloat64Type ||
							elemType == nullInt64Type || elemType == nullStringType ||
							elemType == nullTimeType {
							shouldMapColumn = true
						} else {
							// Pointers para outras structs não mapeados por padrão (podem ser relações *ToOne)
							fmt.Printf("[LOG-Metadata] Campo Ptr para Struct '%s' (tipo %s) pulado (sem tags explícitas de coluna/relação).\n", field.Name, field.Type)
						}
					default:
						// Ponteiros para slice, map, etc. não mapeados por padrão
						fmt.Printf("[LOG-Metadata] Campo Ptr '%s' (tipo %s) pulado (aponta para tipo não mapeado por padrão).\n", field.Name, field.Type)
					}
				default:
					// Map, Interface, Func, Chan, etc., não mapeados por padrão
					fmt.Printf("[LOG-Metadata] Campo '%s' (tipo %s) pulado (tipo não mapeado por padrão).\n", field.Name, field.Type)
				}
			} // Fim Condição B (sem tags)

			// Adiciona a coluna se decidido que deve ser mapeada
			if shouldMapColumn {
				entityMeta.Columns = append(entityMeta.Columns, &columnData)
				entityMeta.ColumnsByName[field.Name] = &columnData
				if _, exists := entityMeta.ColumnsByDBName[columnData.ColumnName]; exists {
					fmt.Printf("[WARN-Metadata] Nome de coluna DB duplicado '%s' detectado (campos: %s, %s?).\n", columnData.ColumnName, entityMeta.ColumnsByDBName[columnData.ColumnName].FieldName, field.Name)
				}
				entityMeta.ColumnsByDBName[columnData.ColumnName] = &columnData

				// Guarda referências (código original OK)
				if columnData.IsPrimaryKey {
					entityMeta.PrimaryKeyColumns = append(entityMeta.PrimaryKeyColumns, &columnData)
				}
				if columnData.IsCreatedAt {
					entityMeta.CreatedAtColumn = &columnData
				}
				if columnData.IsUpdatedAt {
					entityMeta.UpdatedAtColumn = &columnData
				}
				if columnData.IsDeletedAt {
					entityMeta.DeletedAtColumn = &columnData
				}
				fmt.Printf("[LOG-Metadata] Coluna '%s' -> '%s' adicionada.\n", field.Name, columnData.ColumnName)
			} else if !isRelationField {
				// Log de campo pulado agora é feito dentro dos cases acima onde a decisão é tomada.
				// Se chegou aqui e não mapeou, a razão já foi logada (ou é um tipo realmente não mapeável como Map, Func).
			}
		} // Fim do else (Processa como Coluna Normal)

	} // --- Fim do loop de campos da struct ---

	// --- 5. Armazena no Cache (se não houver erros) ---
	// vvv MUDANÇA 13: Verifica a slice e retorna erro agregado vvv
	if len(allParseErrors) > 0 {
		// Monta o erro agregado usando errors.Join (Go 1.20+)
		finalError := errors.Join(allParseErrors...)
		// Retorna um erro principal informando o número de problemas encontrados
		return nil, fmt.Errorf("metadata.Parse: %d erro(s) durante o parsing das tags para %s: %w", len(allParseErrors), structType.Name(), finalError)
	}

	// Só armazena no cache se não houve erros.
	metadataCache[structType] = entityMeta
	fmt.Printf("[LOG-Metadata] Metadados para %s armazenados no cache (%d colunas, %d relações).\n", structType.Name(), len(entityMeta.Columns), len(entityMeta.Relations))

	return entityMeta, nil
}

// isTypeNullable infere se um tipo Go deve ser anulável no banco por padrão.
// Simplificação inicial: ponteiros, interfaces, maps, slices, channels, funcs
// e tipos sql.Null* são anuláveis. Tipos básicos (int, string, bool, time.Time) não são.
func isTypeNullable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return true // Tipos referência/ponteiro são inerentemente anuláveis (valor zero é nil).
	default:
		// Verifica tipos específicos sql.Null*
		switch t {
		case nullStringType, nullInt64Type, nullFloat64Type, nullBoolType, nullTimeType:
			return true // Tipos Null* são explicitamente anuláveis.
		default:
			return false // Tipos valor (int, string, bool, time.Time, structs) não são anuláveis por padrão.
		}
	}
}

// ClearMetadataCache limpa o cache de metadados. Útil para testes ou recarga dinâmica (raro).
// É seguro para uso concorrente.
func ClearMetadataCache() {
	cacheMutex.Lock() // Precisa de Lock de escrita para recriar o mapa.
	defer cacheMutex.Unlock()
	metadataCache = make(map[reflect.Type]*EntityMetadata) // Recria o mapa vazio.
	fmt.Println("[LOG-Metadata][Cache] Cache de metadados limpo.")
}
