// crud.go
package typegorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/chmenegatti/typegorm/metadata"
	"github.com/iancoleman/strcase"
)

type FindOptions struct {
	// Where: Condições de filtro.
	// ATENÇÃO: Versão inicial SIMPLES. Usa um mapa onde a CHAVE é a condição SQL
	// (incluindo placeholder se necessário) e o VALOR é o argumento para essa condição.
	// As condições são unidas por AND. A ordem de aplicação não é garantida pelo mapa,
	// mas a ordem dos argumentos passados ao banco corresponderá a uma ordem alfabética das chaves.
	// Exemplo: map[string]any{"status = ?": 1, "nome_modelo LIKE ?": "Teste%"}
	// TODO: Evoluir para uma representação mais robusta de WHERE (structs, interfaces).
	Where map[string]any

	// OrderBy: Define a ordenação. Slice de strings.
	// Ex: []string{"nome_modelo ASC", "id DESC"}
	// TODO: Adicionar sanitização/validação dos nomes de coluna e direções (ASC/DESC).
	OrderBy []string

	// Limit: Número máximo de registros a retornar (>= 0). 0 significa sem limite.
	Limit int

	// Offset: Número de registros a pular (para paginação) (>= 0).
	Offset int
}

// --- Placeholder Helper ---

// getPlaceholder retorna a string de placeholder correta para o driver e índice fornecidos.
// Índices são base 0 (para @p1, $1, o índice é 0).
func getPlaceholder(driverType DriverType, index int) string {
	switch driverType {
	case Postgres:
		// PostgreSQL usa placeholders posicionais baseados em 1 ($1, $2, ...)
		return fmt.Sprintf("$%d", index+1)
	case SQLServer:
		// SQL Server com go-mssqldb e database/sql funciona bem com @pX nomeado/ordinal
		return fmt.Sprintf("@p%d", index+1)
	case SQLite, MySQL: // MySQL e SQLite geralmente usam ?
		// O driver lida com a ordem posicional do '?'
		return "?"
	default:
		// Fallback seguro, mas pode causar erro se não for suportado
		fmt.Printf("[WARN] getPlaceholder: DriverType %s desconhecido, usando '?' como placeholder.\n", driverType)
		return "?"
	}
}

// --- Funções CRUD ---

// Insert insere uma nova entidade no banco de dados.
// 'entity' deve ser um ponteiro para uma struct mapeável.
// Usa 'any' como alias para interface{}.
func Insert(ctx context.Context, ds DataSource, entity any) error {
	// 1. Valida Input e Obtém Metadados
	entityValue := reflect.ValueOf(entity)
	if entityValue.Kind() != reflect.Ptr || entityValue.IsNil() {
		return fmt.Errorf("typegorm.Insert: 'entity' deve ser ponteiro não-nilo para struct, obteve %T", entity)
	}
	entityStructValue := entityValue.Elem()
	if entityStructValue.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.Insert: 'entity' aponta para %s, não struct", entityStructValue.Kind())
	}

	meta, err := metadata.Parse(entityStructValue.Interface()) // Usa cache interno
	if err != nil {
		return fmt.Errorf("typegorm.Insert: erro metadata %T: %w", entity, err)
	}

	// Obtém o tipo do driver para gerar placeholders corretos
	driverType := ds.GetDriverType()

	// 2. Construir Query e Ordem das Colunas
	sqlQuery, columnOrder, err := buildInsertQuery(meta, driverType)
	if err != nil {
		return fmt.Errorf("typegorm.Insert: build query: %w", err)
	}
	fmt.Printf("[LOG-CRUD] Insert Query (%s) para %s: %s\n", driverType, meta.Name, sqlQuery)

	// 3. Construir Argumentos
	args, err := buildInsertArgs(entityStructValue, meta, columnOrder)
	if err != nil {
		return fmt.Errorf("typegorm.Insert: build args: %w", err)
	}
	// Log com cuidado - pode conter dados sensíveis
	fmt.Printf("[LOG-CRUD] Insert Args para %s: %v\n", meta.Name, args)

	// 4. Executar Query - Ponto crítico para a depuração do erro UNIQUE
	fmt.Printf("[DEBUG-CRUD] Insert: Executando ExecContext com query: %s | args: %v\n", sqlQuery, args) // Log de Debug ANTES
	result, err := ds.ExecContext(ctx, sqlQuery, args...)
	// Log de Debug DEPOIS - crucial ver o valor de 'err' retornado aqui
	fmt.Printf("[DEBUG-CRUD] Insert: ExecContext retornou: result=%v, err=%v\n", result, err)

	if err != nil {
		// Se err NÃO for nil (caminho esperado para erro UNIQUE)
		fmt.Printf("[DEBUG-CRUD] Insert: Erro detectado em ExecContext: %v\n", err)
		// Retorna o erro original encapsulado
		return fmt.Errorf("typegorm.Insert: falha exec (%s) para %s [%s]: %w", driverType, meta.Name, sqlQuery, err)
	}
	// Se chegar aqui, significa que err retornado por ExecContext foi nil
	fmt.Printf("[LOG-CRUD] Insert ExecContext para %s bem-sucedido (err foi nil).\n", meta.Name)

	// 5. Tratar LastInsertId (Só é chamado se ExecContext NÃO retornou erro)
	err = handleLastInsertID(entityValue, meta, result, driverType) // Passa ponteiro original e driverType
	if err != nil {
		// Loga como aviso, pois o Insert no banco (aparentemente) funcionou,
		// mas não conseguimos/precisamos pegar o ID de volta.
		fmt.Printf("[WARN] typegorm.Insert: erro/aviso LastInsertId (%s) para %s: %v\n", driverType, meta.Name, err)
	}

	return nil // Retorna nil, indicando sucesso (baseado no retorno do ExecContext)
}

