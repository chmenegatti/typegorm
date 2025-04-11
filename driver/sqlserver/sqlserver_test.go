// driver/sqlserver/sqlserver_test.go
package sqlserver_test // Sufixo _test para testar a API pública do pacote sqlserver (indiretamente via typegorm)

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	// Importa o pacote TypeGorm principal
	"github.com/chmenegatti/typegorm"
	// Importa a configuração do driver SQL Server
	sqlserver_driver "github.com/chmenegatti/typegorm/driver/sqlserver"

	// Importa o pacote de metadados para limpar o cache nos testes
	"github.com/chmenegatti/typegorm/metadata"
)

// --- Structs de Exemplo para Testes ---
// Reutilizadas de crud_test.go, mas o DDL no helper é específico do SQL Server.

// CrudTestModel simula uma entidade comum com PK auto-incremento e campos especiais.
type CrudTestModel struct {
	ID           uint         `typegorm:"primaryKey;autoIncrement"`               // Mapeia para INT IDENTITY
	Nome         string       `typegorm:"column:nome_modelo;notnull;uniqueIndex"` // Mapeia para NVARCHAR NOT NULL UNIQUE
	Email        *string      `typegorm:"unique;size:150"`                        // Mapeia para NVARCHAR(150) NULL UNIQUE
	Status       int          `typegorm:"default:1"`                              // Mapeia para INT DEFAULT 1
	CriadoEm     time.Time    `typegorm:"createdAt"`                              // Mapeia para DATETIME2 NOT NULL
	AtualizadoEm time.Time    `typegorm:"updatedAt"`                              // Mapeia para DATETIME2 NOT NULL
	DeletadoEm   sql.NullTime `typegorm:"deletedAt;index"`                        // Mapeia para DATETIME2 NULL
}

// ModelManualPK simula uma entidade com chave primária definida manualmente.
type ModelManualPK struct {
	Codigo    string  `typegorm:"primaryKey;size:36"` // Mapeia para NVARCHAR(36) PRIMARY KEY
	Descricao string  `typegorm:"notnull"`            // Mapeia para NVARCHAR(MAX) NOT NULL
	Valor     float64 `typegorm:"type:REAL"`          // Mapeia para REAL
	Ativado   bool    // Mapeia para BIT
}

