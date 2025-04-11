// crud_test.go
package typegorm_test // Sufixo _test para testar o pacote typegorm externamente

import (
	"context"
	"database/sql" // Para verificar dados e sql.Null*
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	// Importa o pacote sendo testado
	"github.com/chmenegatti/typegorm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Importa config e driver específico (SQLite para teste fácil)

	sqlite_driver "github.com/chmenegatti/typegorm/driver/sqlite"

	// Importa pacote de metadados para limpar cache
	"github.com/chmenegatti/typegorm/metadata"
)

// --- Struct de Exemplo para Teste CRUD ---
// (Similar à usada no teste do parser)
type CrudTestModel struct {
	ID           uint         `typegorm:"primaryKey;autoIncrement"`
	Nome         string       `typegorm:"column:nome_modelo;notnull;uniqueIndex"` // Índice único implícito
	Email        *string      `typegorm:"unique;size:150"`                        // Ponteiro para nullable
	Status       int          `typegorm:"default:1"`
	CriadoEm     time.Time    `typegorm:"createdAt"`
	AtualizadoEm time.Time    `typegorm:"updatedAt"`
	DeletadoEm   sql.NullTime `typegorm:"deletedAt"` // Para testes futuros de soft delete/find
}

type ModelManualPK struct {
	Codigo    string  `typegorm:"primaryKey;size:36"` // Ex: UUID ou código customizado
	Descricao string  `typegorm:"notnull"`
	Valor     float64 `typegorm:"type:REAL"` // SQLite não tem DECIMAL nativo
	Ativado   bool    // Sem default explícito aqui
}

// --- Helper para Configurar DB de Teste (SQLite) ---
func setupTestDB(t *testing.T) typegorm.DataSource {
	t.Helper()
	metadata.ClearMetadataCache() // Limpa cache de metadados antes de cada teste CRUD

	// Usa diretório temporário para o arquivo do banco
	tempDir := t.TempDir()
	dbFile := filepath.Join(tempDir, fmt.Sprintf("crud_test_%s.db", t.Name()))
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
	createSQL1 := `
	CREATE TABLE IF NOT EXISTS crud_test_models (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		nome_modelo TEXT NOT NULL UNIQUE,
		email VARCHAR(150) UNIQUE,
		status INTEGER DEFAULT 1,
		criado_em DATETIME NOT NULL,
		atualizado_em DATETIME NOT NULL,
		deletado_em DATETIME NULL
	);`
	// Schema DDL para ModelManualPK
	createSQL2 := `
    CREATE TABLE IF NOT EXISTS model_manual_pks (
        codigo VARCHAR(36) PRIMARY KEY,
        descricao TEXT NOT NULL,
        valor REAL,
        ativado BOOLEAN
    );`
	ctx := context.Background()
	if _, err = ds.ExecContext(ctx, createSQL1); err != nil {
		ds.Close()
		t.Fatalf("Falha ao criar tabela 'crud_test_models': %v", err)
	}
	if _, err = ds.ExecContext(ctx, createSQL2); err != nil {
		ds.Close()
		t.Fatalf("Falha ao criar tabela 'model_manual_pks': %v", err)
	}
	t.Log("Tabelas de teste criadas (ou já existiam).")

	// Adiciona Cleanup para fechar a conexão no final do teste
	t.Cleanup(func() {
		t.Log("Cleanup: Fechando DataSource do teste...")
		ds.Close()
		metadata.ClearMetadataCache() // Limpa cache de metadados após o teste
		// O diretório temporário (e o arquivo .db) é removido automaticamente pelo 't.TempDir()'
	})

	return ds
}

func setupTestDBWithData(t *testing.T) (typegorm.DataSource, []*CrudTestModel) {
	t.Helper()
	ds := setupTestDB(t) // Reutiliza o setup básico
	ctx := context.Background()

	// Dados de Teste
	email1 := "find.a@test.com"
	email3 := "find.c@test.com"
	modelos := []*CrudTestModel{
		{Nome: "Charlie", Email: &email3, Status: 10}, // ID 1 (provável)
		{Nome: "Alice", Status: 20},                   // ID 2 - Email NULL
		{Nome: "Bob", Email: &email1, Status: 10},     // ID 3
		{Nome: "David", Status: 30},                   // ID 4
		{Nome: "Eve", Status: 10},                     // ID 5
	}

	// Insere os dados usando typegorm.Insert
	insertedModels := make([]*CrudTestModel, len(modelos))
	for i, m := range modelos {
		err := typegorm.Insert(ctx, ds, m) // Passa o ponteiro
		if err != nil {
			// Se falhar aqui, o setup falhou, aborta o teste
			ds.Close() // Tenta fechar o DS antes de falhar
			t.Fatalf("Falha ao inserir dado de teste #%d (%s): %v", i+1, m.Nome, err)
		}
		// É importante pegar o modelo retornado pelo Insert se ele preencher o ID
		// Como Insert não retorna o modelo modificado, e LastInsertId não funciona no SQL Server,
		// precisamos buscar o ID separadamente se precisarmos dele com certeza,
		// mas para os testes de Find, podemos confiar na ordem de inserção para IDs sequenciais (exceto PK manual).
		// Vamos assumir IDs sequenciais para SQLite/PG/MySQL/SQLServer IDENTITY.
		insertedModels[i] = m // Guarda o modelo (com ID potencialmente preenchido)
		// Para garantir o ID no SQL Server, teríamos que buscar aqui.
		// Vamos simplificar e assumir que os IDs serão 1, 2, 3, 4, 5 para os testes.
		insertedModels[i].ID = uint(i + 1) // Define IDs manualmente para o teste
		t.Logf("Dado de teste '%s' inserido/assumido com ID %d", m.Nome, m.ID)
	}

	return ds, insertedModels
}

