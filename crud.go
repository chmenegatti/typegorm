// crud.go
package typegorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/chmenegatti/typegorm/metadata"
)

// Insert insere uma nova entidade no banco de dados.
// 'entity' deve ser um ponteiro para uma struct mapeável.
// Usa 'any' como alias para interface{}.
func Insert(ctx context.Context, ds DataSource, entity any) error { // <--- Usa any
	// 1. Valida Input e Obtém Metadados
	entityValue := reflect.ValueOf(entity)
	if entityValue.Kind() != reflect.Ptr || entityValue.IsNil() {
		return fmt.Errorf("typegorm.Insert: 'entity' deve ser um ponteiro não-nilo para uma struct, obteve %T", entity)
	}
	entityStructValue := entityValue.Elem()
	if entityStructValue.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.Insert: 'entity' deve ser um ponteiro para uma struct, mas aponta para %s", entityStructValue.Kind())
	}

	meta, err := metadata.Parse(entityStructValue.Interface()) // Passa a interface concreta para Parse
	if err != nil {
		return fmt.Errorf("typegorm.Insert: erro ao obter metadados para %T: %w", entity, err)
	}

	// 2. Construir Query e Ordem das Colunas
	sqlQuery, columnOrder, err := buildInsertQuery(meta)
	if err != nil {
		return fmt.Errorf("typegorm.Insert: %w", err)
	} // Simplificar erro
	fmt.Printf("[LOG-CRUD] Insert Query para %s: %s\n", meta.Name, sqlQuery)

	// 3. Construir Argumentos
	args, err := buildInsertArgs(entityStructValue, meta, columnOrder) // Usa valor da struct
	if err != nil {
		return fmt.Errorf("typegorm.Insert: %w", err)
	} // Simplificar erro
	fmt.Printf("[LOG-CRUD] Insert Args para %s: %v\n", meta.Name, args)

	// 4. Executar Query
	result, err := ds.ExecContext(ctx, sqlQuery, args...) // Passa slice de 'any'
	if err != nil {
		return fmt.Errorf("typegorm.Insert: falha na execução para %s [%s]: %w", meta.Name, sqlQuery, err)
	}
	fmt.Printf("[LOG-CRUD] Insert ExecContext para %s bem-sucedido.\n", meta.Name)

	// 5. Tratar LastInsertId
	err = handleLastInsertID(entityValue, meta, result) // Passa ponteiro original
	if err != nil {
		fmt.Printf("[WARN] typegorm.Insert: erro ao obter/definir LastInsertId para %s: %v\n", meta.Name, err)
	}

	return nil
}