// Find busca múltiplos registros no banco de dados e preenche o slice fornecido.
// 'slicePtr' deve ser um ponteiro para um slice de structs mapeáveis (ex: &[]Usuario{}).
// O slice existente será resetado antes de ser preenchido.
// 'opts' pode ser nil ou conter opções de filtro, ordenação e paginação.
// Retorna erro em caso de falha.
func Find(ctx context.Context, ds DataSource, slicePtr any, opts *FindOptions) error {
	// 1. Valida Input e Obtém Tipo do Elemento do Slice
	slicePtrValue := reflect.ValueOf(slicePtr)
	if slicePtrValue.Kind() != reflect.Ptr || slicePtrValue.IsNil() {
		return fmt.Errorf("typegorm.Find: slicePtr deve ser um ponteiro não-nilo para um slice, obteve %T", slicePtr)
	}
	sliceValue := slicePtrValue.Elem() // Valor do slice em si
	if sliceValue.Kind() != reflect.Slice {
		return fmt.Errorf("typegorm.Find: slicePtr deve apontar para um slice, mas aponta para %s", sliceValue.Kind())
	}

	// Obtém o tipo do *elemento* do slice (ex: Usuario a partir de []Usuario)
	elementType := sliceValue.Type().Elem()
	if elementType.Kind() == reflect.Ptr { // Se for slice de ponteiros (ex: []*Usuario), pega o tipo da struct
		elementType = elementType.Elem()
	}
	if elementType.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.Find: slice deve ser de structs ou ponteiros para structs, mas elementos são %s", elementType.Kind())
	}

	// 2. Obtém Metadados e DriverType
	// Cria uma instância zerada do tipo do elemento para passar ao Parse
	// Nota: Se elementType for de um pacote não importado diretamente, Parse pode falhar.
	// Isso geralmente não é problema se o usuário define o tipo no mesmo projeto.
	zeroElement := reflect.New(elementType).Interface()
	meta, err := metadata.Parse(zeroElement)
	if err != nil {
		return fmt.Errorf("typegorm.Find: erro ao obter metadados para tipo %s: %w", elementType.Name(), err)
	}

	driverType := ds.GetDriverType()
	fmt.Printf("[LOG-CRUD] Find: Buscando %s...\n", meta.Name)

	// 3. Construir Query SELECT com Opções
	sqlQuery, args, columnOrder, err := buildSelectQuery(meta, opts, driverType)
	if err != nil {
		return fmt.Errorf("typegorm.Find: erro ao construir query para %s: %w", meta.Name, err)
	}
	fmt.Printf("[LOG-CRUD] Find Query (%s) para %s: %s\n", driverType, meta.Name, sqlQuery)
	fmt.Printf("[LOG-CRUD] Find Args para %s: %v\n", meta.Name, args)

	// 4. Executar QueryContext
	rows, err := ds.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return fmt.Errorf("typegorm.Find: falha na execução da query (%s) para %s: %w", driverType, meta.Name, err)
	}
	defer rows.Close() // Garante que rows seja fechado

	// 5. Processar Resultados e Preencher o Slice
	// Reseta o slice original para garantir que ele contenha apenas os resultados desta busca
	sliceValue.Set(reflect.MakeSlice(sliceValue.Type(), 0, 0)) // slice = make([]TipoElemento, 0)

	fmt.Printf("[LOG-CRUD] Find: Iterando sobre os resultados para %s...\n", meta.Name)
	rowIndex := 0
	for rows.Next() {
		// Cria uma nova instância Endereçável do tipo do elemento do slice
		// Ex: Se slice for []Usuario, cria um ponteiro para um novo Usuario.
		// Usamos Elem() depois para obter o valor da struct onde Scan irá escrever.
		newElementPtr := reflect.New(elementType) // Retorna um ponteiro (ex: *Usuario)
		newElementValue := newElementPtr.Elem()   // Obtém o valor da struct (ex: Usuario)

		// Prepara os destinos do Scan (ponteiros para os campos da nova instância)
		scanDest, err := buildScanDest(newElementValue, meta, columnOrder) // Passa o valor da struct
		if err != nil {
			// Erro ao preparar destinos é fatal para o processamento
			return fmt.Errorf("typegorm.Find: erro ao preparar destino do Scan na linha %d para %s: %w", rowIndex, meta.Name, err)
		}

		// Executa o Scan nos ponteiros preparados
		if err := rows.Scan(scanDest...); err != nil {
			// Erro no scan de uma linha específica
			// Poderíamos logar e continuar, ou retornar erro (mais seguro?)
			return fmt.Errorf("typegorm.Find: falha no Scan na linha %d para %s: %w", rowIndex, meta.Name, err)
		}

		// Adiciona a nova instância preenchida ao slice original
		// Se o slice original for de ponteiros (ex: []*Usuario), adicionamos newElementPtr.
		// Se for de valores (ex: []Usuario), adicionamos newElementValue.
		if sliceValue.Type().Elem().Kind() == reflect.Ptr {
			sliceValue.Set(reflect.Append(sliceValue, newElementPtr))
		} else {
			sliceValue.Set(reflect.Append(sliceValue, newElementValue))
		}
		rowIndex++
	}

	// Verifica erro final do cursor após o loop
	if err := rows.Err(); err != nil {
		return fmt.Errorf("typegorm.Find: erro do cursor após iteração para %s: %w", meta.Name, err)
	}

	fmt.Printf("[LOG-CRUD] Find: Finalizado. %d registros encontrados e carregados para %s.\n", rowIndex, meta.Name)
	return nil // Sucesso
}