// --- Teste para a Função Insert ---
func TestInsert_Basic(t *testing.T) {
	ds := setupTestDB(t) // Configura DB e tabela
	ctx := context.Background()

	// 1. Prepara a entidade para inserir (como ponteiro)
	email := "insert@exemplo.com"
	modelo := &CrudTestModel{
		Nome:   "Teste Inserção",
		Email:  &email, // Usa ponteiro para string nullable
		Status: 1,      // <-- DEFINE EXPLICITAMENTE O VALOR DESEJADO
		// ID é zero (será auto-incrementado)
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

// Testa FindByID para um registro que existe.
func TestFindByID_Found(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()

	// 1. Setup: Insere um registro conhecido
	email := "findme@exemplo.com"
	sourceModel := &CrudTestModel{
		Nome:   "Registro Para Buscar",
		Email:  &email,
		Status: 7,
	}
	err := typegorm.Insert(ctx, ds, sourceModel)
	if err != nil {
		t.Fatalf("Setup falhou: Erro ao inserir registro de teste: %v", err)
	}
	if sourceModel.ID == 0 {
		t.Fatal("Setup falhou: ID do registro inserido é zero")
	}
	t.Logf("Registro de teste inserido com ID: %d", sourceModel.ID)
	// Zera os timestamps da struct original para comparação DeepEqual mais fácil,
	// pois os valores lidos podem ter granularidade diferente do time.Now() original.
	sourceModel.CriadoEm = time.Time{}
	sourceModel.AtualizadoEm = time.Time{}

	// 2. Executa FindByID
	foundModel := &CrudTestModel{} // Cria um ponteiro para a struct vazia
	t.Logf("Chamando typegorm.FindByID com ID: %d", sourceModel.ID)
	err = typegorm.FindByID(ctx, ds, foundModel, sourceModel.ID) // Passa o ponteiro e o ID

	// 3. Verifica Erro
	if err != nil {
		t.Fatalf("typegorm.FindByID falhou inesperadamente: %v", err)
	}
	t.Log("typegorm.FindByID executado sem erro.")

	// 4. Verifica Dados Encontrados
	// Zera timestamps lidos para comparar o resto
	foundModel.CriadoEm = time.Time{}
	foundModel.AtualizadoEm = time.Time{}

	// Compara os campos relevantes ou a struct inteira (cuidado com timestamps)
	if !reflect.DeepEqual(sourceModel, foundModel) {
		t.Errorf("Dados encontrados não batem com os originais.\nEsperado: %+v\nObtido:   %+v", sourceModel, foundModel)
	} else {
		t.Logf("Dados encontrados correspondem aos originais (ignorando timestamps): %+v", foundModel)
	}

	// Verificação específica de ponteiro (Email)
	if foundModel.Email == nil {
		t.Error("Email encontrado é nil, mas esperado um valor.")
	} else if *foundModel.Email != *sourceModel.Email {
		t.Errorf("Email encontrado '%s' diferente do esperado '%s'", *foundModel.Email, *sourceModel.Email)
	}

}

// Testa FindByID para um registro que NÃO existe.
func TestFindByID_NotFound(t *testing.T) {
	ds := setupTestDB(t) // Configura DB, mas não insere nada relevante
	ctx := context.Background()

	nonExistentID := uint(999999) // Um ID que certamente não existe

	// 1. Executa FindByID com ID inexistente
	notFoundModel := &CrudTestModel{}
	t.Logf("Chamando typegorm.FindByID com ID inexistente: %d", nonExistentID)
	err := typegorm.FindByID(ctx, ds, notFoundModel, nonExistentID)

	// 2. Verifica Erro
	if err == nil {
		t.Fatal("typegorm.FindByID deveria retornar erro para ID inexistente, mas retornou nil")
	}

	// 3. Verifica SE o erro é sql.ErrNoRows
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Erro inesperado retornado por FindByID. Esperado 'sql.ErrNoRows', obteve: %v (Tipo: %T)", err, err)
	} else {
		t.Logf("Recebeu 'sql.ErrNoRows' esperado para ID inexistente.")
	}

	// 4. Verifica se a struct destino não foi modificada (opcional)
	// Como Scan falhou, a struct deve permanecer com seus valores zero.
	emptyModel := &CrudTestModel{}
	if !reflect.DeepEqual(notFoundModel, emptyModel) {
		t.Errorf("Struct destino foi modificada inesperadamente após FindByID falhar: %+v", notFoundModel)
	}
}

func TestInsert_UniqueConstraintError(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()

	// 1. Insere o primeiro registro (bem-sucedido)
	nomeUnico := "Nome Unico Teste"
	modelo1 := &CrudTestModel{
		Nome:   nomeUnico,
		Status: 1,
	}
	err := typegorm.Insert(ctx, ds, modelo1)
	if err != nil {
		t.Fatalf("Setup falhou: Insert do primeiro registro falhou inesperadamente: %v", err)
	}
	if modelo1.ID == 0 {
		t.Fatal("Setup falhou: ID do primeiro registro não foi preenchido")
	}
	t.Logf("Primeiro registro inserido com ID %d e Nome '%s'", modelo1.ID, nomeUnico)

	// 2. Tenta inserir o segundo registro com o MESMO nome_modelo
	email2 := "outro@email.com"
	modelo2 := &CrudTestModel{
		Nome:   nomeUnico, // Nome repetido!
		Email:  &email2,
		Status: 2,
	}
	t.Logf("Tentando inserir segundo registro com nome repetido: %+v", modelo2)
	err = typegorm.Insert(ctx, ds, modelo2)

	// 3. Verifica se o erro ocorreu e se é o esperado
	if err == nil {
		t.Fatal("Esperado erro ao inserir registro com nome_modelo duplicado, mas obteve nil")
	}

	// Verifica o tipo/conteúdo do erro (depende do driver!)
	// Para SQLite, a mensagem geralmente contém "UNIQUE constraint failed"
	// Para outros bancos, a verificação seria diferente (ex: verificar código do erro)
	errorString := err.Error()
	expectedErrorSubstring := "UNIQUE constraint failed" // Específico do SQLite
	if !strings.Contains(errorString, expectedErrorSubstring) {
		t.Errorf("Erro inesperado retornado. Esperado erro contendo '%s', obteve: %v", expectedErrorSubstring, err)
	} else {
		t.Logf("Recebeu erro esperado de violação de constraint UNIQUE: %v", err)
	}

	// 4. (Opcional) Verifica se o segundo registro NÃO foi inserido
	var count int
	countErr := ds.QueryRowContext(ctx, "SELECT COUNT(*) FROM crud_test_models WHERE nome_modelo = ?", nomeUnico).Scan(&count)
	if countErr != nil {
		t.Errorf("Erro ao verificar contagem de registros após falha: %v", countErr)
	} else if count != 1 {
		t.Errorf("Esperado encontrar apenas 1 registro com o nome '%s', mas encontrou %d", nomeUnico, count)
	}
}

// Testa o Insert com uma struct que tem PK definida manualmente (não auto-increment).
func TestInsert_NonAutoIncrementPK(t *testing.T) {
	ds := setupTestDB(t) // Configura DB e tabela model_manual_pks
	ctx := context.Background()

	// 1. Prepara a entidade com PK manual
	pkManual := "codigo-manual-123"
	modelo := &ModelManualPK{
		Codigo:    pkManual, // Define a PK explicitamente
		Descricao: "Item com PK manual",
		Valor:     99.90,
		Ativado:   true,
	}

	// 2. Chama typegorm.Insert
	t.Logf("Chamando typegorm.Insert para ModeloManualPK: %+v", modelo)
	err := typegorm.Insert(ctx, ds, modelo)

	// 3. Verifica Erro
	if err != nil {
		t.Fatalf("typegorm.Insert falhou para PK manual: %v", err)
	}
	t.Log("typegorm.Insert para PK manual executado sem erro.")

	// 4. Verifica se a PK NÃO foi modificada (o valor original deve permanecer)
	if modelo.Codigo != pkManual {
		t.Errorf("A PK manual '%s' foi modificada inesperadamente para '%s' após Insert", pkManual, modelo.Codigo)
	}
	// Verificar logs: Não deve haver log "[LOG-CRUD] PK AutoIncrement definida..." para este caso.

	// 5. Verifica os Dados Diretamente no Banco
	t.Logf("Verificando dados inseridos no banco para Codigo = %s", pkManual)
	var (
		dbCodigo    string
		dbDescricao string
		dbValor     float64
		dbAtivado   bool
	)
	selectSQL := `SELECT codigo, descricao, valor, ativado FROM model_manual_pks WHERE codigo = ?`
	row := ds.QueryRowContext(ctx, selectSQL, pkManual)
	scanErr := row.Scan(&dbCodigo, &dbDescricao, &dbValor, &dbAtivado)

	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			t.Fatalf("Verificação falhou: Registro com Codigo '%s' não encontrado no banco após Insert.", pkManual)
		} else {
			t.Fatalf("Verificação falhou: Erro ao escanear registro inserido: %v", scanErr)
		}
	}

	// Compara valores lidos com valores esperados
	if dbCodigo != modelo.Codigo {
		t.Errorf("Codigo mismatch: %s vs %s", modelo.Codigo, dbCodigo)
	}
	if dbDescricao != modelo.Descricao {
		t.Errorf("Descricao mismatch: %s vs %s", modelo.Descricao, dbDescricao)
	}
	if dbValor != modelo.Valor {
		t.Errorf("Valor mismatch: %f vs %f", modelo.Valor, dbValor)
	}
	if dbAtivado != modelo.Ativado {
		t.Errorf("Ativado mismatch: %v vs %v", modelo.Ativado, dbAtivado)
	}

	t.Log("Verificação dos dados (PK Manual) no banco bem-sucedida.")
}

