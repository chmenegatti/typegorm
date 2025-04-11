// metadata/parser.go
package metadata

import (
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
	// Validação inicial da entrada
	if target == nil {
		return nil, fmt.Errorf("metadata.Parse: target (alvo) não pode ser nil")
	}

	// Obtém o reflect.Type base da struct, desreferenciando ponteiros se necessário.
	structType := reflect.TypeOf(target)
	isPointer := false // Guarda se o input original era um ponteiro
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem() // Pega o tipo da struct apontada
		isPointer = true
	}

	// Garante que o tipo base é realmente uma struct.
	if structType.Kind() != reflect.Struct {
		originalKind := reflect.TypeOf(target).Kind() // Pega o Kind original para a msg de erro
		return nil, fmt.Errorf("metadata.Parse: target deve ser uma struct ou ponteiro para struct, mas obteve %s", originalKind)
	}

	// --- 1. Verifica o Cache (Leitura Otimizada) ---
	// Tenta ler do cache usando um Read Lock, permitindo leituras simultâneas.
	cacheMutex.RLock()
	meta, found := metadataCache[structType]
	cacheMutex.RUnlock() // Libera o Read Lock o mais rápido possível.
	if found {
		// Se encontrou no cache, retorna o resultado cacheado imediatamente.
		fmt.Printf("[LOG-Metadata][Cache] HIT para o tipo: %s\n", structType.Name())
		return meta, nil
	}

	// --- 2. Cache Miss - Prepara para Parsing e Escrita no Cache ---
	// Adquire um Write Lock. Só uma goroutine pode fazer o parsing de um tipo
	// desconhecido por vez, garantindo a segurança do cache.
	cacheMutex.Lock()
	// Defer garante que o Write Lock será liberado ao final da função,
	// independentemente de como ela sair (sucesso, erro, panic).
	defer cacheMutex.Unlock()

	// --- 3. Double-Check no Cache (Verificação Dupla Essencial) ---
	// Verifica o cache NOVAMENTE após obter o Write Lock. É possível que outra
	// goroutine tenha terminado o parsing e preenchido o cache enquanto esta
	// goroutine esperava pelo Write Lock. Isso evita trabalho duplicado.
	meta, found = metadataCache[structType]
	if found {
		// Se encontrou na segunda verificação, libera o Write Lock (via defer) e retorna.
		fmt.Printf("[LOG-Metadata][Cache] HIT (double-check) para o tipo: %s\n", structType.Name())
		return meta, nil
	}

	// --- 4. Cache Miss Real - Executa o Parsing Detalhado ---
	fmt.Printf("[LOG-Metadata][Cache] MISS. Iniciando parsing para o tipo: %s (Ponteiro original: %v)\n", structType.Name(), isPointer)

	// Inicializa a estrutura principal que armazenará os metadados da entidade.
	entityMeta := &EntityMetadata{
		Name:            structType.Name(),                        // Nome da struct Go.
		Type:            structType,                               // Tipo refletido da struct.
		TableName:       strcase.ToSnake(structType.Name()) + "s", // Nome da tabela inferido (convenção snake_case + 's').
		Columns:         make([]*ColumnMetadata, 0),               // Slice para colunas mapeadas.
		ColumnsByName:   make(map[string]*ColumnMetadata),         // Mapa para acesso rápido por nome de campo Go.
		ColumnsByDBName: make(map[string]*ColumnMetadata),         // Mapa para acesso rápido por nome de coluna DB.
		Relations:       make([]*RelationMetadata, 0),             // Slice para relações mapeadas.
		RelationsByName: make(map[string]*RelationMetadata),       // Mapa para acesso rápido por nome de campo Go da relação.
	}
	fmt.Printf("[LOG-Metadata] Nome da tabela inferido: %s\n", entityMeta.TableName)

	var firstParseError error // Guarda o primeiro erro encontrado durante o parsing das tags.

	// Itera sobre todos os campos definidos na struct Go.
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i) // Obtém informações do campo (nome, tipo, tag, etc.)

		// Pula campos não exportados (privados), que começam com letra minúscula.
		if !field.IsExported() {
			fmt.Printf("[LOG-Metadata] Pulando campo não exportado: %s\n", field.Name)
			continue
		}

		// Obtém o valor da tag "typegorm".
		tagValue := field.Tag.Get("typegorm")

		// Pula campos explicitamente marcados para serem ignorados com `typegorm:"-"`.
		if tagValue == "-" {
			fmt.Printf("[LOG-Metadata] Pulando campo ignorado (tag '-'): %s\n", field.Name)
			continue
		}

		fmt.Printf("[LOG-Metadata] Processando Campo: %s (Tipo Go: %s, Tag: '%s')\n", field.Name, field.Type, tagValue)

		// Variáveis temporárias para armazenar dados parseados para este campo.
		isRelationField := false // Assume inicialmente que não é uma relação.
		// Preenche dados básicos da relação (será usada se 'relation:' for encontrada).
		relationData := RelationMetadata{Entity: entityMeta, FieldName: field.Name, FieldType: field.Type}
		// Preenche dados básicos da coluna (será usada se não for relação ou se tiver tags de coluna).
		columnData := ColumnMetadata{Entity: entityMeta, FieldName: field.Name, FieldType: field.Type, FieldIndex: i, GoType: field.Type.String(), ColumnName: strcase.ToSnake(field.Name), IsNullable: isTypeNullable(field.Type)}

		// --- Parsing das Opções da Tag (separadas por ';') ---
		options := strings.Split(tagValue, ";")
		definedTags := make(map[string]string) // Mapa para detectar tags duplicadas.

		for _, opt := range options {
			opt = strings.TrimSpace(opt)
			if opt == "" {
				continue
			} // Ignora opções vazias (ex: "pk;;autoIncrement")

			// Separa chave:valor (se houver valor após ':')
			var key, value string
			parts := strings.SplitN(opt, ":", 2)
			key = strings.ToLower(strings.TrimSpace(parts[0])) // Chave em minúsculas para case-insensitivity
			if len(parts) == 2 {
				value = strings.TrimSpace(parts[1]) // Valor como string
			}

			// Verifica e marca tags duplicadas para evitar comportamento indefinido.
			if _, exists := definedTags[key]; exists && key != "" {
				parseErr := fmt.Errorf("tag duplicada '%s' no campo %s.%s", key, entityMeta.Name, field.Name)
				if firstParseError == nil {
					firstParseError = parseErr
				} // Guarda o primeiro erro
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				continue // Pula processamento desta tag duplicada
			}
			if key != "" {
				definedTags[key] = value
			} // Marca a tag como vista

			// Processa as chaves conhecidas pelo TypeGorm.
			switch key {
			// --- Tags Comuns de Coluna ---
			case "column":
				columnData.ColumnName = value // Nome da coluna no banco (ex: "nome_completo").
			case "type":
				columnData.DBType = value // Tipo explícito da coluna no DB (ex: "VARCHAR(150)").
			case "size": // Tamanho (para VARCHAR, etc.).
				_, err := fmt.Sscan(value, &columnData.Size)
				if err != nil {
					parseErr := fmt.Errorf("parse 'size' (%s) %s.%s: %w", value, entityMeta.Name, field.Name, err)
					if firstParseError == nil {
						firstParseError = parseErr
					}
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				}
			case "precision": // Precisão (para DECIMAL/NUMERIC).
				_, err := fmt.Sscan(value, &columnData.Precision)
				if err != nil {
					parseErr := fmt.Errorf("parse 'precision' (%s) %s.%s: %w", value, entityMeta.Name, field.Name, err)
					if firstParseError == nil {
						firstParseError = parseErr
					}
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				}
			case "scale": // Escala (para DECIMAL/NUMERIC).
				_, err := fmt.Sscan(value, &columnData.Scale)
				if err != nil {
					parseErr := fmt.Errorf("parse 'scale' (%s) %s.%s: %w", value, entityMeta.Name, field.Name, err)
					if firstParseError == nil {
						firstParseError = parseErr
					}
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				}
			case "primarykey", "pk":
				columnData.IsPrimaryKey = true
				columnData.IsNullable = false // Marca como PK, PKs não podem ser nulas.
			case "autoincrement", "auto_increment", "serial":
				columnData.IsAutoIncrement = true // Marca como auto-incremento do DB.
			case "notnull", "not_null":
				columnData.IsNullable = false // Define como NOT NULL.
			case "nullable":
				columnData.IsNullable = true // Define explicitamente como NULLable.
			case "unique":
				columnData.IsUnique = true // Adiciona constraint UNIQUE simples.
			case "default":
				columnData.DefaultValue = value // Define valor DEFAULT (como string).
			case "index": // Cria índice simples.
				if value != "" {
					columnData.IndexName = value
				} else {
					columnData.IndexName = fmt.Sprintf("idx_%s_%s", entityMeta.TableName, columnData.ColumnName)
				}
			case "uniqueindex": // Cria índice único.
				columnData.IsUnique = true
				if value != "" {
					columnData.UniqueIndexName = value
				} else {
					columnData.UniqueIndexName = fmt.Sprintf("uidx_%s_%s", entityMeta.TableName, columnData.ColumnName)
				}
			case "createdat", "created_at":
				columnData.IsCreatedAt = true // Timestamp de criação automático.
			case "updatedat", "updated_at":
				columnData.IsUpdatedAt = true // Timestamp de atualização automático.
			case "deletedat", "deleted_at":
				columnData.IsDeletedAt = true
				columnData.IsNullable = true // Timestamp para soft delete (deve ser nullable).

			// --- Tags de Relação ---
			case "relation":
				isRelationField = true                          // Marca que este campo Go representa uma relação.
				relationData.RelationType = RelationType(value) // Armazena o tipo (one-to-one, etc.).
				// Validação básica do tipo de relação fornecido.
				switch relationData.RelationType {
				case OneToOne, OneToMany, ManyToOne, ManyToMany: // Tipos válidos
				default: // Tipo inválido
					parseErr := fmt.Errorf("tipo de relação inválido '%s' no campo %s.%s", value, entityMeta.Name, field.Name)
					if firstParseError == nil {
						firstParseError = parseErr
					}
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					isRelationField = false // Desmarca como relação se o tipo for inválido.
				}
			case "joincolumn", "join_column": // Define a(s) coluna(s) de junção (FK).
				if relationData.JoinColumns == nil {
					relationData.JoinColumns = make([]*JoinColumnMetadata, 0, 1)
				}
				// Parsing simples: "fk_coluna" ou "fk_coluna:ref_coluna".
				jcParts := strings.SplitN(value, ":", 2)
				jc := JoinColumnMetadata{ColumnName: strings.TrimSpace(jcParts[0]), ReferencedColumnName: "id"} // Coluna referenciada padrão é "id".
				if len(jcParts) == 2 {
					jc.ReferencedColumnName = strings.TrimSpace(jcParts[1])
				} // Usa ref_coluna se fornecida.
				if jc.ColumnName == "" { // Nome da FK não pode ser vazio.
					parseErr := fmt.Errorf("nome da coluna JoinColumn vazio no campo %s.%s", entityMeta.Name, field.Name)
					if firstParseError == nil {
						firstParseError = parseErr
					}
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				} else {
					relationData.JoinColumns = append(relationData.JoinColumns, &jc)
				}
				relationData.IsOwningSide = true // joinColumn implica que este é o lado "dono" da FK (*ToOne ou ManyToMany).
			case "mappedby", "mapped_by": // Define o lado inverso da relação.
				relationData.MappedByFieldName = value // Nome do campo na entidade ALVO que mapeia de volta.
				relationData.IsOwningSide = false      // mappedBy implica que este é o lado "não dono" (*ToMany ou OneToOne inverso).
			case "jointable", "join_table": // Define a tabela de junção para ManyToMany.
				relationData.JoinTableName = value
				relationData.IsOwningSide = true // Convenção: quem define joinTable é o dono.
				// TODO Futuro: Implementar parsing de tags 'joinColumns' e 'inverseJoinColumns' para colunas explícitas na tabela de junção.

			// Caso padrão para tags não reconhecidas.
			default:
				fmt.Printf("[WARN-Metadata] Tag desconhecida ou não implementada: '%s' no campo %s.%s\n", opt, entityMeta.Name, field.Name)
			}
		} // Fim do loop de opções da tag (;)

		// --- Finaliza Processamento do Campo: Decide se é Coluna ou Relação ---
		if isRelationField {
			// --- Processa como Relação ---

			// --- Validações de conflito e requisitos para relações ---

			// Conflito joinColumn vs mappedBy (igual)
			if len(relationData.JoinColumns) > 0 && relationData.MappedByFieldName != "" {
				parseErr := fmt.Errorf("tags conflitantes 'joinColumn' e 'mappedBy' no campo %s.%s", entityMeta.Name, field.Name)
				if firstParseError == nil {
					firstParseError = parseErr
				}
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				continue // Pula este campo inválido.
			}
			// --- Validação ManyToMany (CORRIGIDA) ---
			if relationData.RelationType == ManyToMany {
				// Lado DONO (sem mappedBy): Precisa ter joinTable
				if relationData.MappedByFieldName == "" && relationData.JoinTableName == "" {
					parseErr := fmt.Errorf("lado dono da relação ManyToMany requer a tag 'joinTable' no campo %s.%s", entityMeta.Name, field.Name)
					if firstParseError == nil {
						firstParseError = parseErr
					}
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					continue // Pula este campo inválido.
				}
				// Lado INVERSO (com mappedBy): NÃO deve ter joinTable
				if relationData.MappedByFieldName != "" && relationData.JoinTableName != "" {
					parseErr := fmt.Errorf("lado inverso (mappedBy) da relação ManyToMany não deve ter a tag 'joinTable' no campo %s.%s", entityMeta.Name, field.Name)
					if firstParseError == nil {
						firstParseError = parseErr
					}
					fmt.Printf("[WARN-Metadata] %v\n", parseErr)
					// Decide se pula ou só ignora a tag joinTable inválida? Pular é mais seguro.
					continue
				}
			}
			// --- Fim Validação ManyToMany ---
			// Valida se joinTable só é usado com ManyToMany.
			if relationData.RelationType != ManyToMany && relationData.JoinTableName != "" {
				parseErr := fmt.Errorf("tag 'joinTable' só é válida para ManyToMany no campo %s.%s", entityMeta.Name, field.Name)
				if firstParseError == nil {
					firstParseError = parseErr
				}
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				continue
			}
			// TODO: Mais validações (ex: mappedBy só em *ToMany/OneToOne inverso, etc.)

			// Determina o tipo da Entidade Alvo da Relação.
			targetType := field.Type // Pega o tipo do campo (ex: []*Post, *Perfil, Perfil, []Autor)
			// Desembrulha ponteiros e slices para encontrar o tipo base da struct.
			if targetType.Kind() == reflect.Ptr || targetType.Kind() == reflect.Slice {
				targetType = targetType.Elem()        // Pega o tipo do elemento (ex: *Post, Perfil, Autor)
				if targetType.Kind() == reflect.Ptr { // Se ainda for ponteiro (slice de ponteiros)
					targetType = targetType.Elem() // Pega o tipo final (ex: Post)
				}
			}
			// Verifica se o tipo final é uma struct.
			if targetType.Kind() != reflect.Struct {
				parseErr := fmt.Errorf("campo de relação '%s' deve ser struct, ponteiro ou slice de struct/ponteiro, tipo final encontrado foi %s", field.Name, targetType.Kind())
				if firstParseError == nil {
					firstParseError = parseErr
				}
				fmt.Printf("[WARN-Metadata] %v\n", parseErr)
				continue // Pula campo com tipo alvo inválido.
			}
			// Armazena o tipo e nome da struct alvo.
			relationData.TargetEntityType = targetType
			relationData.TargetEntityName = targetType.Name()

			// Armazena os metadados da relação na entidade principal.
			entityMeta.Relations = append(entityMeta.Relations, &relationData)
			entityMeta.RelationsByName[field.Name] = &relationData
			fmt.Printf("[LOG-Metadata] Relação '%s' (%s -> %s) adicionada.\n", field.Name, relationData.RelationType, relationData.TargetEntityName)

		} else {
			// --- Processa como Coluna Normal ---
			// Considera como coluna se:
			// 1. Alguma tag de coluna foi definida explicitamente (exceto 'relation')
			// 2. Ou se NÃO for um campo de relação E for um tipo básico (não struct/slice/ptr/etc)
			_, hasRelationTag := definedTags["relation"] // Verifica se a tag relation foi usada
			isComplexGoType := field.Type.Kind() == reflect.Struct || field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Ptr || field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Interface

			// Se teve alguma tag E não for relação, OU se não teve tag NENHUMA e é um tipo simples -> Mapeia como coluna
			shouldMapColumn := (len(definedTags) > 0 && !hasRelationTag) || (len(definedTags) == 0 && !isComplexGoType)

			if shouldMapColumn {
				// Adiciona metadados da coluna à entidade.
				entityMeta.Columns = append(entityMeta.Columns, &columnData)
				entityMeta.ColumnsByName[field.Name] = &columnData
				// Verifica nome de coluna DB duplicado.
				if _, exists := entityMeta.ColumnsByDBName[columnData.ColumnName]; exists {
					fmt.Printf("[WARN-Metadata] Nome de coluna DB duplicado '%s' detectado para o campo %s.\n", columnData.ColumnName, field.Name)
				}
				entityMeta.ColumnsByDBName[columnData.ColumnName] = &columnData
				// Guarda referências para colunas especiais.
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
				fmt.Printf("[LOG-Metadata] Coluna '%s' -> '%s' adicionada (Tags: %v).\n", field.Name, columnData.ColumnName, len(definedTags) > 0)
			} else {
				fmt.Printf("[LOG-Metadata] Campo '%s' pulado (provavelmente struct/slice/ptr sem tags de coluna/relação explícitas).\n", field.Name)
			}
		}

	} // --- Fim do loop de campos da struct ---

	// Retorna erro se ocorreu algum durante o parsing das tags
	if firstParseError != nil {
		// O defer Unlock() vai liberar o Lock de escrita
		return nil, fmt.Errorf("metadata.Parse: erro(s) durante o parsing das tags para %s: %w", structType.Name(), firstParseError)
	}

	// --- 5. Armazena no Cache ---
	metadataCache[structType] = entityMeta
	fmt.Printf("[LOG-Metadata] Metadados para %s armazenados no cache (%d colunas, %d relações).\n", structType.Name(), len(entityMeta.Columns), len(entityMeta.Relations))

	// O defer Unlock() libera o Lock de escrita aqui
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