// FindByID busca uma entidade pelo seu ID e carrega os dados em entityPtr.
// 'entityPtr' deve ser um ponteiro para uma struct mapeável (ex: &Usuario{}).
// 'id' é o valor da chave primária a ser buscada (pode ser de qualquer tipo compatível).
// Retorna sql.ErrNoRows se não encontrado, ou outro erro se ocorrer falha.
func FindByID(ctx context.Context, ds DataSource, entityPtr any, id any) error {
	ptrValue := reflect.ValueOf(entityPtr)
	if ptrValue.Kind() != reflect.Ptr || ptrValue.IsNil() {
		return fmt.Errorf("typegorm.FindByID: entityPtr deve ser ponteiro não-nilo para struct, obteve %T", entityPtr)
	}
	structValue := ptrValue.Elem()
	if structValue.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.FindByID: entityPtr aponta para %s, não struct", structValue.Kind())
	}
	structType := structValue.Type()
	meta, err := metadata.Parse(structValue.Interface())
	if err != nil {
		return fmt.Errorf("typegorm.FindByID: erro metadata %s: %w", structType.Name(), err)
	}

	// Valida PK
	if len(meta.PrimaryKeyColumns) != 1 {
		return fmt.Errorf("typegorm.FindByID: entidade %s não tem PK única definida", meta.Name)
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	fmt.Printf("[LOG-CRUD] FindByID: Buscando %s por PK '%s'\n", meta.Name, pkColumn.ColumnName)

	// Obtém o tipo do driver para gerar placeholders corretos
	driverType := ds.GetDriverType()

	// Constrói Query SELECT com placeholder correto
	sqlQuery, columnOrder, err := buildSelectByIDQuery(meta, pkColumn, driverType) // Passa driverType
	if err != nil {
		return fmt.Errorf("typegorm.FindByID: build query: %w", err)
	}
	fmt.Printf("[LOG-CRUD] FindByID Query (%s) para %s: %s\n", driverType, meta.Name, sqlQuery)

	// Executa QueryRowContext
	row := ds.QueryRowContext(ctx, sqlQuery, id) // Passa apenas o valor do ID
	fmt.Printf("[LOG-CRUD] FindByID executou QueryRowContext para %s com ID %v\n", meta.Name, id)

	// Prepara Destinos para Scan
	scanDest, err := buildScanDest(structValue, meta, columnOrder) // Passa o valor da struct
	if err != nil {
		return fmt.Errorf("typegorm.FindByID: build scan dest: %w", err)
	}
	fmt.Printf("[LOG-CRUD] FindByID: %d destinos preparados para Scan.\n", len(scanDest))

	// Executa Scan
	err = row.Scan(scanDest...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Printf("[LOG-CRUD] FindByID: Não encontrado %s com ID %v\n", meta.Name, id)
			return sql.ErrNoRows
		}
		return fmt.Errorf("typegorm.FindByID: falha scan (%s) para %s [%s]: %w", driverType, meta.Name, sqlQuery, err)
	}

	fmt.Printf("[LOG-CRUD] FindByID: Scan OK para %s com ID %v.\n", meta.Name, id)
	return nil
}

