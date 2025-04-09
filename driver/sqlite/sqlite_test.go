// driver/sqlite/sqlite_test.go
package sqlite_test // Pacote _test para testar apenas a API pública

import (
	"context"
	"database/sql" // Importa database/sql para sql.ErrNoRows etc.
	"errors"       // Importa errors para errors.Is
	"fmt"          // Importa fmt para comparações simples
	"path/filepath"
	"testing"
	"time"

	// Importa o pacote raiz do TypeGorm
	"github.com/chmenegatti/typegorm"
	// Importa o pacote específico do driver COM UM IDENTIFICADOR EM BRANCO (_)
	// Isso garante que sua função init() rode e registre o driver.
	// Também importamos nomeado para usar a struct Config.
	sqlite_driver "github.com/chmenegatti/typegorm/driver/sqlite"
)

// --- Função Auxiliar para obter DataSource conectado para testes ---
func getTestDataSource(t *testing.T) typegorm.DataSource {
	t.Helper() // Marca esta como uma função auxiliar de teste

	// Cria um diretório temporário para o banco de dados do teste.
	// O pacote testing limpa isso automaticamente após o teste.
	tempDir := t.TempDir()
	dbFilePath := filepath.Join(tempDir, "test_exec_query.db")
	t.Logf("getTestDataSource: Usando arquivo de banco de dados temporário: %s", dbFilePath)

	// Cria a configuração específica do driver.
	config := sqlite_driver.Config{
		Database: dbFilePath,
		Options:  map[string]string{"_journal": "WAL", "_busy_timeout": "5000"},
	}

	// Conecta usando a fábrica central do TypeGorm.
	dataSource, err := typegorm.Connect(config)
	if err != nil {
		t.Fatalf("getTestDataSource: typegorm.Connect() falhou: %v", err)
	}
	if dataSource == nil {
		t.Fatal("getTestDataSource: typegorm.Connect() retornou DataSource nulo sem erro")
	}

	// Garante que Close seja chamado quando o escopo do teste que usa este DS terminar.
	t.Cleanup(func() {
		t.Log("getTestDataSource Cleanup: Fechando conexão...")
		if err := dataSource.Close(); err != nil {
			t.Errorf("getTestDataSource Cleanup: dataSource.Close() falhou: %v", err)
		} else {
			t.Log("getTestDataSource Cleanup: Conexão fechada.")
		}
	})

	return dataSource
}

// --- Testes para os métodos da DataSource ---

// Testa a conexão e o ping (ainda relevante para garantir a base).
func TestSQLiteConnectionFactoryAndPing(t *testing.T) {
	ds := getTestDataSource(t) // Obtém a conexão usando o helper
	ctx := context.Background()
	t.Log("TestSQLiteConnectionFactoryAndPing: DataSource obtido.")

	// Testa o Ping
	t.Log("TestSQLiteConnectionFactoryAndPing: Pingando banco de dados...")
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := ds.Ping(pingCtx); err != nil {
		t.Fatalf("dataSource.Ping() falhou: %v", err)
	}
	t.Log("TestSQLiteConnectionFactoryAndPing: Ping bem-sucedido.")

	// Testa GetDriverType
	if driverType := ds.GetDriverType(); driverType != typegorm.SQLite {
		t.Errorf("dataSource.GetDriverType() = %q, esperado %q", driverType, typegorm.SQLite)
	} else {
		t.Logf("dataSource.GetDriverType() retornou tipo correto: %q", driverType)
	}
}

// Testa o método ExecContext.
func TestSQLite_ExecContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Cria uma tabela
	createTableSQL := `CREATE TABLE test_exec (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);`
	_, err := ds.ExecContext(ctx, createTableSQL)
	if err != nil {
		t.Fatalf("ExecContext (CREATE TABLE) falhou: %v", err)
	}
	t.Log("CREATE TABLE bem-sucedido.")

	// Insere dados
	insertSQL := `INSERT INTO test_exec (name) VALUES (?), (?);`
	result, err := ds.ExecContext(ctx, insertSQL, "Alice", "Bob")
	if err != nil {
		t.Fatalf("ExecContext (INSERT) falhou: %v", err)
	}
	t.Log("INSERT bem-sucedido.")

	// Verifica RowsAffected (Linhas Afetadas)
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Errorf("result.RowsAffected() falhou: %v", err)
	} else if rowsAffected != 2 {
		t.Errorf("Esperado 2 linhas afetadas, obteve %d", rowsAffected)
	} else {
		t.Logf("RowsAffected: %d (Correto)", rowsAffected)
	}

	// Verifica LastInsertId (ID da Última Inserção)
	// Pode não ser confiável ou suportado por todos os drivers/casos, mas deve funcionar para SQLite AUTOINCREMENT.
	lastID, err := result.LastInsertId()
	if err != nil {
		t.Logf("result.LastInsertId() retornou erro (pode ser esperado): %v", err)
	} else if lastID <= 0 {
		t.Errorf("Esperado LastInsertId positivo, obteve %d", lastID)
	} else {
		t.Logf("LastInsertId: %d", lastID) // Provavelmente será 2 se a tabela estava vazia
	}
}

