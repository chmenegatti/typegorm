// crud_test.go
package typegorm_test // Sufixo _test para testar o pacote typegorm externamente

import (
	"context"
	"database/sql" // Para verificar dados e sql.Null*
	"errors"
	"path/filepath"
	"testing"
	"time"

	// Importa o pacote sendo testado
	"github.com/chmenegatti/typegorm"
	// Importa config e driver específico (SQLite para teste fácil)

	sqlite_driver "github.com/chmenegatti/typegorm/driver/sqlite"

	// Importa pacote de metadados para limpar cache
	"github.com/chmenegatti/typegorm/metadata"
)

// --- Struct de Exemplo para Teste CRUD ---
// (Similar à usada no teste do parser)
type CrudTestModel struct {
	ID           uint      `typegorm:"primaryKey;autoIncrement"`
	Nome         string    `typegorm:"column:nome_modelo;notnull;uniqueIndex"` // Índice único implícito
	Email        *string   `typegorm:"unique;size:150"`                        // Ponteiro para nullable
	Status       int       `typegorm:"default:1"`
	CriadoEm     time.Time `typegorm:"createdAt"`
	AtualizadoEm time.Time `typegorm:"updatedAt"`
	// DeletadoEm   sql.NullTime `typegorm:"deletedAt"` // Para testes futuros de soft delete/find
}

// --- Helper para Configurar DB de Teste (SQLite) ---
func setupTestDB(t *testing.T) typegorm.DataSource {
	t.Helper()
	metadata.ClearMetadataCache() // Limpa cache de metadados antes de cada teste CRUD

	// Usa diretório temporário para o arquivo do banco
	tempDir := t.TempDir()
	dbFile := filepath.Join(tempDir, "crud_test.db")
	t.Logf("Usando arquivo de banco de dados temporário: %s", dbFile)

	// Configuração SQLite
	config := sqlite_driver.Config{
		Database: dbFile,
		Options:  map[string]string{"_journal": "WAL", "_busy_timeout": "5000"},
	}

	// Conecta usando TypeGorm
	ds, err := typegorm.Connect(config)
	if err != nil {
		t.Fatalf("Falha ao conectar ao SQLite para teste: %v", err)
	}

	// Cria a tabela ANTES de retornar o DataSource
	// Importante: O schema DEVE corresponder ao esperado pelo parser baseado nas tags da CrudTestModel
	createSQL := `
	CREATE TABLE IF NOT EXISTS crud_test_models (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		nome_modelo TEXT NOT NULL UNIQUE,
		email VARCHAR(150) UNIQUE,
		status INTEGER DEFAULT 1,
		criado_em DATETIME NOT NULL,
		atualizado_em DATETIME NOT NULL
		-- deletado_em DATETIME NULL -- Para futuro soft delete
	);`
	ctx := context.Background()
	_, err = ds.ExecContext(ctx, createSQL)
	if err != nil {
		ds.Close() // Tenta fechar se a criação da tabela falhar
		t.Fatalf("Falha ao criar tabela de teste 'crud_test_models': %v", err)
	}
	t.Log("Tabela 'crud_test_models' criada (ou já existia).")

	// Adiciona Cleanup para fechar a conexão no final do teste
	t.Cleanup(func() {
		t.Log("Cleanup: Fechando DataSource do teste...")
		ds.Close()
		metadata.ClearMetadataCache() // Limpa cache de metadados após o teste
		// O diretório temporário (e o arquivo .db) é removido automaticamente pelo 't.TempDir()'
	})

	return ds
}