// FindByID busca uma entidade pelo seu ID e carrega os dados em entityPtr.
// 'entityPtr' deve ser um ponteiro para uma struct mapeável (ex: &Usuario{}).
// 'id' é o valor da chave primária a ser buscada (pode ser de qualquer tipo compatível).
// Retorna sql.ErrNoRows se não encontrado, ou outro erro se ocorrer falha.
func FindByID(ctx context.Context, ds DataSource, entityPtr any, id any) error {
	// 1. Valida Input e Obtém Metadados
	ptrValue := reflect.ValueOf(entityPtr)
	if ptrValue.Kind() != reflect.Ptr || ptrValue.IsNil() {
		return fmt.Errorf("typegorm.FindByID: entityPtr deve ser um ponteiro não-nilo para uma struct, obteve %T", entityPtr)
	}
	structValue := ptrValue.Elem() // A struct real onde carregaremos os dados
	if structValue.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.FindByID: entityPtr deve apontar para uma struct, mas aponta para %s", structValue.Kind())
	}
	structType := structValue.Type()

	meta, err := metadata.Parse(structValue.Interface()) // Passa um valor do tipo da struct para Parse
	if err != nil {
		return fmt.Errorf("typegorm.FindByID: erro ao obter metadados para %s: %w", structType.Name(), err)
	}

	// 2. Valida Chave Primária
	if len(meta.PrimaryKeyColumns) == 0 {
		return fmt.Errorf("typegorm.FindByID: entidade %s não possui chave primária definida nos metadados", meta.Name)
	}
	if len(meta.PrimaryKeyColumns) > 1 {
		// Poderíamos suportar múltiplos IDs como args... mas simplificamos por agora
		return fmt.Errorf("typegorm.FindByID: busca por ID composto ainda não suportada para %s", meta.Name)
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	fmt.Printf("[LOG-CRUD] FindByID: Buscando %s por PK '%s'\n", meta.Name, pkColumn.ColumnName)

	// 3. Construir Query SELECT
	sqlQuery, columnOrder, err := buildSelectByIDQuery(meta, pkColumn)
	if err != nil {
		return fmt.Errorf("typegorm.FindByID: erro ao construir query para %s: %w", meta.Name, err)
	}
	fmt.Printf("[LOG-CRUD] FindByID Query para %s: %s\n", meta.Name, sqlQuery)

	// 4. Executar QueryRowContext
	// Passamos o valor do ID recebido como argumento
	row := ds.QueryRowContext(ctx, sqlQuery, id)
	fmt.Printf("[LOG-CRUD] FindByID executou QueryRowContext para %s com ID %v\n", meta.Name, id)

	// 5. Preparar Destinos para Scan Dinâmico
	scanDest, err := buildScanDest(structValue, meta, columnOrder) // Passa o VALOR da struct, não o ponteiro
	if err != nil {
		return fmt.Errorf("typegorm.FindByID: erro ao preparar destino do Scan para %s: %w", meta.Name, err)
	}
	fmt.Printf("[LOG-CRUD] FindByID: %d destinos preparados para Scan.\n", len(scanDest))

	// 6. Executar Scan
	err = row.Scan(scanDest...) // Usa o slice de ponteiros para os campos
	if err != nil {
		// Verifica especificamente por ErrNoRows e o retorna diretamente
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Printf("[LOG-CRUD] FindByID: Registro não encontrado para %s com ID %v (sql.ErrNoRows)\n", meta.Name, id)
			return sql.ErrNoRows
		}
		// Outro erro durante o Scan
		return fmt.Errorf("typegorm.FindByID: falha no Scan para %s [%s]: %w", meta.Name, sqlQuery, err)
	}

	fmt.Printf("[LOG-CRUD] FindByID: Scan bem-sucedido para %s com ID %v.\n", meta.Name, id)
	return nil // Sucesso! entityPtr agora está preenchido
}