// --- Helper para Configurar DB de Teste (SQL Server) ---
// Renomeado para consistência com outros testes de driver.
func getTestDataSource(t *testing.T) typegorm.DataSource {
	t.Helper()                    // Marca como função auxiliar de teste
	metadata.ClearMetadataCache() // Limpa cache de metadados antes de cada teste

	// -- Leitura da Configuração do Ambiente --
	host := os.Getenv("TEST_MSSQL_HOST")
	if host == "" {
		host = "localhost"
	}
	portStr := os.Getenv("TEST_MSSQL_PORT")
	if portStr == "" {
		portStr = "1433"
	}
	user := os.Getenv("TEST_MSSQL_USER")
	if user == "" {
		user = "sa"
	}
	password := os.Getenv("TEST_MSSQL_PASSWORD")
	if password == "" {
		password = "yourStrong(!)Password"
		// t.Skip("Pulando testes SQL Server: TEST_MSSQL_PASSWORD não definida")
	} // Pula se senha não definida
	dbName := os.Getenv("TEST_MSSQL_DBNAME")
	if dbName == "" {
		dbName = "master"
	} // Conecta ao master ou DB dedicado

	// Permite pular os testes se a flag não estiver ativa
	// if os.Getenv("RUN_MSSQL_TESTS") != "true" {
	// 	t.Skip("Pulando testes de integração SQL Server: RUN_MSSQL_TESTS não definida como 'true'")
	// }

	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("TEST_MSSQL_PORT inválido: %v", err)
	}

	// -- Criação da Configuração --
	config := sqlserver_driver.Config{
		Host: host, Port: port, Username: user, Password: password, Database: dbName,
		Params: map[string]string{"encrypt": "disable", "connect timeout": "30"}, // Params para dev/teste
	}
	t.Logf("getTestDataSource(SQLServer): Conectando a %s:%d (User: %s, DB: %s)", config.Host, config.Port, config.Username, config.Database)

	// -- Conexão via TypeGorm --
	ds, err := typegorm.Connect(config)
	if err != nil {
		t.Fatalf("getTestDataSource: typegorm.Connect() falhou: %v. Verifique conexão, credenciais, firewall, TLS.", err)
	}

	// -- Preparação do Schema (DDL T-SQL) --
	// -- Preparação do Schema (DDL T-SQL) --
	ctx := context.Background()
	// Limpeza inicial (DROP IF EXISTS) - Igual antes
	dropSQL1 := `DROP TABLE IF EXISTS crud_test_models;` // Drop já remove constraints/índices associados
	dropSQL2 := `DROP TABLE IF EXISTS model_manual_pks;`
	dropSQL3 := `DROP TABLE IF EXISTS test_prep_sqlserver;`
	_, _ = ds.ExecContext(ctx, dropSQL1)
	_, _ = ds.ExecContext(ctx, dropSQL2)
	_, _ = ds.ExecContext(ctx, dropSQL3)
	t.Log("Tabelas de teste antigas (se existiam) removidas.")

	// Criação das tabelas (definição das colunas igual)
	createSQL1 := `
	CREATE TABLE crud_test_models (
		id INT IDENTITY(1,1) PRIMARY KEY,
		nome_modelo NVARCHAR(100) NOT NULL,
		email NVARCHAR(150) NULL, -- Permite NULL
		status INT DEFAULT 1,
		criado_em DATETIME2 NOT NULL,
		atualizado_em DATETIME2 NOT NULL,
		deletado_em DATETIME2 NULL
	);`
	// Cria constraints e índices (ÍNDICE DE EMAIL MODIFICADO)
	createConstraints1 := `
    -- Constraint UNIQUE para nome (obrigatório, não nulo por DDL)
    ALTER TABLE crud_test_models ADD CONSTRAINT uq_crud_test_models_nome UNIQUE (nome_modelo);

    -- Índice UNIQUE FILTRADO para email (permite múltiplos NULLs, mas emails preenchidos devem ser únicos)
    CREATE UNIQUE NONCLUSTERED INDEX uq_crud_test_models_email_notnull ON crud_test_models(email) WHERE email IS NOT NULL; -- <-- MUDANÇA AQUI

    -- Índice para soft delete (igual)
    CREATE INDEX idx_crud_test_models_deletado_em ON crud_test_models (deletado_em) WHERE deletado_em IS NOT NULL;
    `
	createSQL2 := `
    CREATE TABLE model_manual_pks (
        codigo NVARCHAR(36) PRIMARY KEY,
        descricao NVARCHAR(MAX) NOT NULL,
        valor DECIMAL(10, 2), -- <-- MUDANÇA: Usar DECIMAL para precisão exata
        ativado BIT
    );`
	createSQL3 := `CREATE TABLE test_prep_sqlserver (id INT IDENTITY(1,1) PRIMARY KEY, data NVARCHAR(MAX));`

	// Executa os comandos de criação
	if _, err = ds.ExecContext(ctx, createSQL1); err != nil {
		ds.Close()
		t.Fatalf("Falha CREATE crud_test_models: %v", err)
	}
	if _, err = ds.ExecContext(ctx, createConstraints1); err != nil {
		ds.Close()
		t.Fatalf("Falha CREATE constraints crud_test_models: %v", err)
	}
	if _, err = ds.ExecContext(ctx, createSQL2); err != nil {
		ds.Close()
		t.Fatalf("Falha CREATE model_manual_pks: %v", err)
	}
	if _, err = ds.ExecContext(ctx, createSQL3); err != nil {
		ds.Close()
		t.Fatalf("Falha CREATE test_prep_sqlserver: %v", err)
	}
	t.Log("Tabelas de teste criadas com sucesso.")

	// -- Cleanup pós-teste --
	t.Cleanup(func() {
		t.Log("Cleanup: Fechando DataSource SQL Server...")
		ds.Close()
		metadata.ClearMetadataCache() // Limpa cache de metadados global
	})

	return ds // Retorna a DataSource pronta para o teste
}

// --- Testes (Adaptados para SQL Server) ---

// TestSQLServerConnectionFactoryAndPing verifica a conexão básica e o ping.
func TestSQLServerConnectionFactoryAndPing(t *testing.T) {
	// ... (igual, não usa parâmetros) ...
	ds := getTestDataSource(t)
	ctx := context.Background()
	t.Log("TestSQLServerConnectionFactoryAndPing: DataSource obtido com sucesso.")
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ds.Ping(pingCtx); err != nil {
		t.Fatalf("dataSource.Ping() falhou: %v", err)
	}
	t.Log("TestSQLServerConnectionFactoryAndPing: Ping bem-sucedido.")
	if driverType := ds.GetDriverType(); driverType != sqlserver_driver.SQLServer {
		t.Errorf("DriverType mismatch: %q vs %q", driverType, sqlserver_driver.SQLServer)
	}
}