// Update atualiza um registro existente no banco de dados com base na entidade fornecida.
// 'entity' deve ser um ponteiro para uma struct mapeável com a PK preenchida.
// Atualiza todas as colunas não-PK, incluindo 'updatedAt'.
// Retorna erro se a PK não for encontrada ou se a atualização falhar.
func Update(ctx context.Context, ds DataSource, entity any) error {
	entityValue := reflect.ValueOf(entity)
	if entityValue.Kind() != reflect.Ptr || entityValue.IsNil() {
		return fmt.Errorf("typegorm.Update: 'entity' deve ser ponteiro não-nilo para struct, obteve %T", entity)
	}
	entityStructValue := entityValue.Elem()
	if entityStructValue.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.Update: 'entity' aponta para %s, não struct", entityStructValue.Kind())
	}
	meta, err := metadata.Parse(entityStructValue.Interface())
	if err != nil {
		return fmt.Errorf("typegorm.Update: erro metadata %T: %w", entity, err)
	}

	// Valida e Extrai PK
	if len(meta.PrimaryKeyColumns) != 1 {
		return fmt.Errorf("typegorm.Update: entidade %s não tem PK única", meta.Name)
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	pkField := entityStructValue.Field(pkColumn.FieldIndex)
	if !pkField.IsValid() {
		return fmt.Errorf("typegorm.Update: campo PK '%s' inválido", pkColumn.FieldName)
	}
	pkValue := pkField.Interface()
	if reflect.ValueOf(pkValue).IsZero() {
		return fmt.Errorf("typegorm.Update: PK '%s' zerada/nula", pkColumn.FieldName)
	}
	fmt.Printf("[LOG-CRUD] Update: Atualizando %s com PK '%s' = %v\n", meta.Name, pkColumn.ColumnName, pkValue)

	// Obtém o tipo do driver
	driverType := ds.GetDriverType()

	// Constrói Query UPDATE e Argumentos
	updateSQL, updateCols, err := buildUpdateQuery(meta, pkColumn, driverType) // Passa driverType
	if err != nil {
		return fmt.Errorf("typegorm.Update: build query: %w", err)
	}
	updateArgs, err := buildUpdateArgs(entityStructValue, meta, updateCols, pkValue)
	if err != nil {
		return fmt.Errorf("typegorm.Update: build args: %w", err)
	}

	fmt.Printf("[LOG-CRUD] Update Query (%s) para %s: %s\n", driverType, meta.Name, updateSQL)
	fmt.Printf("[LOG-CRUD] Update Args para %s: %v\n", meta.Name, updateArgs)

	// Executa Query
	result, err := ds.ExecContext(ctx, updateSQL, updateArgs...)
	if err != nil {
		return fmt.Errorf("typegorm.Update: falha exec (%s) para %s: %w", driverType, meta.Name, err)
	}
	fmt.Printf("[LOG-CRUD] Update ExecContext para %s OK.\n", meta.Name)

	// Verifica Linhas Afetadas
	rowsAffected, raErr := result.RowsAffected()
	if raErr != nil {
		fmt.Printf("[WARN] typegorm.Update: erro RowsAffected (%s) para %s: %v\n", driverType, meta.Name, raErr)
	} else {
		fmt.Printf("[LOG-CRUD] Update RowsAffected para %s: %d\n", meta.Name, rowsAffected)
		if rowsAffected == 0 {
			return fmt.Errorf("typegorm.Update: registro com PK %v não encontrado ou nenhuma linha alterada para %s", pkValue, meta.Name)
		} // Ou erro customizado
		if rowsAffected > 1 {
			return fmt.Errorf("typegorm.Update: RowsAffected=%d (esperado 0 ou 1) para %s com PK %v", rowsAffected, meta.Name, pkValue)
		}
	}
	return nil
}

