// driver/postgres/postgres_test.go
package postgres_test // Pacote _test para testar API pública

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os" // Para ler variáveis de ambiente
	"reflect"
	"strconv" // Para converter porta
	"testing"
	"time"

	// Importa TypeGorm
	"github.com/chmenegatti/typegorm"
	// Importa config do driver postgres e registra o driver via init()
	pg_driver "github.com/chmenegatti/typegorm/driver/postgres"
)

// --- Função Auxiliar para obter DataSource conectado para testes ---
// Lê config de variáveis de ambiente ou usa defaults. Pula teste se não configurado.
func getTestDataSource(t *testing.T) typegorm.DataSource {
	t.Helper() // Marca como função auxiliar de teste

	// Lê detalhes da conexão do ambiente, com padrões para localhost
	pgHost := os.Getenv("TEST_PG_HOST")
	if pgHost == "" {
		pgHost = "localhost"
	}

	pgPortStr := os.Getenv("TEST_PG_PORT")
	if pgPortStr == "" {
		pgPortStr = "5432"
	}

	pgUser := os.Getenv("TEST_PG_USER")
	if pgUser == "" {
		pgUser = "admin"
	} // Usuário padrão do Postgres

	pgPassword := os.Getenv("TEST_PG_PASSWORD")
	if pgPassword == "" {
		pgPassword = "password"
	} // Senha comum em setups de teste

	pgDbName := os.Getenv("TEST_PG_DBNAME")
	if pgDbName == "" {
		pgDbName = "testdb"
	} // Nome comum de DB de teste

	// Permite pular testes de integração se não explicitamente habilitados
	// if os.Getenv("RUN_POSTGRES_TESTS") != "true" {
	// 	t.Skip("Pulando testes de integração PostgreSQL: RUN_POSTGRES_TESTS não definida como 'true'")
	// }

	pgPort, err := strconv.Atoi(pgPortStr)
	if err != nil {
		t.Fatalf("Valor inválido para TEST_PG_PORT: %v", err)
	}

	// Cria a configuração específica do driver
	config := pg_driver.Config{
		Host:     pgHost,
		Port:     pgPort,
		Username: pgUser,
		Password: pgPassword,
		Database: pgDbName,
		SSLMode:  "disable", // Geralmente 'disable' para testes locais via Docker/localhost
		// Params: map[string]string{"connect_timeout": "5"}, // Exemplo de parâmetro extra
	}

	t.Logf("getTestDataSource (Postgres): Conectando a postgresql://%s@%s:%d/%s?sslmode=%s",
		config.Username, config.Host, config.Port, config.Database, config.SSLMode)

	// Conecta usando a fábrica central do TypeGorm
	dataSource, err := typegorm.Connect(config)
	if err != nil {
		t.Fatalf("getTestDataSource: typegorm.Connect() falhou: %v. Garanta que o PostgreSQL está rodando e acessível em %s:%d com usuário '%s' e banco '%s'.",
			err, config.Host, config.Port, config.Username, config.Database)
	}
	if dataSource == nil {
		t.Fatal("getTestDataSource: typegorm.Connect() retornou DataSource nulo sem erro")
	}

	// Garante que Close seja chamado ao final do teste que usar este DS
	t.Cleanup(func() {
		t.Log("getTestDataSource Cleanup (Postgres): Fechando conexão...")
		if err := dataSource.Close(); err != nil {
			t.Errorf("getTestDataSource Cleanup: dataSource.Close() falhou: %v", err)
		} else {
			t.Log("getTestDataSource Cleanup: Conexão fechada.")
		}
	})

	// Opcional: Limpar tabelas criadas pelos testes?
	// Geralmente é mais fácil usar um banco de dados de teste dedicado
	// que pode ser descartado e recriado entre as execuções de teste.

	return dataSource
}

// --- Testes para os Métodos da DataSource (Adaptados para Postgres) ---