// Update atualiza um registro existente no banco de dados com base na entidade fornecida.
// 'entity' deve ser um ponteiro para uma struct mapeável com a PK preenchida.
// Atualiza todas as colunas não-PK, incluindo 'updatedAt'.
// Retorna erro se a PK não for encontrada ou se a atualização falhar.
func Update(ctx context.Context, ds DataSource, entity any) error {
	// 1. Valida Input e Obtém Metadados
	entityValue := reflect.ValueOf(entity)
	if entityValue.Kind() != reflect.Ptr || entityValue.IsNil() {
		return fmt.Errorf("typegorm.Update: 'entity' deve ser um ponteiro não-nilo para uma struct, obteve %T", entity)
	}
	entityStructValue := entityValue.Elem()
	if entityStructValue.Kind() != reflect.Struct {
		return fmt.Errorf("typegorm.Update: 'entity' deve ser ponteiro para struct, aponta para %s", entityStructValue.Kind())
	}

	meta, err := metadata.Parse(entityStructValue.Interface())
	if err != nil {
		return fmt.Errorf("typegorm.Update: erro ao obter metadados para %T: %w", entity, err)
	}

	// 2. Valida e Extrai PK
	if len(meta.PrimaryKeyColumns) == 0 {
		return fmt.Errorf("typegorm.Update: entidade %s não tem PK definida", meta.Name)
	}
	if len(meta.PrimaryKeyColumns) > 1 {
		return fmt.Errorf("typegorm.Update: PK composta não suportada ainda para %s", meta.Name)
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	pkField := entityStructValue.Field(pkColumn.FieldIndex)
	if !pkField.IsValid() {
		return fmt.Errorf("typegorm.Update: campo PK '%s' inválido", pkColumn.FieldName)
	}
	pkValue := pkField.Interface()
	// Verifica se a PK tem valor (não zero/nulo) - importante para WHERE
	if reflect.ValueOf(pkValue).IsZero() {
		return fmt.Errorf("typegorm.Update: valor da chave primária '%s' está zerado/nulo, impossível atualizar", pkColumn.FieldName)
	}
	fmt.Printf("[LOG-CRUD] Update: Atualizando %s com PK '%s' = %v\n", meta.Name, pkColumn.ColumnName, pkValue)

	// 3. Construir Query UPDATE e Argumentos
	var setClauses []string
	var args []any
	var columnOrderForArgs []*metadata.ColumnMetadata // Ordem para os SETs
	now := time.Now()

	for _, col := range meta.Columns {
		// Não inclui PK no SET
		if col.IsPrimaryKey {
			continue
		}
		// Não inclui createdAt no SET
		if col.IsCreatedAt {
			continue
		}

		// Inclui updatedAt ou outra coluna normal
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", col.ColumnName))
		columnOrderForArgs = append(columnOrderForArgs, col)
	}

	if len(setClauses) == 0 {
		return errors.New("typegorm.Update: nenhuma coluna encontrada para atualizar (além da PK/createdAt?)")
	}

	// Monta query
	// Nota: NÃO adicionamos `deleted_at IS NULL` aqui. Permitimos atualizar registros soft-deleted (ex: para restaurar).
	whereClause := fmt.Sprintf("%s = ?", pkColumn.ColumnName)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		meta.TableName,
		strings.Join(setClauses, ", "),
		whereClause,
	)
	fmt.Printf("[LOG-CRUD] Update Query para %s: %s\n", meta.Name, query)

	// Monta argumentos na ordem correta (colunas do SET + PK do WHERE)
	args = make([]any, 0, len(columnOrderForArgs)+1)
	for _, col := range columnOrderForArgs {
		var argValue any
		if col.IsUpdatedAt {
			argValue = now // Define timestamp de atualização
			// Opcional: Atualiza o campo na struct também?
			// field := entityStructValue.Field(col.FieldIndex)
			// if field.CanSet() && field.Type() == reflect.TypeOf(now) { field.Set(reflect.ValueOf(now)) }
		} else {
			fieldValue := entityStructValue.Field(col.FieldIndex)
			if !fieldValue.IsValid() {
				return fmt.Errorf("campo '%s' inválido ao montar args", col.FieldName)
			}
			argValue = fieldValue.Interface()
		}
		args = append(args, argValue)
	}
	// Adiciona o valor da PK por último para a cláusula WHERE
	args = append(args, pkValue)
	fmt.Printf("[LOG-CRUD] Update Args para %s: %v\n", meta.Name, args)

	// 4. Executar Query
	result, err := ds.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("typegorm.Update: falha na execução para %s [%s]: %w", meta.Name, query, err)
	}
	fmt.Printf("[LOG-CRUD] Update ExecContext para %s bem-sucedido.\n", meta.Name)

	// 5. Verificar Linhas Afetadas
	rowsAffected, raErr := result.RowsAffected()
	if raErr != nil {
		// Loga mas não necessariamente falha a operação só por não conseguir RowsAffected
		fmt.Printf("[WARN] typegorm.Update: não foi possível obter RowsAffected para %s: %v\n", meta.Name, raErr)
	} else {
		fmt.Printf("[LOG-CRUD] Update RowsAffected para %s: %d\n", meta.Name, rowsAffected)
		if rowsAffected == 0 {
			// Pode significar que o registro não foi encontrado com aquela PK (ou já tinha os mesmos valores)
			// Retornar um erro aqui pode ser útil.
			return fmt.Errorf("typegorm.Update: registro com PK %v não encontrado ou nenhuma linha foi alterada para %s", pkValue, meta.Name) // Ou um erro customizado
		}
		if rowsAffected > 1 {
			// Inesperado para update por PK única
			return fmt.Errorf("typegorm.Update: RowsAffected foi %d (esperado 0 ou 1) para %s com PK %v", rowsAffected, meta.Name, pkValue)
		}
	}

	return nil // Sucesso
}