// Testa o Update básico de um registro existente.
func TestUpdate_Basic(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()

	// 1. Setup: Insere registro inicial
	emailInicial := "update@teste.com"
	sourceModel := &CrudTestModel{
		Nome:   "Registro Original",
		Email:  &emailInicial,
		Status: 1,
	}
	if err := typegorm.Insert(ctx, ds, sourceModel); err != nil {
		t.Fatalf("Setup falhou: Insert inicial: %v", err)
	}
	if sourceModel.ID == 0 {
		t.Fatal("Setup falhou: ID não preenchido no Insert")
	}
	t.Logf("Registro inicial inserido com ID %d", sourceModel.ID)

	// Guarda timestamps originais para comparação
	criadoOriginal := sourceModel.CriadoEm

	// 2. Modifica a struct em memória
	novoNome := "Registro Atualizado"
	novoEmail := "updated@teste.com"
	novoStatus := 5
	sourceModel.Nome = novoNome       // Modifica Nome
	sourceModel.Email = &novoEmail    // Modifica Email
	sourceModel.Status = novoStatus   // Modifica Status
	time.Sleep(10 * time.Millisecond) // Pequena pausa para garantir que o timestamp de update será diferente
	beforeUpdate := time.Now()

	// 3. Executa Update
	t.Logf("Chamando typegorm.Update para ID %d: %+v", sourceModel.ID, sourceModel)
	err := typegorm.Update(ctx, ds, sourceModel)

	// 4. Verifica erro do Update
	if err != nil {
		t.Fatalf("typegorm.Update falhou inesperadamente: %v", err)
	}
	t.Log("typegorm.Update executado sem erro.")

	// 5. Verifica Dados no Banco (usando FindByID)
	updatedModel := &CrudTestModel{}
	err = typegorm.FindByID(ctx, ds, updatedModel, sourceModel.ID)
	if err != nil {
		t.Fatalf("Verificação falhou: Erro ao buscar registro atualizado com FindByID: %v", err)
	}
	t.Logf("Registro encontrado após Update: %+v", updatedModel)

	// Compara campos atualizados
	if updatedModel.Nome != novoNome {
		t.Errorf("Verificação falhou: Nome esperado '%s', obteve '%s'", novoNome, updatedModel.Nome)
	}
	if updatedModel.Email == nil || *updatedModel.Email != novoEmail {
		t.Errorf("Verificação falhou: Email esperado '%s', obteve '%v'", novoEmail, updatedModel.Email)
	}
	if updatedModel.Status != novoStatus {
		t.Errorf("Verificação falhou: Status esperado %d, obteve %d", novoStatus, updatedModel.Status)
	}

	// Verifica Timestamps
	if updatedModel.CriadoEm.IsZero() || updatedModel.CriadoEm.Equal(criadoOriginal) || updatedModel.CriadoEm.After(beforeUpdate) {
		// CriadoEm não deve mudar no Update, deve ser igual ao original (com alguma margem para precisão do DB)
		// Aqui apenas verificamos se não é zero e se não é igual ao AtualizadoEm (a menos que update seja muito rápido)
		// Uma verificação mais precisa compararia com o criadoOriginal.
		t.Errorf("Verificação falhou: CriadoEm inesperado: %s (Original era ~%s)", updatedModel.CriadoEm, criadoOriginal)
	} else {
		t.Logf("CriadoEm verificado: %s", updatedModel.CriadoEm)
	}

	if updatedModel.AtualizadoEm.IsZero() || updatedModel.AtualizadoEm.Before(beforeUpdate) {
		t.Errorf("Verificação falhou: AtualizadoEm (%s) deveria ser após o início do update (%s)", updatedModel.AtualizadoEm, beforeUpdate)
	} else {
		t.Logf("AtualizadoEm verificado: %s", updatedModel.AtualizadoEm)
	}

	if updatedModel.CriadoEm.Equal(updatedModel.AtualizadoEm) && time.Since(beforeUpdate) > 5*time.Millisecond {
		// Se passou algum tempo, eles deveriam ser diferentes
		t.Errorf("Verificação falhou: CriadoEm e AtualizadoEm são iguais (%s), mas deveriam diferir após Update", updatedModel.CriadoEm)
	}
}