func TestPostgresConnectionFactoryAndPing(t *testing.T) {
	ds := getTestDataSource(t) // Obtém conexão do helper
	ctx := context.Background()
	t.Log("TestPostgresConnectionFactoryAndPing: DataSource obtido.")

	// Testa Ping
	t.Log("TestPostgresConnectionFactoryAndPing: Pingando banco de dados...")
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second) // Timeout mais generoso para rede
	defer cancel()
	if err := ds.Ping(pingCtx); err != nil {
		t.Fatalf("dataSource.Ping() falhou: %v", err)
	}
	t.Log("TestPostgresConnectionFactoryAndPing: Ping bem-sucedido.")

	// Testa GetDriverType
	if driverType := ds.GetDriverType(); driverType != typegorm.Postgres {
		t.Errorf("dataSource.GetDriverType() = %q, esperado %q", driverType, typegorm.Postgres)
	} else {
		t.Logf("dataSource.GetDriverType() retornou tipo correto: %q", driverType)
	}
}

func TestPostgres_ExecContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Limpeza inicial (opcional, mas útil para idempotência)
	_, _ = ds.ExecContext(ctx, `DROP TABLE IF EXISTS test_exec_pg;`)

	// Cria tabela (usando tipos PG)
	// SERIAL é um atalho para INT com sequence e primary key
	createTableSQL := `CREATE TABLE test_exec_pg (id SERIAL PRIMARY KEY, name VARCHAR(100) NOT NULL, data TEXT);`
	_, err := ds.ExecContext(ctx, createTableSQL)
	if err != nil {
		t.Fatalf("ExecContext (CREATE TABLE) falhou: %v", err)
	}
	t.Log("CREATE TABLE bem-sucedido.")

	// Insere dados (usando placeholders $1, $2)
	insertSQL := `INSERT INTO test_exec_pg (name, data) VALUES ($1, $2), ($3, $4);`
	result, err := ds.ExecContext(ctx, insertSQL, "Arara Azul", "Ave", "Onça Pintada", "Felino")
	if err != nil {
		t.Fatalf("ExecContext (INSERT) falhou: %v", err)
	}
	t.Log("INSERT bem-sucedido.")

	// Verifica RowsAffected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Errorf("result.RowsAffected() falhou: %v", err)
	} else if rowsAffected != 2 {
		t.Errorf("Esperado 2 linhas afetadas, obteve %d", rowsAffected)
	} else {
		t.Logf("RowsAffected: %d (Correto)", rowsAffected)
	}

	// LastInsertId geralmente NÃO é suportado diretamente por drivers PG via Result.
	// A forma idiomática no PG é usar `RETURNING id`. Vamos pular essa verificação aqui.
	_, err = result.LastInsertId()
	if err == nil {
		t.Log("result.LastInsertId() não retornou erro (inesperado para PG, mas ok)")
	} else {
		t.Logf("result.LastInsertId() retornou erro (esperado para PG): %v", err)
	}
}

func TestPostgres_QueryContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, _ = ds.ExecContext(ctx, `DROP TABLE IF EXISTS test_query_pg;`)
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_query_pg (code INT PRIMARY KEY, value TEXT);`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO test_query_pg (code, value) VALUES (10, 'dez'), (20, 'vinte'), (30, 'trinta');`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Consulta dados
	rows, err := ds.QueryContext(ctx, `SELECT code, value FROM test_query_pg WHERE code >= $1 ORDER BY code ASC;`, 20)
	if err != nil {
		t.Fatalf("QueryContext falhou: %v", err)
	}
	defer rows.Close() // Garante fechamento

	count := 0
	codes := []int{}
	values := []string{}

	for rows.Next() {
		count++
		var code int
		var value string
		if err := rows.Scan(&code, &value); err != nil {
			t.Errorf("rows.Scan falhou: %v", err)
			continue
		}
		codes = append(codes, code)
		values = append(values, value)
	}
	if err := rows.Err(); err != nil { // Verifica erro pós-loop
		t.Errorf("rows.Err() reportou erro: %v", err)
	}

	// Verifica resultados
	if count != 2 {
		t.Errorf("Esperado 2 linhas, obteve %d", count)
	}
	expectedCodes := []int{20, 30}
	expectedValues := []string{"vinte", "trinta"}
	if fmt.Sprintf("%v", codes) != fmt.Sprintf("%v", expectedCodes) {
		t.Errorf("Esperado Codes %v, obteve %v", expectedCodes, codes)
	}
	if fmt.Sprintf("%v", values) != fmt.Sprintf("%v", expectedValues) {
		t.Errorf("Esperado values %v, obteve %v", expectedValues, values)
	}

	t.Logf("Query bem-sucedida, obteve %d linhas.", count)
}

