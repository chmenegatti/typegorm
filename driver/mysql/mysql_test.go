// driver/mysql/mysql_test.go
package mysql_test // Pacote _test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/chmenegatti/typegorm"
	// Import config do driver e registra via init()
	mysql_driver "github.com/chmenegatti/typegorm/driver/mysql"
)

// --- Helper para obter DataSource conectado ---
func getTestDataSource(t *testing.T) typegorm.DataSource {
	t.Helper()

	// Lê config do ambiente ou usa defaults
	mysqlHost := os.Getenv("TEST_MYSQL_HOST")
	if mysqlHost == "" {
		mysqlHost = "localhost"
	} // 127.0.0.1 às vezes é mais confiável que localhost

	mysqlPortStr := os.Getenv("TEST_MYSQL_PORT")
	if mysqlPortStr == "" {
		mysqlPortStr = "3306"
	}

	mysqlUser := os.Getenv("TEST_MYSQL_USER")
	if mysqlUser == "" {
		mysqlUser = "admin"
	} // Usuário comum em dev

	mysqlPassword := os.Getenv("TEST_MYSQL_PASSWORD")
	// Senha root pode ser vazia ou 'password' em setups de teste
	if mysqlPassword == "" {
		mysqlPassword = "password"
	}

	mysqlDbName := os.Getenv("TEST_MYSQL_DBNAME")
	if mysqlDbName == "" {
		mysqlDbName = "nemesis"
	}

	// Permite pular testes
	// if os.Getenv("RUN_MYSQL_TESTS") != "true" {
	// 	t.Skip("Pulando testes de integração MySQL/MariaDB: RUN_MYSQL_TESTS não definida como 'true'")
	// }

	mysqlPort, err := strconv.Atoi(mysqlPortStr)
	if err != nil {
		t.Fatalf("Valor inválido para TEST_MYSQL_PORT: %v", err)
	}

	// Cria config (sem Params explícitos aqui, Connect adiciona parseTime etc.)
	config := mysql_driver.Config{
		Host:     mysqlHost,
		Port:     mysqlPort,
		Username: mysqlUser,
		Password: mysqlPassword,
		Database: mysqlDbName,
		// Params: map[string]string{"collation": "utf8mb4_unicode_ci"}, // Exemplo
	}

	t.Logf("getTestDataSource (MySQL): Conectando a %s@tcp(%s:%d)/%s", config.Username, config.Host, config.Port, config.Database)

	// Conecta via TypeGorm
	dataSource, err := typegorm.Connect(config)
	if err != nil {
		t.Fatalf("getTestDataSource: typegorm.Connect() falhou: %v. Garanta que o MySQL/MariaDB está rodando e acessível em %s:%d com usuário '%s' e banco '%s'.",
			err, config.Host, config.Port, config.Username, config.Database)
	}
	if dataSource == nil {
		t.Fatal("getTestDataSource: typegorm.Connect() retornou DataSource nulo sem erro")
	}

	// Cleanup
	t.Cleanup(func() {
		t.Log("getTestDataSource Cleanup (MySQL): Fechando conexão...")
		if err := dataSource.Close(); err != nil {
			t.Errorf("getTestDataSource Cleanup: dataSource.Close() falhou: %v", err)
		} else {
			t.Log("getTestDataSource Cleanup: Conexão fechada.")
		}
	})

	// Garante que tabelas de testes anteriores sejam limpas (melhor ter DB dedicado)
	ctx := context.Background()
	_, _ = dataSource.ExecContext(ctx, `DROP TABLE IF EXISTS test_exec_mysql;`)
	_, _ = dataSource.ExecContext(ctx, `DROP TABLE IF EXISTS test_query_mysql;`)
	_, _ = dataSource.ExecContext(ctx, `DROP TABLE IF EXISTS test_row_mysql;`)
	_, _ = dataSource.ExecContext(ctx, `DROP TABLE IF EXISTS accounts_mysql;`)
	_, _ = dataSource.ExecContext(ctx, `DROP TABLE IF EXISTS test_prep_mysql;`)

	return dataSource
}

// --- Testes (Adaptados do Postgres, com sintaxe MySQL/MariaDB) ---