// Testa a falha do Update para um registro que não existe.
func TestUpdate_NotFound(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()

	nonExistentID := uint(999999)
	nonExistentModel := &CrudTestModel{
		ID:     nonExistentID, // Usa um ID que não existe
		Nome:   "Nao Existe",
		Status: 1,
	}

	t.Logf("Tentando chamar typegorm.Update para ID inexistente %d", nonExistentID)
	err := typegorm.Update(ctx, ds, nonExistentModel)

	// Verifica se o erro esperado ocorreu (registro não encontrado / 0 linhas afetadas)
	if err == nil {
		t.Fatal("typegorm.Update deveria retornar erro para ID inexistente, mas retornou nil")
	}

	// Verifica se o erro contém a mensagem esperada de "registro não encontrado"
	expectedErrorSubstring := "registro com PK" // Parte da mensagem de erro em Update
	expectedErrorSubstring2 := "não encontrado"
	if !strings.Contains(err.Error(), expectedErrorSubstring) || !strings.Contains(err.Error(), expectedErrorSubstring2) {
		t.Errorf("Erro inesperado retornado por Update. Esperado erro contendo '%s' e '%s', obteve: %v", expectedErrorSubstring, expectedErrorSubstring2, err)
	} else {
		t.Logf("Recebeu erro esperado ao tentar atualizar registro inexistente: %v", err)
	}
}