// Testa o método QueryContext.
func TestSQLite_QueryContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup: Cria tabela e insere dados
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_query (id INTEGER PRIMARY KEY, value TEXT);`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO test_query (id, value) VALUES (1, 'um'), (2, 'dois');`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Consulta dados
	rows, err := ds.QueryContext(ctx, `SELECT id, value FROM test_query ORDER BY id ASC;`)
	if err != nil {
		t.Fatalf("QueryContext falhou: %v", err)
	}
	// IMPORTANTE: Garante que rows sejam fechados ao final
	defer rows.Close()

	count := 0
	ids := []int{}
	values := []string{}

	// Itera sobre as linhas
	for rows.Next() {
		count++
		var id int
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			t.Errorf("rows.Scan falhou: %v", err)
			continue // Continua para verificar outras linhas se possível
		}
		ids = append(ids, id)
		values = append(values, value)
	}

	// Verifica por erros durante a iteração
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err() reportou erro: %v", err)
	}

	// Verifica os resultados
	if count != 2 {
		t.Errorf("Esperado 2 linhas, obteve %d", count)
	}
	expectedIDs := []int{1, 2}
	expectedValues := []string{"um", "dois"}
	// Compara slices de forma simples (pode precisar de reflect.DeepEqual para tipos complexos)
	if fmt.Sprintf("%v", ids) != fmt.Sprintf("%v", expectedIDs) {
		t.Errorf("Esperado IDs %v, obteve %v", expectedIDs, ids)
	}
	if fmt.Sprintf("%v", values) != fmt.Sprintf("%v", expectedValues) {
		t.Errorf("Esperado values %v, obteve %v", expectedValues, values)
	}

	t.Logf("Query bem-sucedida, obteve %d linhas.", count)
}

// Testa o método QueryRowContext.
func TestSQLite_QueryRowContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_row (id INTEGER PRIMARY KEY, name TEXT);`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO test_row (id, name) VALUES (10, 'Carlos');`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Consulta linha existente
	var name string
	row := ds.QueryRowContext(ctx, `SELECT name FROM test_row WHERE id = ?;`, 10)
	if row == nil { // Verifica se QueryRowContext retornou nil (devido a erro interno)
		t.Fatal("QueryRowContext retornou nil inesperadamente")
	}
	err = row.Scan(&name) // Erro é verificado aqui no Scan
	if err != nil {
		t.Errorf("QueryRowContext / Scan falhou para linha existente: %v", err)
	} else if name != "Carlos" {
		t.Errorf("Esperado nome 'Carlos', obteve '%s'", name)
	} else {
		t.Logf("Scan bem-sucedido para linha existente, obteve nome: %s", name)
	}

	// Consulta linha inexistente
	var missingName string
	row = ds.QueryRowContext(ctx, `SELECT name FROM test_row WHERE id = ?;`, 99)
	if row == nil {
		t.Fatal("QueryRowContext retornou nil inesperadamente para linha inexistente")
	}
	err = row.Scan(&missingName)
	if err == nil {
		t.Error("Esperado erro ao escanear linha inexistente, obteve nil")
	} else if !errors.Is(err, sql.ErrNoRows) { // Verifica erro específico sql.ErrNoRows
		t.Errorf("Esperado sql.ErrNoRows, obteve: %v", err)
	} else {
		t.Logf("Obteve sql.ErrNoRows esperado para linha inexistente.")
	}
}