func TestSQLServer_ExecContext(t *testing.T) {
	// Este já estava corrigido para usar sql.Named na resposta anterior
	// ... (código usando sql.Named para os dois inserts) ...
	ds := getTestDataSource(t)
	ctx := context.Background()
	now := time.Now()
	insertSQL := `INSERT INTO crud_test_models (nome_modelo, status, criado_em, atualizado_em) VALUES (@p1, @p2, @p3, @p4);`
	t.Log("Executando primeiro INSERT com parâmetros nomeados...")
	result1, err := ds.ExecContext(ctx, insertSQL, sql.Named("p1", "SQL Test 1 Named"), sql.Named("p2", 1), sql.Named("p3", now), sql.Named("p4", now))
	if err != nil {
		t.Fatalf("ExecContext (INSERT 1) falhou: %v", err)
	}
	rowsAffected1, _ := result1.RowsAffected()
	t.Logf("INSERT 1 OK (Rows: %d)", rowsAffected1) // LastInsertId removido antes
	t.Log("Executando segundo INSERT com parâmetros nomeados...")
	result2, err := ds.ExecContext(ctx, insertSQL, sql.Named("p1", "SQL Test 2 Named"), sql.Named("p2", 0), sql.Named("p3", now), sql.Named("p4", now))
	if err != nil {
		t.Fatalf("ExecContext (INSERT 2) falhou: %v", err)
	}
	rowsAffected2, _ := result2.RowsAffected()
	t.Logf("INSERT 2 OK (Rows: %d)", rowsAffected2) // LastInsertId removido antes
	if rowsAffected1 != 1 {
		t.Errorf("Esperado 1 RowsAffected para Insert 1, obteve %d", rowsAffected1)
	}
	if rowsAffected2 != 1 {
		t.Errorf("Esperado 1 RowsAffected para Insert 2, obteve %d", rowsAffected2)
	}
	t.Log("ExecContext (Inserts separados com Named Params) OK.")
}

func TestSQLServer_QueryContext(t *testing.T) {
	// CORRIGIDO: Usa sql.Named no INSERT do setup e na query principal
	ds := getTestDataSource(t)
	ctx := context.Background()
	now := time.Now()
	setupSQL := `INSERT INTO crud_test_models (nome_modelo, status, criado_em, atualizado_em) VALUES (@p1, @p2, @p3, @p4), (@p5, @p6, @p7, @p8);`
	_, err := ds.ExecContext(ctx, setupSQL,
		sql.Named("p1", "Busca A"), sql.Named("p2", 10), sql.Named("p3", now), sql.Named("p4", now),
		sql.Named("p5", "Busca B"), sql.Named("p6", 20), sql.Named("p7", now), sql.Named("p8", now))
	if err != nil {
		t.Fatalf("Setup Insert falhou: %v", err)
	}

	// Query usa @p1
	querySQL := `SELECT id, nome_modelo FROM crud_test_models WHERE status >= @p1 ORDER BY status ASC;`
	rows, err := ds.QueryContext(ctx, querySQL, sql.Named("p1", 15)) // Passa sql.Named
	if err != nil {
		t.Fatalf("QueryContext falhou: %v", err)
	}
	defer rows.Close()
	count := 0
	var nomes []string
	for rows.Next() {
		count++
		var id int
		var nome string
		if err := rows.Scan(&id, &nome); err != nil {
			t.Fatalf("Scan falhou: %v", err)
		}
		nomes = append(nomes, nome)
	}
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err(): %v", err)
	}
	if count != 1 {
		t.Errorf("Esperado 1 linha, obteve %d", count)
	}
	expectedNomes := []string{"Busca B"}
	if !reflect.DeepEqual(nomes, expectedNomes) {
		t.Errorf("Esperado %v, obteve %v", expectedNomes, nomes)
	}
	t.Logf("QueryContext OK, nomes: %v", nomes)
}