// --- Teste para a Função Insert ---
func TestInsert_Basic(t *testing.T) {
	ds := setupTestDB(t) // Configura DB e tabela
	ctx := context.Background()

	// 1. Prepara a entidade para inserir (como ponteiro)
	email := "insert@exemplo.com"
	modelo := &CrudTestModel{
		Nome:  "Teste Inserção",
		Email: &email, // Usa ponteiro para string nullable
		// ID é zero (será auto-incrementado)
		// Status usará o default do banco (ou zero do Go se não houver default na DDL)
		// CriadoEm e AtualizadoEm serão definidos pelo Insert (via buildInsertArgs)
	}

	// 2. Chama typegorm.Insert
	t.Logf("Chamando typegorm.Insert para: %+v", modelo)
	err := typegorm.Insert(ctx, ds, modelo)

	// 3. Verifica Erro da Inserção
	if err != nil {
		t.Fatalf("typegorm.Insert falhou: %v", err)
	}
	t.Log("typegorm.Insert executado sem erro.")

	// 4. Verifica se a PK foi preenchida na struct
	if modelo.ID == 0 {
		t.Error("Esperado que o campo ID do modelo fosse preenchido com um valor > 0 após Insert, mas permaneceu 0.")
	} else {
		t.Logf("PK AutoIncrement preenchida na struct: ID = %d", modelo.ID)
	}

	// 5. Verifica os Dados Diretamente no Banco
	t.Logf("Verificando dados inseridos no banco para ID = %d", modelo.ID)
	var (
		dbNome         string
		dbEmail        sql.NullString // Usa sql.NullString para ler coluna potencialmente nula
		dbStatus       int
		dbCriadoEm     time.Time
		dbAtualizadoEm time.Time
	)
	// Usa QueryRowContext diretamente do DataSource para verificar
	selectSQL := `SELECT nome_modelo, email, status, criado_em, atualizado_em FROM crud_test_models WHERE id = ?` // SQLite usa ?
	row := ds.QueryRowContext(ctx, selectSQL, modelo.ID)
	scanErr := row.Scan(&dbNome, &dbEmail, &dbStatus, &dbCriadoEm, &dbAtualizadoEm)

	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			t.Fatalf("Verificação falhou: Registro com ID %d não encontrado no banco após Insert.", modelo.ID)
		} else {
			t.Fatalf("Verificação falhou: Erro ao escanear registro inserido: %v", scanErr)
		}
	}

	// Compara valores lidos com valores esperados
	if dbNome != modelo.Nome {
		t.Errorf("Verificação falhou: Nome esperado '%s', obteve '%s'", modelo.Nome, dbNome)
	}
	if !dbEmail.Valid || dbEmail.String != *modelo.Email {
		t.Errorf("Verificação falhou: Email esperado '%s', obteve '%v' (Valid: %v)", *modelo.Email, dbEmail.String, dbEmail.Valid)
	}
	// Verifica Status: Como não definimos na struct, pode ter pego o default do banco (1) ou zero do Go se a DDL falhou no default.
	// Vamos assumir que pegou o default 1 da DDL. Ajuste se necessário.
	if dbStatus != 1 {
		t.Errorf("Verificação falhou: Status esperado 1 (default), obteve %d", dbStatus)
	}
	// Verifica Timestamps (devem ter sido preenchidos)
	if dbCriadoEm.IsZero() {
		t.Error("Verificação falhou: CriadoEm não deveria ser zero time")
	} else {
		t.Logf("CriadoEm: %s", dbCriadoEm)
	}
	if dbAtualizadoEm.IsZero() {
		t.Error("Verificação falhou: AtualizadoEm não deveria ser zero time")
	} else {
		t.Logf("AtualizadoEm: %s", dbAtualizadoEm)
	}
	// CriadoEm e AtualizadoEm devem ser (quase) iguais no Insert
	if !dbCriadoEm.Equal(dbAtualizadoEm) {
		// Permite pequena diferença devido à execução
		if dbAtualizadoEm.Sub(dbCriadoEm) > time.Second {
			t.Errorf("Verificação falhou: CriadoEm e AtualizadoEm deveriam ser quase iguais, mas diferem: %s vs %s", dbCriadoEm, dbAtualizadoEm)
		}
	}

	t.Log("Verificação dos dados no banco bem-sucedida.")
}

// TODO: Adicionar teste para Insert com erro (ex: violação de constraint UNIQUE)
// TODO: Adicionar teste para Insert com struct sem PK auto-increment (se suportado)
