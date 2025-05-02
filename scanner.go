// typegorm/scanner.go
package typegorm

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time" // Para logs

	"github.com/chmenegatti/typegorm/metadata" // Ajuste o path
)

// scanSingleRow escaneia a primeira (e única esperada) linha de rows
// para dentro da struct apontada por destPtr.
// Fecha rows ao final ou em caso de erro.
// Retorna sql.ErrNoRows se nenhuma linha for encontrada.
func scanSingleRow(rows *sql.Rows, meta *metadata.EntityMetadata, destPtr interface{}) (err error) {
	// Garante que rows será fechado, mesmo em caso de pânico ou erro
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil && err == nil { // Só reporta erro do Close se não houver erro anterior
			err = fmt.Errorf("erro ao fechar rows: %w", closeErr)
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		}
	}()

	// 1. Validação do ponteiro de destino (destPtr)
	destVal := reflect.ValueOf(destPtr)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() {
		return errors.New("scanSingleRow: destPtr deve ser um ponteiro não-nilo para uma struct")
	}
	destElem := destVal.Elem() // A struct em si
	if destElem.Kind() != reflect.Struct {
		return errors.New("scanSingleRow: destPtr deve apontar para uma struct")
	}
	if destElem.Type() != meta.Type { // Compara o tipo da struct com o tipo esperado pelo metadata
		return fmt.Errorf("scanSingleRow: tipo do destino (%s) diferente do tipo dos metadados (%s)", destElem.Type(), meta.Type)
	}

	// 2. Avança para a primeira linha
	if !rows.Next() {
		if err = rows.Err(); err != nil { // Checa erro após Next retornar false
			fmt.Printf("%s Erro durante rows.Next() inicial: %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return fmt.Errorf("erro ao avançar cursor: %w", err)
		}
		// Nenhuma linha encontrada, situação normal para busca por ID que falha
		return sql.ErrNoRows
	}

	// 3. Pega os nomes das colunas retornadas pelo banco
	columns, err := rows.Columns()
	if err != nil {
		fmt.Printf("%s Erro ao obter colunas do resultado: %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return fmt.Errorf("erro ao obter nomes das colunas: %w", err)
	}
	if len(columns) == 0 {
		fmt.Printf("%s Nenhuma coluna retornada na query. [%s]\n", logPrefixWarn, time.Now().Format(time.RFC3339))
		// O que fazer aqui? Scan falhará. Retornar erro?
		return errors.New("scanSingleRow: a query não retornou colunas")
	}
	// fmt.Printf("%s Colunas no ResultSet: %v [%s]\n", logPrefixDebug, columns, time.Now().Format(time.RFC3339))

	// 4. Prepara os destinos para o rows.Scan()
	scanArgs := make([]interface{}, len(columns)) // Slice de ponteiros para onde Scan escreverá
	mappedFields := 0                             // Contador para saber se alguma coluna foi mapeada

	for i, colName := range columns {
		// Usa o mapa `ColumnsByDbName` que adicionamos ao metadata
		colMeta, ok := meta.ColumnsByDbName[colName]
		if !ok {
			// Coluna do DB não mapeada na struct. Precisamos fornecer um destino para Scan,
			// caso contrário ele retorna erro. Usamos um ponteiro para sql.RawBytes para ignorar o valor.
			// fmt.Printf("%s Coluna '%s' retornada pelo DB não está mapeada em %s. Ignorando. [%s]\n", logPrefixWarn, colName, meta.Name, time.Now().Format(time.RFC3339))
			var ignored sql.RawBytes
			scanArgs[i] = &ignored
			continue // Próxima coluna
		}

		// Coluna mapeada! Pega o campo correspondente na struct de destino.
		fieldVal := destElem.FieldByName(colMeta.FieldName)
		if !fieldVal.IsValid() {
			// Isso indica um erro no nosso parser de metadados ou na struct
			err = fmt.Errorf("scanSingleRow: campo '%s' (para coluna '%s') não encontrado na struct %s via reflection", colMeta.FieldName, colName, meta.Name)
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return err
		}
		if !fieldVal.CanSet() {
			// Campo não exportado?
			err = fmt.Errorf("scanSingleRow: campo '%s' (para coluna '%s') não pode ser modificado (não exportado?)", colMeta.FieldName, colName)
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return err
		}

		// Cria um ponteiro para o campo da struct. Scan precisa de ponteiros.
		// fieldVal.Addr() retorna um reflect.Value representando o ponteiro (*TipoDoCampo)
		// .Interface() converte esse reflect.Value de volta para uma interface{} (do tipo *TipoDoCampo)
		scanArgs[i] = fieldVal.Addr().Interface()
		mappedFields++
	}

	if mappedFields == 0 && len(columns) > 0 {
		// Nenhuma coluna retornada foi mapeada para a struct de destino.
		// Isso pode acontecer se Select() foi usado com colunas erradas, ou SELECT * em tabela errada?
		err = fmt.Errorf("scanSingleRow: nenhuma coluna retornada (%v) foi mapeada para campos da struct %s", columns, meta.Name)
		fmt.Printf("%s %v [%s]\n", logPrefixWarn, err, time.Now().Format(time.RFC3339))
		// Decisão: Retornar erro aqui é mais seguro do que retornar uma struct vazia.
		return err
	}

	// 5. Executa o Scan! Os valores serão escritos diretamente nos campos da struct via os ponteiros em scanArgs.
	err = rows.Scan(scanArgs...)
	if err != nil {
		fmt.Printf("%s Erro durante rows.Scan: %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		// Tentar dar um erro mais útil se for conversão de tipo
		// (A análise do erro pode ser mais sofisticada)
		return fmt.Errorf("erro ao escanear dados da linha para a struct: %w", err)
	}

	// Neste ponto, os dados já estão na struct `destElem` porque fizemos Scan nos endereços dos campos.

	// 6. Verifica se há mais linhas (não deveria haver para scanSingleRow)
	if rows.Next() {
		err = errors.New("scanSingleRow: mais de uma linha retornada pela query quando apenas uma era esperada")
		fmt.Printf("%s %v [%s]\n", logPrefixWarn, err, time.Now().Format(time.RFC3339))
		// Podemos retornar o erro ou apenas logar e ignorar o resto? Retornar erro é mais seguro.
		return err
	}

	// 7. Verifica erro final de iteração (mesmo que Next tenha retornado false)
	if err = rows.Err(); err != nil {
		fmt.Printf("%s Erro final após iteração de rows: %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return fmt.Errorf("erro de iteração de rows: %w", err)
	}

	// Se chegou aqui, tudo certo! A struct em destPtr está populada.
	// fmt.Printf("%s Linha única escaneada com sucesso para %s. [%s]\n", logPrefixDebug, meta.Name, time.Now().Format(time.RFC3339))
	return nil
}

// scanRowsToSlice escaneia todas as linhas de rows e as anexa ao slice
// apontado por slicePtr.
// slicePtr deve ser um ponteiro para um slice de structs OU um ponteiro para um slice
// de ponteiros para structs (ex: *[]User ou *[]*User). A função determina
// qual caso é e popula corretamente.
// Fecha rows ao final ou em caso de erro.
// Retorna nil se bem sucedido (mesmo que nenhuma linha seja escaneada,
// resultando em um slice vazio), ou um erro.
func scanRowsToSlice(rows *sql.Rows, meta *metadata.EntityMetadata, slicePtr interface{}) (err error) {
	// Garante fechamento de rows
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil && err == nil {
			err = fmt.Errorf("erro ao fechar rows: %w", closeErr)
			fmt.Printf("[ERROR-SCANNER] %v [%s]\n", err, time.Now().Format(time.RFC3339))
		}
	}()

	// 1. Validação do slicePtr (deve ser *[]Tipo ou *[]*Tipo)
	sliceVal := reflect.ValueOf(slicePtr)
	if sliceVal.Kind() != reflect.Ptr || sliceVal.IsNil() {
		return errors.New("scanRowsToSlice: slicePtr deve ser um ponteiro não-nilo para um slice")
	}
	sliceDest := sliceVal.Elem() // O slice em si (ex: []User ou []*User)
	if sliceDest.Kind() != reflect.Slice {
		return errors.New("scanRowsToSlice: slicePtr deve apontar para um slice")
	}

	// 2. Determina o tipo dos elementos do slice
	sliceElemType := sliceDest.Type().Elem() // Tipo do elemento (ex: User ou *User)
	isPtrElement := sliceElemType.Kind() == reflect.Ptr
	var structType reflect.Type // Tipo da struct base (ex: User)

	if isPtrElement {
		structType = sliceElemType.Elem() // Pega User de *User
		if structType.Kind() != reflect.Struct {
			return fmt.Errorf("scanRowsToSlice: slicePtr (%T) aponta para slice de ponteiros para não-struct (%s)", slicePtr, structType)
		}
	} else if sliceElemType.Kind() == reflect.Struct {
		structType = sliceElemType // É User de []User
	} else {
		return fmt.Errorf("scanRowsToSlice: slicePtr (%T) aponta para slice de tipo não suportado (%s)", slicePtr, sliceElemType)
	}

	// Valida se o tipo base da struct corresponde aos metadados
	if structType != meta.Type {
		return fmt.Errorf("scanRowsToSlice: tipo do elemento do slice (%s) não corresponde ao tipo dos metadados (%s)", structType, meta.Type)
	}

	// 3. Obtém nomes das colunas do resultado
	columns, err := rows.Columns()
	if err != nil {
		fmt.Printf("[ERROR-SCANNER] Erro ao obter colunas do resultado: %v [%s]\n", err, time.Now().Format(time.RFC3339))
		return fmt.Errorf("erro ao obter nomes das colunas: %w", err)
	}
	// Se não houver colunas, não há o que fazer, retorna slice vazio (sem erro)
	if len(columns) == 0 {
		fmt.Printf("[WARN-SCANNER] Nenhuma coluna retornada na query para slice scan. [%s]\n", time.Now().Format(time.RFC3339))
		return nil
	}

	// 4. Prepara mapa reverso para performance no loop (DB Col -> Campo Struct)
	// Poderia ser pré-calculado e passado como argumento se chamado muitas vezes.
	dbNameToColMeta := meta.ColumnsByDbName // Reutiliza o mapa dos metadados

	// 5. Itera sobre as linhas do resultado
	baseSlice := sliceDest // Guarda a referência original do slice para usar Append

	for rows.Next() {
		// Cria uma NOVA instância da struct base para cada linha
		// reflect.New sempre retorna um ponteiro (*StructType)
		newStructPtrVal := reflect.New(structType) // Ex: *User
		newStructVal := newStructPtrVal.Elem()     // Ex: User (endereçável)

		// Prepara os argumentos para o Scan desta linha, apontando para os campos da nova instância
		scanArgs := make([]interface{}, len(columns))
		mappedFields := 0
		for i, colName := range columns {
			colMeta, ok := dbNameToColMeta[colName]
			if !ok {
				var ignored sql.RawBytes // Ignora coluna não mapeada
				scanArgs[i] = &ignored
				continue
			}

			// Pega o campo na *nova* struct criada
			fieldVal := newStructVal.FieldByName(colMeta.FieldName)
			if !fieldVal.IsValid() || !fieldVal.CanSet() {
				err = fmt.Errorf("scanRowsToSlice: campo '%s' (coluna '%s') inválido/não configurável em %s", colMeta.FieldName, colName, meta.Name)
				fmt.Printf("[ERROR-SCANNER] %v [%s]\n", err, time.Now().Format(time.RFC3339))
				return err // Erro fatal aqui
			}
			scanArgs[i] = fieldVal.Addr().Interface() // Adiciona ponteiro do campo aos args do scan
			mappedFields++
		}

		if mappedFields == 0 && len(columns) > 0 {
			// Nenhuma coluna correspondeu? Pode ser um erro de query. Logar e continuar? Ou erro?
			// Vamos logar um aviso e continuar, pois pode ser um SELECT em tabela errada
			fmt.Printf("[WARN-SCANNER] Nenhuma coluna retornada (%v) foi mapeada para campos da struct %s em uma das linhas. [%s]\n", columns, meta.Name, time.Now().Format(time.RFC3339))
			// Não retorna erro aqui, apenas resultará em structs zeradas no slice se isso acontecer para todas as linhas.
		}

		// Escaneia a linha atual nos campos da nova struct
		err = rows.Scan(scanArgs...)
		if err != nil {
			fmt.Printf("[ERROR-SCANNER] Erro durante rows.Scan para slice: %v [%s]\n", err, time.Now().Format(time.RFC3339))
			return fmt.Errorf("erro ao escanear dados da linha para struct %s: %w", structType.Name(), err)
		}

		// Adiciona a nova struct (ou ponteiro para ela) ao slice de destino
		if isPtrElement {
			// O slice destino é do tipo []*StructType, então adiciona o ponteiro criado
			baseSlice = reflect.Append(baseSlice, newStructPtrVal)
		} else {
			// O slice destino é do tipo []StructType, então adiciona o valor da struct
			baseSlice = reflect.Append(baseSlice, newStructVal)
		}
	} // Fim do loop rows.Next()

	// 6. Atualiza o slice original via ponteiro
	// sliceDest é o slice original (ex: o valor de *slicePtr)
	// baseSlice contém os elementos adicionados. Precisamos fazer sliceDest = baseSlice
	sliceDest.Set(baseSlice)

	// 7. Verifica erro final de iteração do rows
	if err = rows.Err(); err != nil {
		fmt.Printf("[ERROR-SCANNER] Erro final após iteração de rows para slice: %v [%s]\n", err, time.Now().Format(time.RFC3339))
		return fmt.Errorf("erro de iteração de rows: %w", err)
	}

	// Tudo certo, o slice em slicePtr foi populado (pode estar vazio se não houver resultados)
	return nil
}