// Testa o Soft Delete (atualiza DeletadoEm).
func TestDelete_Soft(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()

	// 1. Setup: Insere registro
	modelToSoftDelete := &CrudTestModel{Nome: "Para Soft Delete", Status: 1}
	if err := typegorm.Insert(ctx, ds, modelToSoftDelete); err != nil {
		t.Fatalf("Setup falhou: Insert: %v", err)
	}
	if modelToSoftDelete.ID == 0 {
		t.Fatal("Setup falhou: ID zero após Insert")
	}
	recordID := modelToSoftDelete.ID
	t.Logf("Registro inserido para Soft Delete com ID: %d", recordID)

	// 2. Executa Delete (que deve fazer Soft Delete por causa do DeletadoEm na struct)
	t.Logf("Chamando typegorm.Delete para ID %d (esperado Soft Delete)...", recordID)
	err := typegorm.Delete(ctx, ds, modelToSoftDelete)

	// 3. Verifica Erro do Delete
	if err != nil {
		t.Fatalf("typegorm.Delete (Soft) falhou inesperadamente: %v", err)
	}
	t.Log("typegorm.Delete (Soft) executado sem erro.")

	// 4. Verifica no Banco se DeletadoEm foi preenchido
	t.Logf("Verificando DeletadoEm no banco para ID = %d", recordID)
	var dbDeletadoEm sql.NullTime
	checkSQL := `SELECT deletado_em FROM crud_test_models WHERE id = ?`
	err = ds.QueryRowContext(ctx, checkSQL, recordID).Scan(&dbDeletadoEm)
	if err != nil {
		t.Fatalf("Verificação falhou: Erro ao buscar registro após Delete: %v", err)
	}

	if !dbDeletadoEm.Valid {
		t.Error("Verificação falhou: DeletadoEm deveria estar Válido (não NULL) após Soft Delete, mas está NULL.")
	} else if dbDeletadoEm.Time.IsZero() {
		t.Error("Verificação falhou: DeletadoEm.Time não deveria ser Zero após Soft Delete.")
	} else {
		t.Logf("Verificação bem-sucedida: DeletadoEm no banco é: %s (Valid: %v)", dbDeletadoEm.Time, dbDeletadoEm.Valid)
	}

	// 5. Verifica se FindByID agora retorna ErrNoRows para este ID
	t.Logf("Verificando se FindByID ignora o registro soft-deletado (ID %d)...", recordID)
	notFoundModel := &CrudTestModel{}
	err = typegorm.FindByID(ctx, ds, notFoundModel, recordID)

	if err == nil {
		t.Error("FindByID deveria retornar erro para registro soft-deletado, mas retornou nil")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Erro inesperado retornado por FindByID após Soft Delete. Esperado sql.ErrNoRows, obteve: %v", err)
	} else {
		t.Log("FindByID retornou sql.ErrNoRows esperado para registro soft-deletado.")
	}
}

