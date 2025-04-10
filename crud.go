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

// buildInsertQuery (sem mudanças na assinatura, mas retorna []*metadata.ColumnMetadata)
func buildInsertQuery(meta *metadata.EntityMetadata) (string, []*metadata.ColumnMetadata, error) {
	// ... (lógica igual à anterior) ...
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
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		meta.TableName, strings.Join(columnNames, ", "), strings.Join(placeholders, ", "))
	return query, columnOrder, nil
}

// buildInsertArgs retorna []any
func buildInsertArgs(entityStructValue reflect.Value, meta *metadata.EntityMetadata, columnOrder []*metadata.ColumnMetadata) ([]any, error) { // <-- Retorna []any
	args := make([]any, 0, len(columnOrder)) // <-- Cria slice de any
	now := time.Now()
	for _, col := range columnOrder {
		var argValue any // <-- Usa any
		if col.IsCreatedAt || col.IsUpdatedAt {
			argValue = now
		} else {
			fieldValue := entityStructValue.Field(col.FieldIndex)
			if !fieldValue.IsValid() {
				return nil, fmt.Errorf("campo '%s' inválido", col.FieldName)
			}
			argValue = fieldValue.Interface() // .Interface() retorna interface{}, que é compatível com any
		}
		args = append(args, argValue)
	}
	return args, nil
}

// handleLastInsertID recebe entity any
func handleLastInsertID(entityPtrValue reflect.Value, meta *metadata.EntityMetadata, result sql.Result) error { // <-- Recebe reflect.Value do ponteiro
	if len(meta.PrimaryKeyColumns) != 1 || !meta.PrimaryKeyColumns[0].IsAutoIncrement {
		return nil
	}
	pkColumn := meta.PrimaryKeyColumns[0]
	lastID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("driver não suporta ou falhou ao obter LastInsertId: %w", err)
	}
	if lastID <= 0 {
		return fmt.Errorf("LastInsertId retornou valor inválido: %d", lastID)
	}

	// Usa .Elem() para obter a struct do ponteiro, e então o campo
	pkField := entityPtrValue.Elem().Field(pkColumn.FieldIndex)
	if !pkField.CanSet() {
		return fmt.Errorf("não é possível definir PK '%s'", pkColumn.FieldName)
	}

	switch pkField.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintID := uint64(lastID)
		if pkField.OverflowUint(uintID) {
			return fmt.Errorf("LastInsertId (%d) excede capacidade de %s (%s)", lastID, pkColumn.FieldName, pkField.Type())
		}
		pkField.SetUint(uintID)
		fmt.Printf("[LOG-CRUD] PK AutoIncrement definida em %s.%s = %d\n", meta.Name, pkColumn.FieldName, uintID)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if pkField.OverflowInt(lastID) {
			return fmt.Errorf("LastInsertId (%d) excede capacidade de %s (%s)", lastID, pkColumn.FieldName, pkField.Type())
		}
		pkField.SetInt(lastID)
		fmt.Printf("[LOG-CRUD] PK AutoIncrement definida em %s.%s = %d\n", meta.Name, pkColumn.FieldName, lastID)
	default:
		return fmt.Errorf("tipo PK '%s' (%s) não suportado para auto-increment", pkColumn.FieldName, pkField.Kind())
	}
	return nil
}

// FindByID recebe entityPtr any e id any
// TODO: Implementar FindByID e suas funções auxiliares (buildSelectByIDQuery, buildScanDest)
func FindByID(ctx context.Context, ds DataSource, entityPtr any, id any) error { // <-- Usa any
	return errors.New("typegorm.FindByID ainda não implementado")
}

// buildSelectByIDQuery (placeholder)
func buildSelectByIDQuery(meta *metadata.EntityMetadata, pk *metadata.ColumnMetadata) (string, []*metadata.ColumnMetadata) {
	panic("implementar")
}

// buildScanDest retornará []any
func buildScanDest(entityPtr any, meta *metadata.EntityMetadata, columnOrder []*metadata.ColumnMetadata) ([]any, error) {
	panic("implementar")
} // <-- Retorna []any