func TestPostgres_QueryRowContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, _ = ds.ExecContext(ctx, `DROP TABLE IF EXISTS test_row_pg;`)
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_row_pg (id SERIAL PRIMARY KEY, nickname TEXT UNIQUE);`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO test_row_pg (nickname) VALUES ('Capivara');`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Consulta linha existente
	var nickname string
	// Usa $1 para placeholder
	row := ds.QueryRowContext(ctx, `SELECT nickname FROM test_row_pg WHERE id = $1;`, 1)
	if row == nil {
		t.Fatal("QueryRowContext retornou nil inesperadamente")
	}
	err = row.Scan(&nickname)
	if err != nil {
		t.Errorf("QueryRowContext / Scan falhou para linha existente: %v", err)
	} else if nickname != "Capivara" {
		t.Errorf("Esperado nickname 'Capivara', obteve '%s'", nickname)
	} else {
		t.Logf("Scan bem-sucedido para linha existente, obteve: %s", nickname)
	}

	// Consulta linha inexistente
	var missingNickname string
	row = ds.QueryRowContext(ctx, `SELECT nickname FROM test_row_pg WHERE id = $1;`, 99)
	if row == nil {
		t.Fatal("QueryRowContext retornou nil inesperadamente para linha inexistente")
	}
	err = row.Scan(&missingNickname)
	if err == nil {
		t.Error("Esperado erro ao escanear linha inexistente, obteve nil")
	} else if !errors.Is(err, sql.ErrNoRows) { // Verifica erro padrão
		t.Errorf("Esperado sql.ErrNoRows, obteve: %v", err)
	} else {
		t.Logf("Obteve sql.ErrNoRows esperado para linha inexistente.")
	}
}

func TestPostgres_BeginTx(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, _ = ds.ExecContext(ctx, `DROP TABLE IF EXISTS accounts_pg;`)
	_, err := ds.ExecContext(ctx, `CREATE TABLE accounts_pg (id INT PRIMARY KEY, balance NUMERIC(10, 2));`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO accounts_pg (id, balance) VALUES (1, 100.50), (2, 200.00);`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Inicia Tx
	tx, err := ds.BeginTx(ctx, nil) // Usa opções padrão
	if err != nil {
		t.Fatalf("BeginTx falhou: %v", err)
	}
	t.Log("BeginTx bem-sucedido.")

	// Controle de Rollback/Commit
	committedOrRolledBack := false
	defer func() {
		if !committedOrRolledBack {
			t.Log("Defer: Transação não finalizada explicitamente, revertendo (Rollback)...")
			tx.Rollback()
		}
	}()

	// Operações na Tx (usando $1, $2)
	_, err = tx.ExecContext(ctx, `UPDATE accounts_pg SET balance = balance - $1 WHERE id = $2;`, 10.50, 1)
	if err != nil {
		tx.Rollback()
		committedOrRolledBack = true
		t.Fatalf("tx.ExecContext (UPDATE 1) falhou: %v", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts_pg SET balance = balance + $1 WHERE id = $2;`, 10.50, 2)
	if err != nil {
		tx.Rollback()
		committedOrRolledBack = true
		t.Fatalf("tx.ExecContext (UPDATE 2) falhou: %v", err)
	}

	// Comita Tx
	if err = tx.Commit(); err != nil {
		// Commit falhou, considera revertido
		committedOrRolledBack = true
		t.Fatalf("tx.Commit() falhou: %v", err)
	}
	committedOrRolledBack = true // Commit bem-sucedido!
	t.Log("Transação commitada com sucesso.")

	// Verifica resultados
	var balance1, balance2 float64 // Usar float para NUMERIC
	err = ds.QueryRowContext(ctx, `SELECT balance FROM accounts_pg WHERE id = $1;`, 1).Scan(&balance1)
	if err != nil {
		t.Errorf("Scan de verificação falhou para id=1: %v", err)
	}
	err = ds.QueryRowContext(ctx, `SELECT balance FROM accounts_pg WHERE id = $1;`, 2).Scan(&balance2)
	if err != nil {
		t.Errorf("Scan de verificação falhou para id=2: %v", err)
	}

	// Cuidado com comparações de float! Usar uma margem pequena se necessário.
	if balance1 != 90.00 {
		t.Errorf("Esperado saldo 90.00 para id=1, obteve %.2f", balance1)
	}
	if balance2 != 210.50 {
		t.Errorf("Esperado saldo 210.50 para id=2, obteve %.2f", balance2)
	}
	t.Logf("Saldos verificados após commit: id1=%.2f, id2=%.2f", balance1, balance2)
}