// Delete remove um registro do banco de dados.
// Se a entidade tiver a tag `deletedAt`, realiza um Soft Delete (atualiza a coluna).
// Caso contrário, realiza um Hard Delete (DELETE FROM ...).
// 'entity' deve ser um ponteiro para uma struct mapeável com a PK preenchida.
// Retorna erro se a PK não for encontrada ou se a operação falhar.
func Delete(ctx context.Context, ds DataSource, entity any) error {
	// 1. Valida Input e Obtém Metadados
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

	// 2. Valida e Extrai PK
	if len(meta.PrimaryKeyColumns) != 1 {
		return fmt.Errorf("typegorm.Delete: PK ausente ou composta não suportada para %s", meta.Name)
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	pkField := entityStructValue.Field(pkColumn.FieldIndex)
	if !pkField.IsValid() {
		return fmt.Errorf("typegorm.Delete: campo PK '%s' inválido", pkColumn.FieldName)
	}
	pkValue := pkField.Interface()
	if reflect.ValueOf(pkValue).IsZero() {
		return fmt.Errorf("typegorm.Delete: PK '%s' está zerada/nula", pkColumn.FieldName)
	}
	fmt.Printf("[LOG-CRUD] Delete: Deletando %s com PK '%s' = %v\n", meta.Name, pkColumn.ColumnName, pkValue)

	var query string
	var args []any
	now := time.Now() // Usado para soft delete

	// 3. Verifica se é Soft Delete ou Hard Delete
	if meta.DeletedAtColumn != nil {
		// --- Soft Delete ---
		fmt.Printf("[LOG-CRUD] Delete: Executando Soft Delete para %s (coluna %s)\n", meta.Name, meta.DeletedAtColumn.ColumnName)
		// UPDATE tableName SET deleted_at = ? WHERE pkCol = ? AND deleted_at IS NULL
		query = fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ? AND %s IS NULL",
			meta.TableName,
			meta.DeletedAtColumn.ColumnName,
			pkColumn.ColumnName,
			meta.DeletedAtColumn.ColumnName, // Garante que só deletamos uma vez
		)
		args = []any{now, pkValue} // Argumentos: tempo atual, valor da PK

		// Opcional: Atualizar campo DeletadoEm na struct?
		// deletedAtField := entityStructValue.Field(meta.DeletedAtColumn.FieldIndex)
		// if deletedAtField.CanSet() && deletedAtField.Type() == reflect.TypeOf(sql.NullTime{}) {
		//     deletedAtField.Set(reflect.ValueOf(sql.NullTime{Time: now, Valid: true}))
		// }

	} else {
		// --- Hard Delete ---
		fmt.Printf("[LOG-CRUD] Delete: Executando Hard Delete para %s\n", meta.Name)
		// DELETE FROM tableName WHERE pkCol = ?
		query = fmt.Sprintf("DELETE FROM %s WHERE %s = ?",
			meta.TableName,
			pkColumn.ColumnName,
		)
		args = []any{pkValue} // Argumento: valor da PK
	}

	// 4. Executar Query
	fmt.Printf("[LOG-CRUD] Delete Query para %s: %s\n", meta.Name, query)
	fmt.Printf("[LOG-CRUD] Delete Args para %s: %v\n", meta.Name, args)
	result, err := ds.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("typegorm.Delete: falha na execução para %s [%s]: %w", meta.Name, query, err)
	}
	fmt.Printf("[LOG-CRUD] Delete ExecContext para %s bem-sucedido.\n", meta.Name)

	// 5. Verificar Linhas Afetadas
	rowsAffected, raErr := result.RowsAffected()
	if raErr != nil {
		fmt.Printf("[WARN] typegorm.Delete: não foi possível obter RowsAffected para %s: %v\n", meta.Name, raErr)
	} else {
		fmt.Printf("[LOG-CRUD] Delete RowsAffected para %s: %d\n", meta.Name, rowsAffected)
		if rowsAffected == 0 {
			// Registro não encontrado OU (no caso de soft delete) já estava deletado
			return fmt.Errorf("typegorm.Delete: registro com PK %v não encontrado ou já deletado para %s", pkValue, meta.Name) // Ou sql.ErrNoRows? Ou erro customizado?
		}
		if rowsAffected > 1 {
			// Inesperado para delete por PK única
			return fmt.Errorf("typegorm.Delete: RowsAffected foi %d (esperado 0 ou 1) para %s com PK %v", rowsAffected, meta.Name, pkValue)
		}
	}

	return nil // Sucesso
}