// Delete remove um registro do banco de dados.
// Se a entidade tiver a tag `deletedAt`, realiza um Soft Delete (atualiza a coluna).
// Caso contrário, realiza um Hard Delete (DELETE FROM ...).
// 'entity' deve ser um ponteiro para uma struct mapeável com a PK preenchida.
// Retorna erro se a PK não for encontrada ou se a operação falhar.
func Delete(ctx context.Context, ds DataSource, entity any) error {
	entityValue := reflect.ValueOf(entity)
	if entityValue.Kind() != reflect.Ptr || entityValue.IsNil() {
		return fmt.Errorf("typegorm.Delete: 'entity' deve ser ponteiro não-nilo para struct, obteve %T", entity)
	}
	entityStructValue := entityValue.Elem()
	if entityStructValue.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.Delete: 'entity' aponta para %s, não struct", entityStructValue.Kind())
	}
	meta, err := metadata.Parse(entityStructValue.Interface())
	if err != nil {
		return fmt.Errorf("typegorm.Delete: %w", err)
	}

	if len(meta.PrimaryKeyColumns) != 1 {
		return fmt.Errorf("typegorm.Delete: PK ausente/composta não suportada para %s", meta.Name)
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	pkField := entityStructValue.Field(pkColumn.FieldIndex)
	if !pkField.IsValid() {
		return fmt.Errorf("typegorm.Delete: campo PK '%s' inválido", pkColumn.FieldName)
	}
	pkValue := pkField.Interface()
	if reflect.ValueOf(pkValue).IsZero() {
		return fmt.Errorf("typegorm.Delete: PK '%s' zerada/nula", pkColumn.FieldName)
	}
	fmt.Printf("[LOG-CRUD] Delete: Deletando %s com PK '%s' = %v\n", meta.Name, pkColumn.ColumnName, pkValue)

	driverType := ds.GetDriverType() // Obtém tipo do driver
	var query string
	var args []any
	now := time.Now()

	if meta.DeletedAtColumn != nil {
		// --- Soft Delete ---
		fmt.Printf("[LOG-CRUD] Delete: Executando Soft Delete para %s\n", meta.Name)
		placeholderTime := getPlaceholder(driverType, 0) // Placeholder para now
		placeholderPK := getPlaceholder(driverType, 1)   // Placeholder para pkValue
		query = fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s AND %s IS NULL",
			meta.TableName, meta.DeletedAtColumn.ColumnName, placeholderTime,
			pkColumn.ColumnName, placeholderPK, meta.DeletedAtColumn.ColumnName,
		)
		args = []any{now, pkValue}
	} else {
		// --- Hard Delete ---
		fmt.Printf("[LOG-CRUD] Delete: Executando Hard Delete para %s\n", meta.Name)
		placeholderPK := getPlaceholder(driverType, 0) // Placeholder para pkValue
		query = fmt.Sprintf("DELETE FROM %s WHERE %s = %s", meta.TableName, pkColumn.ColumnName, placeholderPK)
		args = []any{pkValue}
	}

	// Executa Query
	fmt.Printf("[LOG-CRUD] Delete Query (%s) para %s: %s\n", driverType, meta.Name, query)
	fmt.Printf("[LOG-CRUD] Delete Args para %s: %v\n", meta.Name, args)
	result, err := ds.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("typegorm.Delete: falha exec (%s) para %s: %w", driverType, meta.Name, err)
	}
	fmt.Printf("[LOG-CRUD] Delete ExecContext para %s OK.\n", meta.Name)

	// Verifica Linhas Afetadas
	rowsAffected, raErr := result.RowsAffected()
	if raErr != nil {
		fmt.Printf("[WARN] typegorm.Delete: erro RowsAffected (%s) para %s: %v\n", driverType, meta.Name, raErr)
	} else {
		fmt.Printf("[LOG-CRUD] Delete RowsAffected para %s: %d\n", meta.Name, rowsAffected)
		if rowsAffected == 0 {
			return fmt.Errorf("typegorm.Delete: registro com PK %v não encontrado ou já deletado para %s", pkValue, meta.Name)
		} // Ou sql.ErrNoRows?
		if rowsAffected > 1 {
			return fmt.Errorf("typegorm.Delete: RowsAffected=%d (esperado 0 ou 1) para %s com PK %v", rowsAffected, meta.Name, pkValue)
		}
	}
	return nil
}

// --- Funções Auxiliares para FindByID ---

// buildSelectByIDQuery constrói "SELECT col1, col2 FROM table WHERE pk = ?"
// Retorna a query e a ordem das colunas selecionadas.
func buildSelectByIDQuery(meta *metadata.EntityMetadata, pk *metadata.ColumnMetadata, driverType DriverType) (string, []*metadata.ColumnMetadata, error) {
	if len(meta.Columns) == 0 {
		return "", nil, errors.New("nenhuma coluna mapeada")
	}
	var selectColumns []string
	var columnOrder []*metadata.ColumnMetadata
	for _, col := range meta.Columns {
		selectColumns = append(selectColumns, col.ColumnName)
		columnOrder = append(columnOrder, col)
	}

	// Usa helper para placeholder correto no WHERE
	whereClause := fmt.Sprintf("%s = %s", pk.ColumnName, getPlaceholder(driverType, 0)) // Índice 0 para PK

	if meta.DeletedAtColumn != nil { // Adiciona filtro soft delete
		whereClause = fmt.Sprintf("%s AND %s IS NULL", whereClause, meta.DeletedAtColumn.ColumnName)
		fmt.Printf("[LOG-CRUD] buildSelectByIDQuery: Adicionando filtro WHERE %s IS NULL\n", meta.DeletedAtColumn.ColumnName)
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s", strings.Join(selectColumns, ", "), meta.TableName, whereClause)
	return query, columnOrder, nil
}

// buildScanDest cria um slice de ponteiros para os campos da struct na ordem correta.
// Recebe structValue (o reflect.Value da struct, NÃO o ponteiro para ela).
// Retorna um slice de `any` (que conterá ponteiros para os campos).
func buildScanDest(structValue reflect.Value, meta *metadata.EntityMetadata, columnOrder []*metadata.ColumnMetadata) ([]any, error) {
	// Garante que estamos trabalhando com a struct, não o ponteiro para ela
	if structValue.Kind() == reflect.Ptr {
		structValue = structValue.Elem()
	}
	// Verificação dupla
	if structValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("valor interno não é uma struct (%s)", structValue.Kind())
	}

	dest := make([]any, len(columnOrder)) // Slice de 'any' para os ponteiros

	for i, col := range columnOrder {
		// Obtém o reflect.Value do campo na struct usando o índice armazenado
		fieldValue := structValue.Field(col.FieldIndex)

		// Verifica se o campo existe e pode ter seu endereço obtido/modificado
		if !fieldValue.IsValid() {
			return nil, fmt.Errorf("campo '%s' inválido na struct destino", col.FieldName)
		}
		if !fieldValue.CanAddr() {
			// Se CanAddr é falso, não podemos pegar o ponteiro para Scan
			// Isso pode acontecer com campos não exportados (já filtrados) ou
			// se a structValue original não for endereçável (raro neste fluxo).
			return nil, fmt.Errorf("não é possível obter o endereço do campo '%s' para Scan", col.FieldName)
		}
		if !fieldValue.CanSet() {
			// Se não podemos definir, Scan não funcionará
			return nil, fmt.Errorf("não é possível definir o valor do campo '%s' (não exportado?)", col.FieldName)
		}

		// Obtém o ponteiro para o campo e o armazena como 'any' no slice
		fieldAddr := fieldValue.Addr()  // Obtém o ponteiro para o campo
		dest[i] = fieldAddr.Interface() // Guarda o ponteiro como interface{} (compatível com any)
	}

	return dest, nil
}