// Testa o Hard Delete (DELETE FROM ...) para struct sem DeletadoEm.
func TestDelete_Hard(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()

	// 1. Setup: Insere registro com PK manual
	modelToHardDelete := &ModelManualPK{Codigo: "hard-delete-1", Descricao: "Registro para Hard Delete"}
	if err := typegorm.Insert(ctx, ds, modelToHardDelete); err != nil {
		t.Fatalf("Setup falhou: Insert: %v", err)
	}
	recordID := modelToHardDelete.Codigo
	t.Logf("Registro inserido para Hard Delete com Codigo: %s", recordID)

	// 2. Executa Delete (deve fazer Hard Delete)
	t.Logf("Chamando typegorm.Delete para Codigo %s (esperado Hard Delete)...", recordID)
	err := typegorm.Delete(ctx, ds, modelToHardDelete) // Passa o modelo com a PK preenchida

	// 3. Verifica Erro do Delete
	if err != nil {
		t.Fatalf("typegorm.Delete (Hard) falhou inesperadamente: %v", err)
	}
	t.Log("typegorm.Delete (Hard) executado sem erro.")

	// 4. Verifica no Banco se o registro realmente sumiu
	t.Logf("Verificando ausência no banco para Codigo = %s", recordID)
	var count int
	// Tentamos contar quantos registros existem com aquele código
	checkSQL := `SELECT COUNT(*) FROM model_manual_pks WHERE codigo = ?`
	err = ds.QueryRowContext(ctx, checkSQL, recordID).Scan(&count)
	if err != nil {
		// Scan não deve falhar aqui, pois COUNT(*) sempre retorna uma linha
		t.Fatalf("Verificação falhou: Erro ao executar COUNT(*) após Delete: %v", err)
	}

	if count != 0 {
		t.Errorf("Verificação falhou: Esperado COUNT=0 para registro deletado (Hard Delete), mas obteve %d", count)
	} else {
		t.Log("Verificação bem-sucedida: Registro não encontrado no banco após Hard Delete (COUNT=0).")
	}
}

// Testa explicitamente se FindByID ignora registros que sofreram soft delete.
// Redundante com TestDelete_Soft, mas bom para clareza.
func TestFindByID_IgnoresSoftDeleted(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()

	// 1. Setup: Insere um registro
	model := &CrudTestModel{Nome: "Para Soft Delete Direto", Status: 1}
	if err := typegorm.Insert(ctx, ds, model); err != nil {
		t.Fatalf("Setup falhou: Insert: %v", err)
	}
	recordID := model.ID
	t.Logf("Registro inserido com ID: %d", recordID)

	// 2. Setup: Aplica Soft Delete manualmente via SQL direto
	softDeleteTime := time.Now()
	updateSQL := `UPDATE crud_test_models SET deletado_em = ? WHERE id = ?`
	_, err := ds.ExecContext(ctx, updateSQL, softDeleteTime, recordID)
	if err != nil {
		t.Fatalf("Setup falhou: Não foi possível aplicar soft delete manual: %v", err)
	}
	t.Logf("Soft delete manual aplicado para ID %d com tempo %s", recordID, softDeleteTime)

	// 3. Executa FindByID para o ID soft-deletado
	foundModel := &CrudTestModel{}
	t.Logf("Chamando FindByID para registro soft-deletado (ID %d)...", recordID)
	err = typegorm.FindByID(ctx, ds, foundModel, recordID)

	// 4. Verifica se retornou ErrNoRows
	if err == nil {
		t.Error("FindByID deveria retornar erro para registro soft-deletado, mas retornou nil")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Erro inesperado retornado por FindByID. Esperado sql.ErrNoRows, obteve: %v", err)
	} else {
		t.Log("FindByID retornou sql.ErrNoRows esperado para registro soft-deletado.")
	}
}

func TestFind_NoOptions(t *testing.T) {
	ds, insertedModels := setupTestDBWithData(t)
	ctx := context.Background()

	var results []CrudTestModel // Slice de valores (não ponteiros)

	t.Log("Chamando typegorm.Find sem opções...")
	err := typegorm.Find(ctx, ds, &results, nil) // Passa ponteiro para o slice

	require.NoError(t, err, "typegorm.Find não deveria retornar erro sem opções")
	assert.Len(t, results, len(insertedModels), "Número de resultados diferente do esperado")
	t.Logf("Find sem opções retornou %d registros.", len(results))
	// Comparação mais profunda pode ser adicionada se necessário
}