// Testa o método PrepareContext com JSONB, usando DeepEqual para comparação.
func TestPostgres_PrepareContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, _ = ds.ExecContext(ctx, `DROP TABLE IF EXISTS test_prep_pg;`) // Garante limpeza
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_prep_pg (id SERIAL PRIMARY KEY, data JSONB);`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}

	// Prepara statement
	stmt, err := ds.PrepareContext(ctx, `INSERT INTO test_prep_pg (data) VALUES ($1);`)
	if err != nil {
		t.Fatalf("PrepareContext falhou: %v", err)
	}
	defer stmt.Close()
	t.Log("PrepareContext bem-sucedido.")

	// Executa múltiplas vezes com dados JSON (como string)
	inputs := []string{`{"value": 1}`, `{"value": 2, "tag": "teste"}`, `null`}
	for _, jsonData := range inputs {
		_, err := stmt.ExecContext(ctx, jsonData)
		if err != nil {
			t.Errorf("stmt.ExecContext para data '%s' falhou: %v", jsonData, err)
		}
	}
	t.Log("Statement preparado executado múltiplas vezes.")

	// Verifica os resultados (Lógica de Comparação Corrigida)
	rows, err := ds.QueryContext(ctx, `SELECT data FROM test_prep_pg ORDER BY id ASC;`)
	if err != nil {
		t.Fatalf("Query de verificação falhou: %v", err)
	}
	defer rows.Close()

	var resultsData []any // Armazena dados decodificados
	for rows.Next() {
		var dataBytes []byte // Lê JSONB como bytes brutos
		if err := rows.Scan(&dataBytes); err != nil {
			// Se Scan falhou, reporta erro
			t.Fatalf("Scan de verificação (bytes) falhou: %v", err)
		}

		// Verifica se Scan para []byte retorna nil para NULLs do banco
		// (Comportamento pode variar um pouco entre drivers/versões)
		if dataBytes == nil {
			resultsData = append(resultsData, nil)
			continue
		}

		// Decodifica os bytes JSON para interface{} genérica
		// Isso lida com objetos, arrays, strings, números, booleans, null JSON.
		var item interface{}
		if err := json.Unmarshal(dataBytes, &item); err != nil {
			t.Fatalf("Falha ao decodificar JSONB '%s' do banco: %v", string(dataBytes), err)
		}
		resultsData = append(resultsData, item)
	}
	if err := rows.Err(); err != nil { // Verifica erro pós-loop
		t.Errorf("Erro durante iteração de rows: %v", err)
	}

	// Prepara dados esperados (decodificados)
	var expectedData []interface{}
	for _, inputStr := range inputs {
		var item interface{}
		if err := json.Unmarshal([]byte(inputStr), &item); err != nil {
			// Se o input for "null", json.Unmarshal para interface{} resulta em nil
			if inputStr != "null" { // Evita erro desnecessário para "null" literal
				t.Fatalf("Falha ao decodificar JSON de input '%s': %v", inputStr, err)
			}
			// Append nil se o input for "null" ou se Unmarshal resultar em nil
			expectedData = append(expectedData, nil)
		} else {
			expectedData = append(expectedData, item)
		}
	}

	// Compara usando reflect.DeepEqual
	if len(resultsData) != len(expectedData) {
		t.Errorf("Número de resultados diferente. Esperado %d, obteve %d", len(expectedData), len(resultsData))
	} else if !reflect.DeepEqual(resultsData, expectedData) {
		// Log detalhado das diferenças se DeepEqual falhar
		t.Errorf("Dados JSONB não correspondem.\nEsperado: %#v\nObteve:   %#v", expectedData, resultsData)
	} else {
		t.Logf("Dados JSONB verificados com sucesso usando DeepEqual.")
	}
}