// --- Funções Auxiliares de Insert (permanecem iguais) ---
func buildInsertQuery(meta *metadata.EntityMetadata, driverType DriverType) (string, []*metadata.ColumnMetadata, error) {
	if len(meta.Columns) == 0 {
		return "", nil, errors.New("nenhuma coluna mapeada")
	}
	var columnNames, placeholders []string
	var columnOrder []*metadata.ColumnMetadata
	colIndex := 0
	for _, col := range meta.Columns {
		if col.IsPrimaryKey && col.IsAutoIncrement {
			continue
		}
		columnNames = append(columnNames, col.ColumnName)
		placeholders = append(placeholders, getPlaceholder(driverType, colIndex)) // Usa helper
		columnOrder = append(columnOrder, col)
		colIndex++
	}
	if len(columnNames) == 0 {
		return "", nil, errors.New("nenhuma coluna para inserir")
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", meta.TableName, strings.Join(columnNames, ", "), strings.Join(placeholders, ", "))
	return query, columnOrder, nil
}

func buildInsertArgs(entityStructValue reflect.Value, meta *metadata.EntityMetadata, columnOrder []*metadata.ColumnMetadata) ([]any, error) { /* ... (igual antes, usando any) ... */
	args := make([]any, 0, len(columnOrder))
	now := time.Now()
	for _, col := range columnOrder {
		var argValue any
		if col.IsCreatedAt || col.IsUpdatedAt {
			argValue = now
		} else {
			fieldValue := entityStructValue.Field(col.FieldIndex)
			if !fieldValue.IsValid() {
				return nil, fmt.Errorf("campo '%s' inválido", col.FieldName)
			}
			argValue = fieldValue.Interface()
		}
		args = append(args, argValue)
	}
	return args, nil
}

// handleLastInsertID trata o retorno do LastInsertId para PKs auto-incrementais.
// Atualiza o campo PK na struct com o valor retornado.
// 'entityPtrValue' é o reflect.Value do ponteiro para a struct.
// 'meta' é a metadata da entidade.
// 'result' é o resultado da execução do comando SQL (INSERT).
func handleLastInsertID(entityPtrValue reflect.Value, meta *metadata.EntityMetadata, result sql.Result, driverType DriverType) error { // <-- Aceita driverType
	// Só continua se for PK única e auto-increment
	if len(meta.PrimaryKeyColumns) != 1 || !meta.PrimaryKeyColumns[0].IsAutoIncrement {
		return nil
	}

	// *** ADICIONADO: Checagem específica para SQL Server ***
	if driverType == SQLServer {
		// Informa que não vai tentar buscar o ID automaticamente e retorna sucesso (pois Insert funcionou)
		fmt.Printf("[INFO] typegorm.handleLastInsertID: (%s) LastInsertId() não suportado; ID não será populado automaticamente na struct.\n", driverType)
		return nil // Retorna nil, pois não é um erro do Insert em si.
	}
	// *** FIM CHECAGEM ***

	pkColumn := meta.PrimaryKeyColumns[0]

	// Tenta obter o LastInsertId (para outros drivers)
	fmt.Printf("[LOG-CRUD] handleLastInsertID: (%s) Tentando obter LastInsertId para PK %s...\n", driverType, pkColumn.FieldName)
	lastID, err := result.LastInsertId()
	if err != nil {
		// Se chegar aqui, é um erro inesperado de um driver que *deveria* suportar
		return fmt.Errorf("driver (%s) retornou erro inesperado para LastInsertId: %w", driverType, err)
	}

	if lastID <= 0 {
		return fmt.Errorf("LastInsertId retornou valor inválido: %d", lastID)
	}

	// Define o ID de volta na struct (lógica igual)
	pkField := entityPtrValue.Elem().Field(pkColumn.FieldIndex)
	// ... (resto da lógica com SetUint/SetInt e checagem de overflow) ...
	if !pkField.CanSet() {
		return fmt.Errorf("não pode definir PK '%s'", pkColumn.FieldName)
	}
	switch pkField.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintID := uint64(lastID)
		if pkField.OverflowUint(uintID) {
			return fmt.Errorf("overflow uint PK '%s'", pkColumn.FieldName)
		}
		pkField.SetUint(uintID)
		fmt.Printf("[LOG-CRUD] PK AutoIncrement (%s) definida em %s.%s = %d\n", driverType, meta.Name, pkColumn.FieldName, uintID)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if pkField.OverflowInt(lastID) {
			return fmt.Errorf("overflow int PK '%s'", pkColumn.FieldName)
		}
		pkField.SetInt(lastID)
		fmt.Printf("[LOG-CRUD] PK AutoIncrement (%s) definida em %s.%s = %d\n", driverType, meta.Name, pkColumn.FieldName, lastID)
	default:
		return fmt.Errorf("tipo PK '%s' (%s) não suportado", pkColumn.FieldName, pkField.Kind())
	}

	return nil
}