// Testa Find com uma condição WHERE simples.
func TestFind_WhereSimple(t *testing.T) {
	ds, _ := setupTestDBWithData(t) // Ignora modelos inseridos, conhecemos os dados
	ctx := context.Background()

	var results []*CrudTestModel // Slice de ponteiros

	opts := &typegorm.FindOptions{
		Where: map[string]any{
			// Busca por status=10. Placeholders dependem do driver!
			// buildSelectQuery usará getPlaceholder internamente.
			// A chave do mapa PRECISA incluir o placeholder.
			"status = ?": 10, // Chave inclui placeholder genérico '?' que será adaptado
		},
	}

	t.Logf("Chamando typegorm.Find com Where: %v", opts.Where)
	err := typegorm.Find(ctx, ds, &results, opts)

	require.NoError(t, err, "typegorm.Find com Where simples falhou")
	require.Len(t, results, 3, "Esperado 3 registros com status=10")
	t.Logf("Find com Where simples retornou %d registros.", len(results))

	// Verifica se todos os resultados têm o status correto
	for _, r := range results {
		assert.Equal(t, 10, r.Status, "Registro retornado tem status incorreto")
	}
}

// Testa Find com múltiplas condições WHERE (AND).
func TestFind_WhereMultiple(t *testing.T) {
	ds, _ := setupTestDBWithData(t)
	ctx := context.Background()

	var results []CrudTestModel

	emailPattern := "find.%" // Padrão para email LIKE
	status := 10
	opts := &typegorm.FindOptions{
		Where: map[string]any{
			"status = ?":       status,
			"email LIKE ?":     emailPattern, // Busca emails que começam com "find."
			"nome_modelo != ?": "Eve",        // Exclui um dos nomes
		},
	}

	t.Logf("Chamando typegorm.Find com Where múltiplo: %v", opts.Where)
	err := typegorm.Find(ctx, ds, &results, opts)

	require.NoError(t, err, "typegorm.Find com Where múltiplo falhou")
	// Esperamos 2 resultados: Charlie (status 10, email find.c) e Bob (status 10, email find.a)
	// Eve (status 10) é excluída pelo nome. Alice (status 20) e David (status 30) pelo status.
	require.Len(t, results, 2, "Esperado 2 registros com status=10 e email LIKE 'find.%' e nome != Eve")
	t.Logf("Find com Where múltiplo retornou %d registros.", len(results))

	for _, r := range results {
		assert.Equal(t, status, r.Status, "Status incorreto")
		require.NotNil(t, r.Email, "Email não deveria ser nil")
		assert.True(t, strings.HasPrefix(*r.Email, "find."), "Email não corresponde ao padrão LIKE")
		assert.NotEqual(t, "Eve", r.Nome, "Nome não deveria ser Eve")
	}
}

// Testa Find com ORDER BY.
func TestFind_OrderBy(t *testing.T) {
	ds, _ := setupTestDBWithData(t)
	ctx := context.Background()

	var results []CrudTestModel

	opts := &typegorm.FindOptions{
		OrderBy: []string{"nome_modelo DESC"}, // Ordena por nome descendente
	}

	t.Logf("Chamando typegorm.Find com OrderBy: %v", opts.OrderBy)
	err := typegorm.Find(ctx, ds, &results, opts)

	require.NoError(t, err, "typegorm.Find com OrderBy falhou")
	require.Len(t, results, 5, "Esperado 5 registros totais") // Deve retornar todos

	// Verifica a ordem
	expectedOrder := []string{"Eve", "David", "Charlie", "Bob", "Alice"}
	actualOrder := make([]string, len(results))
	for i, r := range results {
		actualOrder[i] = r.Nome
	}
	assert.Equal(t, expectedOrder, actualOrder, "Ordem dos resultados incorreta")
	t.Logf("Find com OrderBy retornou a ordem correta: %v", actualOrder)
}

// Testa Find com LIMIT.
func TestFind_Limit(t *testing.T) {
	ds, _ := setupTestDBWithData(t)
	ctx := context.Background()
	var results []CrudTestModel
	opts := &typegorm.FindOptions{Limit: 2} // Limita a 2 resultados

	t.Logf("Chamando typegorm.Find com Limit: %d", opts.Limit)
	err := typegorm.Find(ctx, ds, &results, opts)

	require.NoError(t, err, "typegorm.Find com Limit falhou")
	assert.Len(t, results, 2, "Esperado 2 registros devido ao Limit")
	t.Logf("Find com Limit retornou %d registros.", len(results))
}

// Testa Find com OFFSET.
func TestFind_Offset(t *testing.T) {
	ds, _ := setupTestDBWithData(t)
	ctx := context.Background()
	var results []CrudTestModel
	// Ordena por ID para ter um offset previsível
	opts := &typegorm.FindOptions{Offset: 3, OrderBy: []string{"id ASC"}} // Pula os 3 primeiros

	t.Logf("Chamando typegorm.Find com Offset: %d (OrderBy ID ASC)", opts.Offset)
	err := typegorm.Find(ctx, ds, &results, opts)

	require.NoError(t, err, "typegorm.Find com Offset falhou")
	// Esperamos 2 resultados (total 5 - offset 3 = 2)
	assert.Len(t, results, 2, "Esperado 2 registros após Offset 3")
	// Verifica se os IDs são os últimos (assumindo 1 a 5)
	if len(results) == 2 {
		assert.Equal(t, uint(4), results[0].ID, "Primeiro resultado após offset incorreto") // ID 4
		assert.Equal(t, uint(5), results[1].ID, "Segundo resultado após offset incorreto")  // ID 5
	}
	t.Logf("Find com Offset retornou %d registros (IDs %d, %d).", len(results), results[0].ID, results[1].ID)
}