func TestMySQLConnectionFactoryAndPing(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()
	t.Log("TestMySQLConnectionFactoryAndPing: DataSource obtido.")

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ds.Ping(pingCtx); err != nil {
		t.Fatalf("dataSource.Ping() falhou: %v", err)
	}
	t.Log("TestMySQLConnectionFactoryAndPing: Ping bem-sucedido.")

	if driverType := ds.GetDriverType(); driverType != typegorm.MySQL {
		t.Errorf("dataSource.GetDriverType() = %q, esperado %q", driverType, typegorm.MySQL)
	} else {
		t.Logf("dataSource.GetDriverType() retornou tipo correto: %q", driverType)
	}
}

func TestMySQL_ExecContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Cria tabela (AUTO_INCREMENT)
	createTableSQL := "CREATE TABLE test_exec_mysql (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(100) NOT NULL, data TEXT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;"
	_, err := ds.ExecContext(ctx, createTableSQL)
	if err != nil {
		t.Fatalf("ExecContext (CREATE TABLE) falhou: %v", err)
	}
	t.Log("CREATE TABLE bem-sucedido.")

	// Insere dados (placeholder é ?)
	insertSQL := `INSERT INTO test_exec_mysql (name, data) VALUES (?, ?), (?, ?);`
	result, err := ds.ExecContext(ctx, insertSQL, "Tucano", "Ave", "Tamanduá", "Mamífero")
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

	// Verifica LastInsertId (suportado pelo driver mysql)
	lastID, err := result.LastInsertId()
	if err != nil {
		t.Errorf("result.LastInsertId() retornou erro: %v", err)
	} else if lastID <= 0 {
		t.Errorf("Esperado LastInsertId positivo, obteve %d", lastID)
	} else {
		t.Logf("LastInsertId: %d", lastID)
	}
}