func TestSQLServer_QueryRowContext(t *testing.T) {
	// CORRIGIDO: Usa sql.Named no INSERT do setup e nas queries
	ds := getTestDataSource(t)
	ctx := context.Background()
	now := time.Now()
	nomeModelo := "UnicoRegistroSQLRow"
	// Setup Insert
	insertSQL := `INSERT INTO crud_test_models (nome_modelo, status, criado_em, atualizado_em) VALUES (@p1, @p2, @p3, @p4);`
	_, err := ds.ExecContext(ctx, insertSQL, sql.Named("p1", nomeModelo), sql.Named("p2", 5), sql.Named("p3", now), sql.Named("p4", now))
	if err != nil {
		t.Fatalf("Setup Insert falhou: %v", err)
	}
	t.Logf("Setup: Registro '%s' inserido.", nomeModelo)

	// Busca existente
	var status int
	row := ds.QueryRowContext(ctx, `SELECT status FROM crud_test_models WHERE nome_modelo = @p1;`, sql.Named("p1", nomeModelo))
	err = row.Scan(&status)
	if err != nil {
		t.Fatalf("Scan falhou para '%s': %v", nomeModelo, err)
	}
	if status != 5 {
		t.Errorf("Esperado status 5, obteve %d", status)
	} else {
		t.Logf("QueryRow OK para '%s', status: %d", nomeModelo, status)
	}

	// Busca inexistente
	row = ds.QueryRowContext(ctx, `SELECT status FROM crud_test_models WHERE nome_modelo = @p1;`, sql.Named("p1", "RegistroInexistente"))
	err = row.Scan(&status)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Esperado sql.ErrNoRows para 'Inexistente', obteve: %v", err)
	} else {
		t.Log("sql.ErrNoRows OK para 'Inexistente'")
	}
}

func TestSQLServer_Insert_UniqueConstraintError(t *testing.T) {
	// CORRIGIDO: Usa sql.Named nos INSERTs do setup
	ds := getTestDataSource(t)
	ctx := context.Background()
	now := time.Now()
	nomeUnico := "UnicoConstraintSQLServer2"
	insertSQL := `INSERT INTO crud_test_models (nome_modelo, status, criado_em, atualizado_em) VALUES (@p1, @p2, @p3, @p4);`
	// Insere primeiro
	_, err := ds.ExecContext(ctx, insertSQL, sql.Named("p1", nomeUnico), sql.Named("p2", 1), sql.Named("p3", now), sql.Named("p4", now))
	if err != nil {
		t.Fatalf("Setup Insert 1 falhou: %v", err)
	}
	// Tenta inserir segundo
	_, err = ds.ExecContext(ctx, insertSQL, sql.Named("p1", nomeUnico), sql.Named("p2", 2), sql.Named("p3", now), sql.Named("p4", now))
	if err == nil {
		t.Fatal("Esperado erro de constraint UNIQUE, mas obteve nil")
	}
	errorString := err.Error()
	if !strings.Contains(errorString, "UNIQUE KEY constraint") {
		t.Errorf("Esperado erro de UNIQUE constraint, obteve: %v", err)
	} else {
		t.Logf("Erro UNIQUE constraint OK: %v", err)
	}
}

func TestSQLServer_Insert_NonAutoIncrementPK(t *testing.T) {
	// Usa typegorm.Insert, NÃO PRECISA MUDAR aqui.
	// A verificação QueryRowContext ABAIXO precisa mudar.
	ds := getTestDataSource(t)
	ctx := context.Background()
	pkManual := "mssql-manual-pk-final"
	modelo := &ModelManualPK{Codigo: pkManual, Descricao: "Item PK Manual MS Ultimo", Valor: 1.01, Ativado: false}
	err := typegorm.Insert(ctx, ds, modelo)
	if err != nil {
		t.Fatalf("Insert falhou para PK manual: %v", err)
	}
	if modelo.Codigo != pkManual {
		t.Errorf("PK manual '%s' modificada para '%s'", pkManual, modelo.Codigo)
	}
	var dbCodigo string
	var dbDescricao string
	var dbValor float64
	var dbAtivado bool
	// CORRIGIDO: Usa @p1 e sql.Named na verificação
	row := ds.QueryRowContext(ctx, `SELECT codigo, descricao, valor, ativado FROM model_manual_pks WHERE codigo = @p1`, sql.Named("p1", pkManual))
	scanErr := row.Scan(&dbCodigo, &dbDescricao, &dbValor, &dbAtivado)
	if scanErr != nil {
		t.Fatalf("Scan de verificação falhou: %v", scanErr)
	}
	if dbCodigo != modelo.Codigo || dbDescricao != modelo.Descricao || dbValor != modelo.Valor || dbAtivado != modelo.Ativado {
		t.Errorf("Mismatch DB. Esperado %+v, Obteve %s,%s,%f,%v", modelo, dbCodigo, dbDescricao, dbValor, dbAtivado)
	}
	t.Log("Insert/Verificação PK Manual OK.")
}