// Testa Find com LIMIT e OFFSET combinados.
func TestFind_LimitOffset(t *testing.T) {
	ds, _ := setupTestDBWithData(t)
	ctx := context.Background()
	var results []CrudTestModel
	// Pula 2, pega os próximos 2 (IDs 3 e 4)
	opts := &typegorm.FindOptions{Limit: 2, Offset: 2, OrderBy: []string{"id ASC"}}

	t.Logf("Chamando typegorm.Find com Limit: %d, Offset: %d (OrderBy ID ASC)", opts.Limit, opts.Offset)
	err := typegorm.Find(ctx, ds, &results, opts)

	require.NoError(t, err, "typegorm.Find com Limit/Offset falhou")
	assert.Len(t, results, 2, "Esperado 2 registros")
	if len(results) == 2 {
		assert.Equal(t, uint(3), results[0].ID, "Primeiro resultado incorreto") // ID 3
		assert.Equal(t, uint(4), results[1].ID, "Segundo resultado incorreto")  // ID 4
	}
	t.Logf("Find com Limit/Offset retornou %d registros (IDs %d, %d).", len(results), results[0].ID, results[1].ID)
}

// Testa Find quando nenhuma linha corresponde ao critério WHERE.
func TestFind_NotFound(t *testing.T) {
	ds, _ := setupTestDBWithData(t)
	ctx := context.Background()
	var results []CrudTestModel
	opts := &typegorm.FindOptions{Where: map[string]any{"nome_modelo = ?": "Nome Que Nao Existe"}}

	t.Logf("Chamando typegorm.Find esperando nenhum resultado...")
	err := typegorm.Find(ctx, ds, &results, opts)

	require.NoError(t, err, "typegorm.Find não deveria retornar erro quando nada é encontrado")
	assert.Len(t, results, 0, "Esperado 0 registros")
	assert.Empty(t, results, "Slice de resultados deveria estar vazio")
	t.Log("Find esperando nenhum resultado retornou 0 registros, como esperado.")
}

// Testa erro ao usar coluna inválida no OrderBy.
func TestFind_InvalidOrderByColumn(t *testing.T) {
	ds := setupTestDB(t) // Não precisa de dados pré-inseridos
	ctx := context.Background()
	var results []CrudTestModel
	opts := &typegorm.FindOptions{OrderBy: []string{"coluna_invalida ASC"}}

	t.Log("Chamando typegorm.Find com coluna de ordenação inválida...")
	err := typegorm.Find(ctx, ds, &results, opts)

	require.Error(t, err, "Esperado erro para coluna de ordenação inválida")
	assert.Contains(t, err.Error(), "coluna de ordenação inválida", "Mensagem de erro não contém o texto esperado")
	t.Logf("Erro esperado recebido para coluna inválida: %v", err)
}

// Testa erro ao usar direção inválida no OrderBy.
func TestFind_InvalidOrderByDirection(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()
	var results []CrudTestModel
	opts := &typegorm.FindOptions{OrderBy: []string{"nome_modelo INVAL"}} // Direção inválida

	t.Log("Chamando typegorm.Find com direção de ordenação inválida...")
	err := typegorm.Find(ctx, ds, &results, opts)

	require.Error(t, err, "Esperado erro para direção de ordenação inválida")
	assert.Contains(t, err.Error(), "direção de ordenação inválida", "Mensagem de erro não contém o texto esperado")
	t.Logf("Erro esperado recebido para direção inválida: %v", err)
}

// Testa erro ao usar chave Where sem placeholder (na implementação atual).
func TestFind_InvalidWherePlaceholder(t *testing.T) {
	ds := setupTestDB(t)
	ctx := context.Background()
	var results []CrudTestModel
	opts := &typegorm.FindOptions{Where: map[string]any{"status": 1}} // Falta ' = ?' na chave

	t.Log("Chamando typegorm.Find com chave Where sem placeholder...")
	err := typegorm.Find(ctx, ds, &results, opts)

	require.Error(t, err, "Esperado erro para chave Where sem placeholder")
	assert.Contains(t, err.Error(), "deve conter placeholder", "Mensagem de erro não contém o texto esperado")
	t.Logf("Erro esperado recebido para chave Where inválida: %v", err)
}