func TestMySQL_QueryContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_query_mysql (code INT PRIMARY KEY, value VARCHAR(50)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO test_query_mysql (code, value) VALUES (5, 'cinco'), (15, 'quinze'), (25, 'vinte e cinco');`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Consulta dados (placeholder ?)
	rows, err := ds.QueryContext(ctx, `SELECT code, value FROM test_query_mysql WHERE code >= ? ORDER BY code ASC;`, 10)
	if err != nil {
		t.Fatalf("QueryContext falhou: %v", err)
	}
	defer rows.Close()

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
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err() reportou erro: %v", err)
	}

	// Verifica resultados
	if count != 2 {
		t.Errorf("Esperado 2 linhas, obteve %d", count)
	}
	expectedCodes := []int{15, 25}
	expectedValues := []string{"quinze", "vinte e cinco"}
	if fmt.Sprintf("%v", codes) != fmt.Sprintf("%v", expectedCodes) {
		t.Errorf("Esperado Codes %v, obteve %v", expectedCodes, codes)
	}
	if fmt.Sprintf("%v", values) != fmt.Sprintf("%v", expectedValues) {
		t.Errorf("Esperado values %v, obteve %v", expectedValues, values)
	}
	t.Logf("Query bem-sucedida, obteve %d linhas.", count)
}

func TestMySQL_QueryRowContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_row_mysql (id INT AUTO_INCREMENT PRIMARY KEY, nickname VARCHAR(50) UNIQUE) ENGINE=InnoDB;`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO test_row_mysql (nickname) VALUES ('Boto cor-de-rosa');`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Consulta linha existente (placeholder ?)
	var nickname string
	row := ds.QueryRowContext(ctx, `SELECT nickname FROM test_row_mysql WHERE id = ?;`, 1)
	if row == nil {
		t.Fatal("QueryRowContext retornou nil inesperadamente")
	}
	err = row.Scan(&nickname)
	if err != nil {
		t.Errorf("Scan falhou para linha existente: %v", err)
	}
	if nickname != "Boto cor-de-rosa" {
		t.Errorf("Esperado 'Boto cor-de-rosa', obteve '%s'", nickname)
	}
	t.Logf("Scan bem-sucedido para linha existente, obteve: %s", nickname)

	// Consulta inexistente
	var missingNickname string
	row = ds.QueryRowContext(ctx, `SELECT nickname FROM test_row_mysql WHERE id = ?;`, 99)
	if row == nil {
		t.Fatal("QueryRowContext retornou nil inesperadamente para linha inexistente")
	}
	err = row.Scan(&missingNickname)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Esperado sql.ErrNoRows, obteve: %v", err)
	}
	t.Logf("Obteve sql.ErrNoRows esperado para linha inexistente.")
}

func TestMySQL_BeginTx(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup (usando DECIMAL)
	_, err := ds.ExecContext(ctx, `CREATE TABLE accounts_mysql (id INT PRIMARY KEY, balance DECIMAL(10, 2)) ENGINE=InnoDB;`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO accounts_mysql (id, balance) VALUES (10, 500.75), (20, 1000.00);`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Inicia Tx
	tx, err := ds.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx falhou: %v", err)
	}
	t.Log("BeginTx bem-sucedido.")

	committedOrRolledBack := false
	defer func() {
		if !committedOrRolledBack {
			t.Log("Defer: Rollback")
			tx.Rollback()
		}
	}()

	// Operações na Tx (placeholder ?)
	_, err = tx.ExecContext(ctx, `UPDATE accounts_mysql SET balance = balance - ? WHERE id = ?;`, 50.25, 10)
	if err != nil {
		tx.Rollback()
		committedOrRolledBack = true
		t.Fatalf("tx.ExecContext (UPDATE 1) falhou: %v", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts_mysql SET balance = balance + ? WHERE id = ?;`, 50.25, 20)
	if err != nil {
		tx.Rollback()
		committedOrRolledBack = true
		t.Fatalf("tx.ExecContext (UPDATE 2) falhou: %v", err)
	}

	// Comita Tx
	if err = tx.Commit(); err != nil {
		committedOrRolledBack = true
		t.Fatalf("tx.Commit() falhou: %v", err)
	}
	committedOrRolledBack = true
	t.Log("Transação commitada com sucesso.")

	// Verifica resultados (ler DECIMAL como string ou float64)
	var balance1, balance2 float64
	err = ds.QueryRowContext(ctx, `SELECT balance FROM accounts_mysql WHERE id = ?;`, 10).Scan(&balance1)
	if err != nil {
		t.Errorf("Scan de verificação falhou para id=10: %v", err)
	}
	err = ds.QueryRowContext(ctx, `SELECT balance FROM accounts_mysql WHERE id = ?;`, 20).Scan(&balance2)
	if err != nil {
		t.Errorf("Scan de verificação falhou para id=20: %v", err)
	}

	if balance1 != 450.50 {
		t.Errorf("Esperado saldo 450.50 para id=10, obteve %.2f", balance1)
	}
	if balance2 != 1050.25 {
		t.Errorf("Esperado saldo 1050.25 para id=20, obteve %.2f", balance2)
	}
	t.Logf("Saldos verificados após commit: id10=%.2f, id20=%.2f", balance1, balance2)
}

func TestMySQL_PrepareContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup (usando JSON type - disponível no MySQL 5.7+)
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_prep_mysql (id INT AUTO_INCREMENT PRIMARY KEY, data JSON) ENGINE=InnoDB;`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}

	// Prepara statement (placeholder ?)
	stmt, err := ds.PrepareContext(ctx, `INSERT INTO test_prep_mysql (data) VALUES (?);`)
	if err != nil {
		t.Fatalf("PrepareContext falhou: %v", err)
	}
	defer stmt.Close()
	t.Log("PrepareContext bem-sucedido.")

	// Executa múltiplas vezes
	inputs := []string{`{"nome": "Jabuti"}`, `[1, 2, "a"]`, `"texto simples"`, `null`}
	for _, jsonData := range inputs {
		_, err := stmt.ExecContext(ctx, jsonData)
		if err != nil {
			t.Errorf("stmt.ExecContext para data '%s' falhou: %v", jsonData, err)
		}
	}
	t.Log("Statement preparado executado múltiplas vezes.")

	// Verifica (comparação exata da string JSON pode funcionar melhor no MySQL)
	rows, err := ds.QueryContext(ctx, `SELECT data FROM test_prep_mysql ORDER BY id ASC;`)
	if err != nil {
		t.Fatalf("Query de verificação falhou: %v", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var data sql.NullString // Lê JSON como string/nullstring
		if err := rows.Scan(&data); err != nil {
			t.Fatalf("Scan de verificação falhou: %v", err)
		}
		if data.Valid {
			results = append(results, data.String)
		} else {
			results = append(results, "null")
		}
	}
	// No MySQL, a comparação de string JSON pode ser mais estável que no PG JSONB quanto à ordem.
	// Mas usar DeepEqual ainda é mais robusto se a ordem não for garantida.
	if fmt.Sprintf("%v", results) != fmt.Sprintf("%v", inputs) {
		t.Errorf("Esperado results %v, obteve %v", inputs, results)
	}
	t.Logf("Dados JSON verificados: %v", results)
}