func TestSQLServer_Update_Basic(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()
	emailInicial := "update-sql-basic@teste.com" // Email único para busca

	// 1. Setup: Insere registro inicial
	sourceModel := &CrudTestModel{
		Nome:   "Original Update Basic MS Final", // Nome único para busca
		Email:  &emailInicial,
		Status: 10,
	}
	if err := typegorm.Insert(ctx, ds, sourceModel); err != nil {
		t.Fatalf("Setup falhou: Insert inicial: %v", err)
	}
	// Neste ponto, sourceModel.ID NÃO é confiável para SQL Server.
	t.Logf("Registro inicial inserido (Nome: %s). ID não populado automaticamente.", sourceModel.Nome)

	// 1.1 Setup: Busca o registro inserido AGORA para obter o ID real e estado inicial
	initialModel := &CrudTestModel{}
	// Usa QueryRowContext direto com o nome (que é UNIQUE) para buscar o registro completo
	// Precisa selecionar todas as colunas que serão comparadas depois
	findSQL := `SELECT id, nome_modelo, email, status, criado_em, atualizado_em, deletado_em
	            FROM crud_test_models WHERE nome_modelo = @p1`
	err := ds.QueryRowContext(ctx, findSQL, sql.Named("p1", sourceModel.Nome)).Scan(
		&initialModel.ID, &initialModel.Nome, &initialModel.Email, &initialModel.Status,
		&initialModel.CriadoEm, &initialModel.AtualizadoEm, &initialModel.DeletadoEm,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("Setup falhou: Registro inserido com nome '%s' não foi encontrado na busca de verificação.", sourceModel.Nome)
		}
		t.Fatalf("Setup falhou: Não foi possível buscar o registro recém-inserido pelo nome: %v", err)
	}
	// Verifica se o ID foi realmente lido do banco
	if initialModel.ID == 0 {
		t.Fatal("Setup falhou: ID buscado após insert ainda é zero.")
	}
	t.Logf("Registro inicial buscado com ID %d: %+v", initialModel.ID, initialModel)

	// 2. Modifica a struct BUSCADA (initialModel), que agora tem o ID correto
	novoNome := "Atualizado Com Sucesso MS Final"
	novoEmail := "updated-sql-basic@final.com"
	novoStatus := 11
	initialModel.Nome = novoNome
	initialModel.Email = &novoEmail
	initialModel.Status = novoStatus
	// Zera DeletadoEm para garantir que não está setado antes do Update (se fosse o caso)
	initialModel.DeletadoEm = sql.NullTime{Valid: false}
	time.Sleep(10 * time.Millisecond)
	//beforeUpdate := time.Now()

	// 3. Executa Update usando a struct 'initialModel' que contém o ID correto
	t.Logf("Chamando typegorm.Update para ID %d: %+v", initialModel.ID, initialModel)
	err = typegorm.Update(ctx, ds, initialModel) // Passa initialModel (que é um ponteiro)

	// 4. Verifica erro do Update
	if err != nil {
		t.Fatalf("typegorm.Update falhou inesperadamente: %v", err)
	}
	t.Log("typegorm.Update executado sem erro.")

	// 5. Verifica Dados no Banco (usando FindByID com o ID conhecido)
	updatedModel := &CrudTestModel{}
	err = typegorm.FindByID(ctx, ds, updatedModel, initialModel.ID) // Usa o ID obtido
	if err != nil {
		t.Fatalf("Verificação falhou: Erro ao buscar registro atualizado com FindByID: %v", err)
	}
	t.Logf("Registro encontrado após Update: %+v", updatedModel)

	// 6. Compara campos atualizados (comparar com 'novoNome', 'novoEmail', 'novoStatus')
	if updatedModel.Nome != novoNome {
		t.Errorf("Nome mismatch: %s vs %s", novoNome, updatedModel.Nome)
	}
	if updatedModel.Email == nil || *updatedModel.Email != novoEmail {
		t.Errorf("Email mismatch: %s vs %v", novoEmail, updatedModel.Email)
	}
	if updatedModel.Status != novoStatus {
		t.Errorf("Status mismatch: %d vs %d", novoStatus, updatedModel.Status)
	}

	if updatedModel.AtualizadoEm.IsZero() {
		t.Errorf("AtualizadoEm é zero após update")
	} else if !updatedModel.AtualizadoEm.After(initialModel.AtualizadoEm) {
		// Verifica se o novo AtualizadoEm é estritamente DEPOIS do AtualizadoEm ANTES do update.
		// Isso permite que sejam iguais se o update for extremamente rápido ou houver truncamento.
		// Uma verificação melhor poderia usar uma pequena tolerância 'After(initialModel.AtualizadoEm.Add(-time.Microsecond))'
		// Mas vamos começar com After()
		t.Errorf("Verificação falhou: AtualizadoEm (%s) deveria ser após o timestamp anterior (%s)",
			updatedModel.AtualizadoEm, initialModel.AtualizadoEm)
	} else {
		t.Logf("AtualizadoEm OK: %s (era %s)", updatedModel.AtualizadoEm, initialModel.AtualizadoEm)
	}
	// Verifica se CriadoEm não foi alterado (comparando com o valor lido no passo 1.1)
	// Permite pequena diferença de nanosegundos que pode ocorrer na leitura/escrita de DATETIME2
	if updatedModel.CriadoEm.IsZero() {
		t.Errorf("CriadoEm é zero após update")
	} else if updatedModel.CriadoEm.Sub(initialModel.CriadoEm).Abs() > time.Millisecond { // Compara com tolerância
		t.Errorf("CriadoEm foi alterado inesperadamente. Original: %s, Atual: %s", initialModel.CriadoEm, updatedModel.CriadoEm)
	} else {
		t.Logf("CriadoEm OK (não alterado): %s", updatedModel.CriadoEm)
	}

	t.Log("Update Básico OK.")
}