// Testa o método BeginTx.
func TestSQLite_BeginTx(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_tx (id INTEGER PRIMARY KEY, balance INTEGER);`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}
	_, err = ds.ExecContext(ctx, `INSERT INTO test_tx (id, balance) VALUES (1, 100), (2, 50);`)
	if err != nil {
		t.Fatalf("Setup falhou (INSERT): %v", err)
	}

	// Inicia Tx
	tx, err := ds.BeginTx(ctx, nil) // Usa opções padrão
	if err != nil {
		t.Fatalf("BeginTx falhou: %v", err)
	}
	t.Log("BeginTx bem-sucedido.")

	// Adia Rollback para garantir limpeza em caso de pânico ou erro não tratado
	// NOTA: É complexo garantir que Rollback só ocorra se Commit não foi chamado.
	// Uma função helper de transação de alto nível simplifica isso.
	// Aqui, confiaremos nos Rollbacks explícitos nos caminhos de erro.
	// Este defer é mais um "último recurso".
	rolledBack := false // Flag para evitar duplo rollback/commit
	defer func() {
		if !rolledBack {
			// Se recuperou de panic ou teste falhou sem commit/rollback explícito
			if p := recover(); p != nil {
				t.Log("Recuperado de panic, revertendo transação (rollback).")
				tx.Rollback() // Tenta reverter
				panic(p)      // Re-lança o panic
			} else if t.Failed() {
				t.Log("Teste falhou, revertendo transação (rollback).")
				tx.Rollback() // Tenta reverter
			}
			// Se chegou aqui sem falha e sem commit, algo está errado, mas rollback é seguro.
		}
	}()

	// Operações dentro da Tx
	_, err = tx.ExecContext(ctx, `UPDATE test_tx SET balance = balance - 10 WHERE id = 1;`)
	if err != nil {
		tx.Rollback() // Reverte em caso de erro
		rolledBack = true
		t.Fatalf("tx.ExecContext (UPDATE 1) falhou: %v", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE test_tx SET balance = balance + 10 WHERE id = 2;`)
	if err != nil {
		tx.Rollback() // Reverte em caso de erro
		rolledBack = true
		t.Fatalf("tx.ExecContext (UPDATE 2) falhou: %v", err)
	}

	// Comita a Tx
	if err = tx.Commit(); err != nil {
		rolledBack = true // Considera falha no commit como rollback implícito
		t.Fatalf("tx.Commit() falhou: %v", err)
	}
	rolledBack = true // Marca como finalizada (commit bem-sucedido)
	t.Log("Transação commitada com sucesso.")

	// Verifica resultados fora da transação
	var balance1, balance2 int
	// Usar QueryRowContext para buscar os saldos atualizados
	row1 := ds.QueryRowContext(ctx, `SELECT balance FROM test_tx WHERE id = 1;`)
	if row1 == nil {
		t.Fatal("QueryRow para id=1 retornou nil")
	}
	err = row1.Scan(&balance1)
	if err != nil {
		t.Errorf("Scan de verificação falhou para id=1: %v", err)
	}

	row2 := ds.QueryRowContext(ctx, `SELECT balance FROM test_tx WHERE id = 2;`)
	if row2 == nil {
		t.Fatal("QueryRow para id=2 retornou nil")
	}
	err = row2.Scan(&balance2)
	if err != nil {
		t.Errorf("Scan de verificação falhou para id=2: %v", err)
	}

	if balance1 != 90 {
		t.Errorf("Esperado saldo 90 para id=1, obteve %d", balance1)
	}
	if balance2 != 60 {
		t.Errorf("Esperado saldo 60 para id=2, obteve %d", balance2)
	}
	t.Logf("Saldos verificados após commit: id1=%d, id2=%d", balance1, balance2)
}

// Testa o método PrepareContext.
func TestSQLite_PrepareContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// Setup
	_, err := ds.ExecContext(ctx, `CREATE TABLE test_prep (id INTEGER PRIMARY KEY, value TEXT);`)
	if err != nil {
		t.Fatalf("Setup falhou (CREATE): %v", err)
	}

	// Prepara o statement
	stmt, err := ds.PrepareContext(ctx, `INSERT INTO test_prep (value) VALUES (?);`)
	if err != nil {
		t.Fatalf("PrepareContext falhou: %v", err)
	}
	t.Log("PrepareContext bem-sucedido.")
	// IMPORTANTE: Garante que o statement seja fechado
	defer stmt.Close()

	// Executa o statement preparado múltiplas vezes
	expectedValues := []string{"prep1", "prep2", "prep3"}
	for _, val := range expectedValues {
		result, err := stmt.ExecContext(ctx, val) // Executa o stmt preparado
		if err != nil {
			t.Errorf("stmt.ExecContext para valor '%s' falhou: %v", val, err)
			continue
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected != 1 {
			t.Errorf("Esperado 1 linha afetada para valor '%s', obteve %d", val, rowsAffected)
		}
	}
	t.Log("Statement preparado executado múltiplas vezes.")

	// Verifica os resultados
	rows, err := ds.QueryContext(ctx, `SELECT value FROM test_prep ORDER BY id ASC;`)
	if err != nil {
		t.Fatalf("Query de verificação falhou: %v", err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("Scan de verificação falhou: %v", err)
		}
		values = append(values, v)
	}
	if fmt.Sprintf("%v", values) != fmt.Sprintf("%v", expectedValues) {
		t.Errorf("Esperado values %v, obteve %v", expectedValues, values)
	}
	t.Logf("Valores inseridos verificados: %v", values)
}