// --- Funções Auxiliares para FindByID ---

// buildSelectByIDQuery constrói "SELECT col1, col2 FROM table WHERE pk = ?"
// Retorna a query e a ordem das colunas selecionadas.
func buildSelectByIDQuery(meta *metadata.EntityMetadata, pk *metadata.ColumnMetadata) (string, []*metadata.ColumnMetadata, error) {
	if len(meta.Columns) == 0 {
		return "", nil, errors.New("nenhuma coluna mapeada")
	}
	var selectColumns []string
	var columnOrder []*metadata.ColumnMetadata

	for _, col := range meta.Columns {
		selectColumns = append(selectColumns, col.ColumnName)
		columnOrder = append(columnOrder, col)
	}

	whereClause := fmt.Sprintf("%s = ?", pk.ColumnName)

	// *** ADICIONADO: Filtro para soft delete ***
	if meta.DeletedAtColumn != nil {
		whereClause = fmt.Sprintf("%s AND %s IS NULL", whereClause, meta.DeletedAtColumn.ColumnName)
		fmt.Printf("[LOG-CRUD] buildSelectByIDQuery: Adicionando filtro WHERE %s IS NULL\n", meta.DeletedAtColumn.ColumnName)
	}
	// *** FIM ADIÇÃO ***

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		strings.Join(selectColumns, ", "), meta.TableName, whereClause)

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
func buildInsertQuery(meta *metadata.EntityMetadata) (string, []*metadata.ColumnMetadata, error) { /* ... (igual antes) ... */
	if len(meta.Columns) == 0 {
		return "", nil, errors.New("nenhuma coluna mapeada encontrada")
	}
	var columnNames []string
	var placeholders []string
	var columnOrder []*metadata.ColumnMetadata
	for _, col := range meta.Columns {
		if col.IsPrimaryKey && col.IsAutoIncrement {
			continue
		}
		columnNames = append(columnNames, col.ColumnName)
		placeholders = append(placeholders, "?")
		columnOrder = append(columnOrder, col)
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
func handleLastInsertID(entityPtrValue reflect.Value, meta *metadata.EntityMetadata, result sql.Result) error { /* ... (igual antes) ... */
	if len(meta.PrimaryKeyColumns) != 1 || !meta.PrimaryKeyColumns[0].IsAutoIncrement {
		return nil
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	lastID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("driver não suporta ou falhou LastInsertId: %w", err)
	}
	if lastID <= 0 {
		return fmt.Errorf("LastInsertId inválido: %d", lastID)
	}
	pkField := entityPtrValue.Elem().Field(pkColumn.FieldIndex)
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
		fmt.Printf("[LOG-CRUD] PK AutoIncrement: %s.%s = %d\n", meta.Name, pkColumn.FieldName, uintID)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if pkField.OverflowInt(lastID) {
			return fmt.Errorf("overflow int PK '%s'", pkColumn.FieldName)
		}
		pkField.SetInt(lastID)
		fmt.Printf("[LOG-CRUD] PK AutoIncrement: %s.%s = %d\n", meta.Name, pkColumn.FieldName, lastID)
	default:
		return fmt.Errorf("tipo PK '%s' (%s) não suportado", pkColumn.FieldName, pkField.Kind())
	}
	return nil
}