func TestSQLServer_Update_NotFound(t *testing.T) {
	// Usa typegorm.Update. NÃO PRECISA MUDAR.
	ds := getTestDataSource(t)
	ctx := context.Background()
	nonExistentID := uint(666666)
	nonExistentModel := &CrudTestModel{ID: nonExistentID, Nome: "Nao Existe Update MS Final", Status: 1}
	err := typegorm.Update(ctx, ds, nonExistentModel)
	if err == nil {
		t.Fatal("Update deveria retornar erro, obteve nil")
	}
	expectedErrorSubstring := "registro com PK"
	expectedErrorSubstring2 := "não encontrado"
	if !strings.Contains(err.Error(), expectedErrorSubstring) || !strings.Contains(err.Error(), expectedErrorSubstring2) {
		t.Errorf("Erro inesperado: %v", err)
	} else {
		t.Logf("Erro Update não encontrado OK: %v", err)
	}
}

func TestSQLServer_Delete_Soft(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()

	// 1. Setup: Insere registro
	modelToInsert := &CrudTestModel{Nome: "Soft Delete SQLServer Test", Status: 1}
	if err := typegorm.Insert(ctx, ds, modelToInsert); err != nil {
		t.Fatalf("Setup falhou: Insert: %v", err)
	}
	// ID não é populado aqui para SQL Server

	// 1.1 Setup: Busca o registro para obter o ID real
	modelToDelete := &CrudTestModel{} // Struct para guardar o registro buscado (com ID)
	findSQL := `SELECT id, nome_modelo, email, status, criado_em, atualizado_em, deletado_em FROM crud_test_models WHERE nome_modelo = @p1`
	err := ds.QueryRowContext(ctx, findSQL, sql.Named("p1", modelToInsert.Nome)).Scan(
		&modelToDelete.ID, &modelToDelete.Nome, &modelToDelete.Email, &modelToDelete.Status,
		&modelToDelete.CriadoEm, &modelToDelete.AtualizadoEm, &modelToDelete.DeletadoEm,
	)
	if err != nil {
		t.Fatalf("Setup falhou: Não foi possível buscar registro recém-inserido: %v", err)
	}
	if modelToDelete.ID == 0 {
		t.Fatal("Setup falhou: ID buscado após insert ainda é zero.")
	}
	recordID := modelToDelete.ID // Usa o ID buscado do banco
	t.Logf("Registro inserido e buscado para Soft Delete com ID: %d", recordID)

	// 2. Executa Delete (que deve fazer Soft Delete) usando o modelo buscado (com ID)
	t.Logf("Chamando typegorm.Delete para ID %d (esperado Soft Delete)...", recordID)
	err = typegorm.Delete(ctx, ds, modelToDelete) // <--- Passa modelToDelete (com ID)

	// 3. Verifica Erro do Delete
	if err != nil {
		t.Fatalf("typegorm.Delete (Soft) falhou inesperadamente: %v", err)
	}
	t.Log("Delete (Soft) OK.")

	// 4. Verifica no Banco se DeletadoEm foi preenchido
	t.Logf("Verificando DeletadoEm no banco para ID = %d", recordID)
	var dbDeletadoEm sql.NullTime
	// Usa parâmetro nomeado na verificação também
	checkSQL := `SELECT deletado_em FROM crud_test_models WHERE id = @p1`
	err = ds.QueryRowContext(ctx, checkSQL, sql.Named("p1", recordID)).Scan(&dbDeletadoEm)
	if err != nil {
		t.Fatalf("Verificação falhou: Erro ao buscar registro após Delete: %v", err)
	}
	if !dbDeletadoEm.Valid || dbDeletadoEm.Time.IsZero() {
		t.Errorf("DeletadoEm inválido: %v", dbDeletadoEm)
	} else {
		t.Logf("Verificação DeletadoEm OK: %s", dbDeletadoEm.Time)
	}

	// 5. Verifica se FindByID agora retorna ErrNoRows para este ID
	t.Logf("Verificando se FindByID ignora o registro soft-deletado (ID %d)...", recordID)
	err = typegorm.FindByID(ctx, ds, &CrudTestModel{}, recordID) // FindByID não precisa de parâmetro nomeado
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("FindByID após Soft Delete: esperado sql.ErrNoRows, obteve %v", err)
	} else {
		t.Log("FindByID ignorou soft-delete OK.")
	}
}