func buildUpdateQuery(meta *metadata.EntityMetadata, pk *metadata.ColumnMetadata, driverType DriverType) (string, []*metadata.ColumnMetadata, error) {
	var setClauses []string
	var columnOrder []*metadata.ColumnMetadata
	colIndex := 0
	for _, col := range meta.Columns {
		if col.IsPrimaryKey || col.IsCreatedAt {
			continue
		} // Ignora PK e CriadoEm
		setClauses = append(setClauses, fmt.Sprintf("%s = %s", col.ColumnName, getPlaceholder(driverType, colIndex))) // Usa helper
		columnOrder = append(columnOrder, col)
		colIndex++
	}
	if len(setClauses) == 0 {
		return "", nil, errors.New("nenhuma coluna para atualizar")
	}
	pkPlaceholder := getPlaceholder(driverType, colIndex) // Placeholder para PK no WHERE
	whereClause := fmt.Sprintf("%s = %s", pk.ColumnName, pkPlaceholder)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", meta.TableName, strings.Join(setClauses, ", "), whereClause)
	return query, columnOrder, nil
}

// buildInsertArgs, handleLastInsertID, buildScanDest (permanecem iguais)
// buildUpdateArgs (precisa ser criada ou revisada)
func buildUpdateArgs(entityStructValue reflect.Value, meta *metadata.EntityMetadata, columnOrder []*metadata.ColumnMetadata, pkValue any) ([]any, error) {
	args := make([]any, 0, len(columnOrder)+1)
	now := time.Now()
	for _, col := range columnOrder { // Itera sobre colunas no SET
		var argValue any
		if col.IsUpdatedAt {
			argValue = now
		} else {
			fieldValue := entityStructValue.Field(col.FieldIndex)
			if !fieldValue.IsValid() {
				return nil, fmt.Errorf("campo '%s' inválido", col.FieldName)
			}
			argValue = fieldValue.Interface()
		}
		args = append(args, argValue)
	}
	args = append(args, pkValue) // Adiciona PK por último para o WHERE
	return args, nil
}