func TestSQLServer_Delete_Hard(t *testing.T) {
	// Usa typegorm.Insert e typegorm.Delete.
	// A verificação QueryRowContext ABAIXO precisa mudar.
	ds := getTestDataSource(t)
	ctx := context.Background()
	model := &ModelManualPK{Codigo: "hard-delete-ms-final-again", Descricao: "Hard Delete Test MS Final Again"}
	if err := typegorm.Insert(ctx, ds, model); err != nil {
		t.Fatalf("Insert falhou: %v", err)
	}
	recordID := model.Codigo
	t.Logf("Inserido Codigo %s", recordID)
	err := typegorm.Delete(ctx, ds, model)
	if err != nil {
		t.Fatalf("Delete (Hard) falhou: %v", err)
	}
	t.Log("Delete (Hard) OK.")
	// CORRIGIDO: Usa @p1 e sql.Named na verificação
	var count int
	checkSQL := "SELECT COUNT(*) FROM model_manual_pks WHERE codigo = @p1"
	err = ds.QueryRowContext(ctx, checkSQL, sql.Named("p1", recordID)).Scan(&count)
	if err != nil {
		t.Fatalf("COUNT(*) falhou: %v", err)
	}
	if count != 0 {
		t.Errorf("Esperado COUNT=0 após Hard Delete, obteve %d", count)
	} else {
		t.Log("Verificação Hard Delete OK (COUNT=0).")
	}
}

func TestSQLServer_FindByID_IgnoresSoftDeleted(t *testing.T) {
	// Usa typegorm.Insert.
	// O ExecContext manual ABAIXO precisa mudar.
	// FindByID não precisa mudar.
	ds := getTestDataSource(t)
	ctx := context.Background()
	model := &CrudTestModel{Nome: "Ignore Soft Delete MS Final Again"}
	if err := typegorm.Insert(ctx, ds, model); err != nil {
		t.Fatalf("Insert falhou: %v", err)
	}
	recordID := model.ID
	t.Logf("Inserido ID %d", recordID)
	// CORRIGIDO: Usa @p1, @p2 e sql.Named no UPDATE manual
	updateSQL := `UPDATE crud_test_models SET deletado_em = @p1 WHERE id = @p2`
	softDeleteTime := time.Now()
	_, err := ds.ExecContext(ctx, updateSQL, sql.Named("p1", softDeleteTime), sql.Named("p2", recordID))
	if err != nil {
		t.Fatalf("UPDATE manual falhou: %v", err)
	}
	t.Logf("Soft delete manual aplicado para ID %d", recordID)
	err = typegorm.FindByID(ctx, ds, &CrudTestModel{}, recordID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("FindByID: esperado sql.ErrNoRows para soft-deleted, obteve %v", err)
	} else {
		t.Log("FindByID ignorou soft-delete OK.")
	}
}

func TestSQLServer_PrepareContext(t *testing.T) {
	ds := getTestDataSource(t)
	ctx := context.Background()
	// Prepara statement - usa '?' que PrepareContext deve entender
	stmt, err := ds.PrepareContext(ctx, `INSERT INTO test_prep_sqlserver (data) VALUES (@p1);`)
	if err != nil {
		t.Fatalf("PrepareContext falhou: %v", err)
	}
	defer stmt.Close() // Fecha o statement
	t.Log("PrepareContext bem-sucedido.")

	// Insere dados
	inputs := []string{"Texto Prep 1 MS VFinal", "Outro Texto Prep MS VFinal", "Terceiro SQLS VFinal"}
	var expectedInsertCount int64 = 0 // Contador para verificar inserções
	for i, val := range inputs {
		t.Logf("Executando stmt.ExecContext #%d para valor: '%s'", i+1, val)
		result, err := stmt.ExecContext(ctx, val) // Executa usando valor posicional
		if err != nil {
			// Falha imediatamente se qualquer insert retornar erro
			t.Fatalf("stmt.ExecContext #%d falhou para '%s': %v", i+1, val, err)
		}
		// Verifica RowsAffected (PODE não ser confiável, mas ajuda a diagnosticar)
		rowsAff, raErr := result.RowsAffected()
		if raErr != nil {
			t.Logf("Aviso: Não foi possível obter RowsAffected para insert #%d: %v", i+1, raErr)
			// Se RowsAffected não for suportado, contamos a tentativa como sucesso se não houve erro
			expectedInsertCount++
		} else if rowsAff != 1 {
			// Se RowsAffected retornar 0, algo deu errado no insert!
			t.Errorf("stmt.ExecContext #%d: Esperado 1 RowsAffected, obteve %d", i+1, rowsAff)
			// NÃO incrementa expectedInsertCount se rowsAff não for 1
		} else {
			expectedInsertCount++ // Incrementa só se afetou 1 linha
			t.Logf("stmt.ExecContext #%d OK (RowsAffected=1)", i+1)
		}
	}
	t.Logf("Prepare: Tentativa de inserir %d dados concluída. Esperadas %d inserções bem-sucedidas.", len(inputs), expectedInsertCount)

	// VERIFICAÇÃO ADICIONAL: Conta as linhas na tabela ANTES de tentar ler
	var actualCount int64
	countSQL := "SELECT COUNT(*) FROM test_prep_sqlserver"
	err = ds.QueryRowContext(ctx, countSQL).Scan(&actualCount)
	if err != nil {
		t.Fatalf("Falha ao executar COUNT(*) para verificação: %v", err)
	}
	t.Logf("Contagem atual na tabela test_prep_sqlserver: %d", actualCount)

	// Compara contagem atual com a esperada (baseada em RowsAffected ou número de loops sem erro)
	if actualCount != expectedInsertCount {
		t.Errorf("Contagem de linhas na tabela (%d) diferente do esperado (%d) após inserts.", actualCount, expectedInsertCount)
		// Se a contagem estiver errada, DeepEqual vai falhar, podemos parar aqui ou continuar para ver a leitura.
		t.FailNow() // Para imediatamente se a contagem não bater.
	}
	// Se a contagem esperada for 0, não adianta prosseguir para a leitura.
	if expectedInsertCount == 0 && actualCount == 0 {
		t.Log("Nenhuma linha foi inserida (conforme esperado ou devido a erro em RowsAffected), pulando verificação de leitura.")
		// Considerar o teste como PASS ou FAIL aqui? Depende se RowsAffected era esperado.
		// Por enquanto, se chegou aqui sem t.Fatalf, vamos considerar OK (embora estranho).
		return // Termina o teste aqui se nada foi inserido.
	}

	// Verifica dados lidos (Query e Scan)
	rows, err := ds.QueryContext(ctx, `SELECT data FROM test_prep_sqlserver ORDER BY id ASC;`)
	if err != nil {
		t.Fatalf("Query de verificação falhou: %v", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			t.Fatalf("Scan falhou: %v", err)
		}
		results = append(results, data) // Mantém sem TrimSpace por enquanto
	}
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err(): %v", err)
	}

	// Logs de Debug e Comparação Final (iguais)
	t.Logf("DEBUG: Inputs  (len=%d): %#v", len(inputs), inputs)
	t.Logf("DEBUG: Results (len=%d): %#v", len(results), results)
	if !reflect.DeepEqual(results, inputs) {
		t.Errorf("Dados preparados não correspondem. Esperado %d itens, obteve %d.\nEsperado: %#v\nObteve:   %#v", len(inputs), len(results), inputs, results)
	} else {
		t.Logf("PrepareContext OK, dados verificados via DeepEqual.")
	}
}