// buildSelectQuery constrói a query SELECT completa com base nos metadados e opções.
// Retorna a string SQL, os argumentos para os placeholders, a ordem das colunas selecionadas, e um erro.
// buildSelectQuery constrói a query SELECT completa com base nos metadados e opções.
func buildSelectQuery(meta *metadata.EntityMetadata, opts *FindOptions, driverType DriverType) (string, []any, []*metadata.ColumnMetadata, error) {
	if len(meta.Columns) == 0 { /* ... erro ... */
	}

	// 1. Monta SELECT clause (igual antes)
	var selectColumns []string
	var columnOrder []*metadata.ColumnMetadata
	for _, col := range meta.Columns {
		selectColumns = append(selectColumns, col.ColumnName)
		columnOrder = append(columnOrder, col)
	}
	selectClause := strings.Join(selectColumns, ", ")

	// 2. Monta FROM clause (igual antes)
	fromClause := meta.TableName

	// 3. Monta WHERE clause e coleta Args (igual antes)
	whereConditions := []string{}
	var args []any
	if meta.DeletedAtColumn != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("%s IS NULL", meta.DeletedAtColumn.ColumnName))
	}
	if opts != nil && len(opts.Where) > 0 {
		keys := make([]string, 0, len(opts.Where))
		for k := range opts.Where {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, conditionKey := range keys {
			value := opts.Where[conditionKey]
			// Validação básica do placeholder na chave (pode melhorar)
			if !(strings.Contains(conditionKey, "?") || strings.Contains(conditionKey, "$") || strings.Contains(conditionKey, "@p")) {
				// Tenta inferir se for só nome de coluna? Ex: where["nome"] = valor -> "nome = ?"
				// Por simplicidade, mantemos a exigência do placeholder na chave por enquanto.
				return "", nil, nil, fmt.Errorf("condição Where inválida: chave '%s' deve conter placeholder explícito (?, $N, @pX)", conditionKey)
			}
			// Adiciona condição e argumento (confiando na chave por enquanto)
			whereConditions = append(whereConditions, conditionKey)
			args = append(args, value)
		}
	}
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// 4. Monta ORDER BY clause (COM VALIDAÇÃO - MODIFICADO)
	orderByClause := ""
	if opts != nil && len(opts.OrderBy) > 0 {
		var safeOrderClauses []string // Armazena cláusulas validadas

		for _, orderByItem := range opts.OrderBy {
			orderByItem = strings.TrimSpace(orderByItem)
			if orderByItem == "" {
				continue
			}

			// Divide por espaço para obter coluna e direção (opcional)
			parts := strings.Fields(orderByItem)
			if len(parts) == 0 {
				continue
			} // Ignora se for só espaço

			potentialColumnName := parts[0]
			var validDBColumnName string // Nome da coluna validado

			// Valida se a coluna existe nos metadados (case-insensitive com snake_case)
			if colMeta, exists := meta.ColumnsByDBName[potentialColumnName]; exists {
				validDBColumnName = colMeta.ColumnName // Usa o nome exato do DB se encontrado diretamente
			} else {
				// Tenta converter para snake_case e verifica de novo
				inferredSnakeName := strcase.ToSnake(potentialColumnName)
				if colMetaSnake, existsSnake := meta.ColumnsByDBName[inferredSnakeName]; existsSnake {
					validDBColumnName = colMetaSnake.ColumnName // Usa a versão snake_case validada
				} else {
					// Se não encontrou nem direto nem como snake_case, retorna erro
					return "", nil, nil, fmt.Errorf("coluna de ordenação inválida ou não mapeada: '%s'", potentialColumnName)
				}
			}

			// Valida a direção (ASC/DESC)
			direction := "ASC" // Padrão é ASC
			if len(parts) > 1 {
				dirUpper := strings.ToUpper(parts[1])
				if dirUpper == "DESC" {
					direction = "DESC"
				} else if dirUpper != "ASC" {
					// Se especificou algo diferente de ASC/DESC, consideramos inválido
					return "", nil, nil, fmt.Errorf("direção de ordenação inválida: '%s' para coluna '%s' (use ASC ou DESC)", parts[1], validDBColumnName)
				}
				// Se for "ASC", já é o default
			}

			// Adiciona a cláusula validada e formatada corretamente
			// Não precisamos nos preocupar com SQL Injection aqui, pois validamos
			// `validDBColumnName` contra os metadados e `direction` só pode ser ASC ou DESC.
			safeOrderClauses = append(safeOrderClauses, fmt.Sprintf("%s %s", validDBColumnName, direction))
		} // Fim do loop de opts.OrderBy

		// Monta a cláusula final se houver itens válidos
		if len(safeOrderClauses) > 0 {
			orderByClause = "ORDER BY " + strings.Join(safeOrderClauses, ", ")
		}
	} // Fim if opts.OrderBy

	// 5. Monta LIMIT / OFFSET clause (igual antes, já consciente do dialeto)
	limitOffsetClause := ""
	limit := 0
	offset := 0
	hasPagination := false
	if opts != nil {
		limit = opts.Limit
		offset = opts.Offset
		hasPagination = limit > 0 || offset > 0
	}
	placeholderIndex := len(args) // Índice para placeholders continua de onde WHERE parou
	switch driverType {
	case SQLite, Postgres, MySQL: /* ... lógica LIMIT/OFFSET ... */
		if limit > 0 {
			limitOffsetClause += " LIMIT " + getPlaceholder(driverType, placeholderIndex)
			args = append(args, limit)
			placeholderIndex++
		}
		if offset > 0 {
			if limit <= 0 {
				limitOffsetClause += " LIMIT -1"
			}
			limitOffsetClause += " OFFSET " + getPlaceholder(driverType, placeholderIndex)
			args = append(args, offset)
			placeholderIndex++
		}
	case SQLServer: /* ... lógica OFFSET/FETCH ... */
		if hasPagination {
			if orderByClause == "" { /* ... default ORDER BY ou erro ... */
				if len(meta.PrimaryKeyColumns) == 1 {
					orderByClause = "ORDER BY " + meta.PrimaryKeyColumns[0].ColumnName + " ASC"
				} else {
					return "", nil, nil, errors.New("OFFSET/FETCH SQL Server exige ORDER BY")
				}
			}
			offsetVal := 0
			if offset > 0 {
				offsetVal = offset
			}
			limitVal := -1
			if limit > 0 {
				limitVal = limit
			}
			limitOffsetClause += " OFFSET " + getPlaceholder(driverType, placeholderIndex) + " ROWS"
			args = append(args, offsetVal)
			placeholderIndex++
			if limitVal > 0 {
				limitOffsetClause += " FETCH NEXT " + getPlaceholder(driverType, placeholderIndex) + " ROWS ONLY"
				args = append(args, limitVal)
				placeholderIndex++
			}
		}
	default:
		if hasPagination {
			fmt.Printf("[WARN] buildSelectQuery: Paginação não implementada para driver %s\n", driverType)
		}
	}

	// 6. Monta Query Final (igual antes)
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(selectClause)
	sb.WriteString(" FROM ")
	sb.WriteString(fromClause)
	if whereClause != "" {
		sb.WriteString(" ")
		sb.WriteString(whereClause)
	}
	if orderByClause != "" {
		sb.WriteString(" ")
		sb.WriteString(orderByClause)
	}
	if limitOffsetClause != "" {
		sb.WriteString(" ")
		sb.WriteString(limitOffsetClause)
	}
	finalQuery := sb.String()

	return finalQuery, args, columnOrder, nil
}
