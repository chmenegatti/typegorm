// metadata/parser_test.go
package metadata_test // Usar _test para testar como um pacote externo

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	// Importa o pacote a ser testado
	"github.com/chmenegatti/typegorm/metadata"
)

// --- Structs de Exemplo para os Testes ---

type ModeloBasico struct {
	ID    uint   `typegorm:"primaryKey;autoIncrement"`
	Nome  string `typegorm:"column:nome_usuario;uniqueIndex:idx_nome_unico"`
	Email string // Deve inferir coluna 'email'
	Ativo bool   `typegorm:"default:true"`
	Bio   string `typegorm:"-"` // Campo ignorado
	// campoPrivado string // Campo não exportado (ignorado automaticamente)
}

type TiposCompletos struct {
	ID           int64      `typegorm:"pk"` // pk é alias para primaryKey
	TextoLongo   string     `typegorm:"type:TEXT"`
	Preco        float64    `typegorm:"type:decimal(10,2);precision:10;scale:2"` // Inclui precision/scale
	Quantidade   int        `typegorm:"default:0"`
	DataEvento   time.Time  `typegorm:"type:timestamp with time zone"`
	DataNula     *time.Time // Ponteiro infere nullable=true
	Flag         bool
	MaybeFlag    *bool          // Ponteiro infere nullable=true
	Contrato     []byte         `typegorm:"type:bytea"` // Exemplo para Postgres bytea
	Descricao    sql.NullString // Tipos sql.Null* inferem nullable=true
	Contador     sql.NullInt64
	Percentual   sql.NullFloat64
	Confirmado   sql.NullBool
	AgendadoEm   sql.NullTime
	CriadoEm     time.Time    `typegorm:"createdAt"`
	AtualizadoEm time.Time    `typegorm:"updatedAt"`
	DeletadoEm   sql.NullTime `typegorm:"deletedAt;index"` // Soft delete com índice
}

type Constraints struct {
	Ref         string  `typegorm:"column:ref_id;unique;size:50"`
	Opcional    *string `typegorm:"nullable"` // nullable explícito (embora *string já seja)
	Obrigatorio string  `typegorm:"notnull"`
	TamanhoMax  string  `typegorm:"size:200"`
	PadraoStr   string  `typegorm:"default:'PENDENTE'"` // Default string com aspas
	PadraoNum   int     `typegorm:"default:1"`
}

type ErroTag struct {
	Nome string `typegorm:"size:abc"` // 'abc' não é um número válido
}

// --- Funções Auxiliares de Teste ---

// findColumn por nome do campo Go
func findColumn(meta *metadata.EntityMetadata, fieldName string) *metadata.ColumnMetadata {
	if meta == nil || meta.ColumnsByName == nil {
		return nil
	}
	col, ok := meta.ColumnsByName[fieldName]
	if !ok {
		return nil
	}
	return col
}

// --- Testes ---

func TestParse_ModeloBasico(t *testing.T) {
	metadata.ClearMetadataCache()          // Limpa cache antes do teste
	t.Cleanup(metadata.ClearMetadataCache) // Garante limpeza depois

	meta, err := metadata.Parse(ModeloBasico{})
	if err != nil {
		t.Fatalf("Parse(ModeloBasico) falhou: %v", err)
	}

	// Verifica metadados da entidade
	if meta == nil {
		t.Fatal("Parse retornou metadata nil sem erro")
	}
	if meta.Name != "ModeloBasico" {
		t.Errorf("Esperado Name 'ModeloBasico', obteve '%s'", meta.Name)
	}
	if meta.TableName != "modelo_basicos" {
		t.Errorf("Esperado TableName 'modelo_basicos', obteve '%s'", meta.TableName)
	} // snake_case + s
	if len(meta.Columns) != 4 {
		t.Fatalf("Esperado 4 colunas mapeadas, obteve %d", len(meta.Columns))
	} // ID, Nome, Email, Ativo (Bio e campoPrivado ignorados)
	if len(meta.PrimaryKeyColumns) != 1 {
		t.Fatalf("Esperado 1 coluna PK, obteve %d", len(meta.PrimaryKeyColumns))
	}
	if meta.PrimaryKeyColumns[0].FieldName != "ID" {
		t.Errorf("Esperado PK no campo 'ID', obteve '%s'", meta.PrimaryKeyColumns[0].FieldName)
	}

	// Verifica metadados da coluna ID
	colID := findColumn(meta, "ID")
	if colID == nil {
		t.Fatal("Coluna 'ID' não encontrada")
	}
	if !colID.IsPrimaryKey {
		t.Error("Coluna 'ID' deveria ser IsPrimaryKey=true")
	}
	if !colID.IsAutoIncrement {
		t.Error("Coluna 'ID' deveria ser IsAutoIncrement=true")
	}
	if colID.ColumnName != "id" {
		t.Errorf("Esperado ColumnName 'id', obteve '%s'", colID.ColumnName)
	} // snake_case
	if colID.IsNullable {
		t.Error("Coluna 'ID' (PK) não deveria ser IsNullable=true")
	}

	// Verifica metadados da coluna Nome
	colNome := findColumn(meta, "Nome")
	if colNome == nil {
		t.Fatal("Coluna 'Nome' não encontrada")
	}
	if colNome.IsPrimaryKey {
		t.Error("Coluna 'Nome' não deveria ser IsPrimaryKey=true")
	}
	if colNome.ColumnName != "nome_usuario" {
		t.Errorf("Esperado ColumnName 'nome_usuario' (da tag), obteve '%s'", colNome.ColumnName)
	}
	if colNome.UniqueIndexName == "" {
		t.Errorf("Esperado UniqueIndexName não vazio para 'Nome', obteve vazio")
	}
	if !strings.Contains(colNome.UniqueIndexName, "idx_nome_unico") {
		t.Errorf("Esperado UniqueIndexName contendo 'idx_nome_unico', obteve '%s'", colNome.UniqueIndexName)
	}
	if !colNome.IsUnique {
		t.Error("Coluna 'Nome' com uniqueIndex deveria ser IsUnique=true")
	}
	if colNome.IsNullable {
		t.Error("Coluna 'Nome' (string) não deveria ser IsNullable=true por padrão")
	}

	// Verifica metadados da coluna Email
	colEmail := findColumn(meta, "Email")
	if colEmail == nil {
		t.Fatal("Coluna 'Email' não encontrada")
	}
	if colEmail.ColumnName != "email" {
		t.Errorf("Esperado ColumnName 'email' (inferido), obteve '%s'", colEmail.ColumnName)
	}
	if colEmail.IsNullable {
		t.Error("Coluna 'Email' (string) não deveria ser IsNullable=true por padrão")
	}

	// Verifica metadados da coluna Ativo
	colAtivo := findColumn(meta, "Ativo")
	if colAtivo == nil {
		t.Fatal("Coluna 'Ativo' não encontrada")
	}
	if colAtivo.ColumnName != "ativo" {
		t.Errorf("Esperado ColumnName 'ativo', obteve '%s'", colAtivo.ColumnName)
	}
	if colAtivo.DefaultValue != "true" {
		t.Errorf("Esperado DefaultValue 'true', obteve '%s'", colAtivo.DefaultValue)
	}
	if colAtivo.IsNullable {
		t.Error("Coluna 'Ativo' (bool) não deveria ser IsNullable=true por padrão")
	}

	// Verifica se Bio foi ignorado
	if findColumn(meta, "Bio") != nil {
		t.Error("Coluna 'Bio' deveria ter sido ignorada pela tag '-'")
	}
}

func TestParse_TiposCompletos(t *testing.T) {
	metadata.ClearMetadataCache()
	t.Cleanup(metadata.ClearMetadataCache)

	meta, err := metadata.Parse(TiposCompletos{})
	if err != nil {
		t.Fatalf("Parse(TiposCompletos) falhou: %v", err)
	}
	if meta == nil {
		t.Fatal("Parse retornou metadata nil sem erro")
	}

	// Verifica tipos e nullability inferidos e explícitos
	tests := []struct {
		fieldName       string
		expectedDBType  string // Vazio se não especificado na tag
		expectNullable  bool
		expectCreatedAt bool
		expectUpdatedAt bool
		expectDeletedAt bool
	}{
		{"ID", "", false, false, false, false},
		{"TextoLongo", "TEXT", false, false, false, false},
		{"Preco", "decimal(10,2)", false, false, false, false}, // tipo explícito
		{"Quantidade", "", false, false, false, false},
		{"DataEvento", "timestamp with time zone", false, false, false, false}, // tipo explícito
		{"DataNula", "", true, false, false, false},                            // Ponteiro *time.Time -> nullable
		{"Flag", "", false, false, false, false},
		{"MaybeFlag", "", true, false, false, false},     // Ponteiro *bool -> nullable
		{"Contrato", "bytea", true, false, false, false}, // tipo explícito, slice []byte -> nullable
		{"Descricao", "", true, false, false, false},     // sql.NullString -> nullable
		{"Contador", "", true, false, false, false},      // sql.NullInt64 -> nullable
		{"Percentual", "", true, false, false, false},    // sql.NullFloat64 -> nullable
		{"Confirmado", "", true, false, false, false},    // sql.NullBool -> nullable
		{"AgendadoEm", "", true, false, false, false},    // sql.NullTime -> nullable
		{"CriadoEm", "", false, true, false, false},      // createdAt
		{"AtualizadoEm", "", false, false, true, false},  // updatedAt
		{"DeletadoEm", "", true, false, false, true},     // deletedAt (sql.NullTime -> nullable)
	}

	if len(meta.Columns) != len(tests) {
		t.Fatalf("Esperado %d colunas, obteve %d", len(tests), len(meta.Columns))
	}

	for _, tt := range tests {
		col := findColumn(meta, tt.fieldName)
		if col == nil {
			t.Errorf("Coluna '%s' não encontrada", tt.fieldName)
			continue
		}
		if tt.expectedDBType != "" && col.DBType != tt.expectedDBType {
			t.Errorf("Campo '%s': Esperado DBType '%s', obteve '%s'", tt.fieldName, tt.expectedDBType, col.DBType)
		}
		if col.IsNullable != tt.expectNullable {
			t.Errorf("Campo '%s': Esperado IsNullable=%v, obteve %v", tt.fieldName, tt.expectNullable, col.IsNullable)
		}
		if col.IsCreatedAt != tt.expectCreatedAt {
			t.Errorf("Campo '%s': Esperado IsCreatedAt=%v, obteve %v", tt.fieldName, tt.expectCreatedAt, col.IsCreatedAt)
		}
		if col.IsUpdatedAt != tt.expectUpdatedAt {
			t.Errorf("Campo '%s': Esperado IsUpdatedAt=%v, obteve %v", tt.fieldName, tt.expectUpdatedAt, col.IsUpdatedAt)
		}
		if col.IsDeletedAt != tt.expectDeletedAt {
			t.Errorf("Campo '%s': Esperado IsDeletedAt=%v, obteve %v", tt.fieldName, tt.expectDeletedAt, col.IsDeletedAt)
		}

		// Verifica Precision/Scale para Preco
		if tt.fieldName == "Preco" {
			if col.Precision != 10 {
				t.Errorf("Campo 'Preco': Esperado Precision=10, obteve %d", col.Precision)
			}
			if col.Scale != 2 {
				t.Errorf("Campo 'Preco': Esperado Scale=2, obteve %d", col.Scale)
			}
		}
	}

	// Verifica ponteiros de colunas especiais na entidade
	if meta.CreatedAtColumn == nil || meta.CreatedAtColumn.FieldName != "CriadoEm" {
		t.Error("EntityMetadata.CreatedAtColumn não foi definido corretamente")
	}
	if meta.UpdatedAtColumn == nil || meta.UpdatedAtColumn.FieldName != "AtualizadoEm" {
		t.Error("EntityMetadata.UpdatedAtColumn não foi definido corretamente")
	}
	if meta.DeletedAtColumn == nil || meta.DeletedAtColumn.FieldName != "DeletadoEm" {
		t.Error("EntityMetadata.DeletedAtColumn não foi definido corretamente")
	}
}

func TestParse_Constraints(t *testing.T) {
	metadata.ClearMetadataCache()
	t.Cleanup(metadata.ClearMetadataCache)

	meta, err := metadata.Parse(Constraints{})
	if err != nil {
		t.Fatalf("Parse(Constraints) falhou: %v", err)
	}

	colRef := findColumn(meta, "Ref")
	if colRef == nil {
		t.Fatal("Coluna 'Ref' não encontrada")
	}
	if !colRef.IsUnique {
		t.Error("Coluna 'Ref' deveria ser IsUnique=true")
	}
	if colRef.ColumnName != "ref_id" {
		t.Errorf("Esperado ColumnName 'ref_id', obteve '%s'", colRef.ColumnName)
	}
	if colRef.Size != 50 {
		t.Errorf("Esperado Size=50, obteve %d", colRef.Size)
	}

	colOpcional := findColumn(meta, "Opcional")
	if colOpcional == nil {
		t.Fatal("Coluna 'Opcional' não encontrada")
	}
	if !colOpcional.IsNullable {
		t.Error("Coluna 'Opcional' (*string) deveria ser IsNullable=true")
	}

	colObrigatorio := findColumn(meta, "Obrigatorio")
	if colObrigatorio == nil {
		t.Fatal("Coluna 'Obrigatorio' não encontrada")
	}
	if colObrigatorio.IsNullable {
		t.Error("Coluna 'Obrigatorio' (tag notnull) não deveria ser IsNullable=true")
	}

	colTamanhoMax := findColumn(meta, "TamanhoMax")
	if colTamanhoMax == nil {
		t.Fatal("Coluna 'TamanhoMax' não encontrada")
	}
	if colTamanhoMax.Size != 200 {
		t.Errorf("Esperado Size=200, obteve %d", colTamanhoMax.Size)
	}

	colPadraoStr := findColumn(meta, "PadraoStr")
	if colPadraoStr == nil {
		t.Fatal("Coluna 'PadraoStr' não encontrada")
	}
	if colPadraoStr.DefaultValue != "'PENDENTE'" {
		t.Errorf("Esperado DefaultValue \"'PENDENTE'\", obteve '%s'", colPadraoStr.DefaultValue)
	}

	colPadraoNum := findColumn(meta, "PadraoNum")
	if colPadraoNum == nil {
		t.Fatal("Coluna 'PadraoNum' não encontrada")
	}
	if colPadraoNum.DefaultValue != "1" {
		t.Errorf("Esperado DefaultValue '1', obteve '%s'", colPadraoNum.DefaultValue)
	}
}

func TestParse_Cache(t *testing.T) {
	metadata.ClearMetadataCache() // Garante cache limpo no início
	t.Cleanup(metadata.ClearMetadataCache)

	// Primeira chamada - deve fazer o parsing
	t.Log("Primeira chamada Parse(ModeloBasico{})...")
	start := time.Now()
	meta1, err1 := metadata.Parse(ModeloBasico{})
	duration1 := time.Since(start)
	if err1 != nil {
		t.Fatalf("Parse 1 falhou: %v", err1)
	}
	if meta1 == nil {
		t.Fatal("Parse 1 retornou nil meta")
	}

	// Segunda chamada - deve usar o cache (e ser mais rápida, embora não testemos tempo)
	t.Log("Segunda chamada Parse(ModeloBasico{})...")
	start = time.Now()
	meta2, err2 := metadata.Parse(ModeloBasico{})
	duration2 := time.Since(start)
	if err2 != nil {
		t.Fatalf("Parse 2 falhou: %v", err2)
	}
	if meta2 == nil {
		t.Fatal("Parse 2 retornou nil meta")
	}

	// Verifica se retornou o mesmo objeto (ponteiro) do cache
	if meta1 != meta2 {
		t.Errorf("Esperado o mesmo ponteiro de metadados nas chamadas 1 e 2 (cache), mas ponteiros diferem: %p != %p", meta1, meta2)
	}
	t.Logf("Duração Parse 1: %s, Duração Parse 2 (cache): %s", duration1, duration2)

	// Limpa o cache
	metadata.ClearMetadataCache()
	t.Log("Cache limpo via ClearMetadataCache()")

	// Terceira chamada - deve fazer o parsing novamente
	t.Log("Terceira chamada Parse(ModeloBasico{})...")
	meta3, err3 := metadata.Parse(ModeloBasico{})
	if err3 != nil {
		t.Fatalf("Parse 3 falhou: %v", err3)
	}
	if meta3 == nil {
		t.Fatal("Parse 3 retornou nil meta")
	}

	// Verifica se o objeto é diferente do anterior (pois o cache foi limpo)
	if meta1 == meta3 {
		t.Error("Esperado ponteiro diferente na chamada 3 após limpar cache, mas foi igual ao da chamada 1")
	}
}

func TestParse_InvalidInput(t *testing.T) {
	metadata.ClearMetadataCache()
	t.Cleanup(metadata.ClearMetadataCache)

	// Teste com nil
	_, err := metadata.Parse(nil)
	if err == nil {
		t.Error("Esperado erro ao passar nil para Parse, mas obteve nil")
	} else {
		t.Logf("Obteve erro esperado para input nil: %v", err)
	}

	// Teste com tipo não-struct (int)
	_, err = metadata.Parse(123)
	if err == nil {
		t.Error("Esperado erro ao passar int para Parse, mas obteve nil")
	} else if !strings.Contains(err.Error(), "deve ser uma struct") { // Verifica conteúdo do erro
		t.Errorf("Mensagem de erro inesperada para int: %v", err)
	} else {
		t.Logf("Obteve erro esperado para input int: %v", err)
	}

	// Teste com ponteiro para não-struct
	num := 456
	_, err = metadata.Parse(&num)
	if err == nil {
		t.Error("Esperado erro ao passar *int para Parse, mas obteve nil")
	} else if !strings.Contains(err.Error(), "deve ser uma struct") {
		t.Errorf("Mensagem de erro inesperada para *int: %v", err)
	} else {
		t.Logf("Obteve erro esperado para input *int: %v", err)
	}
}

func TestParse_TagErrors(t *testing.T) {
	metadata.ClearMetadataCache()
	t.Cleanup(metadata.ClearMetadataCache)

	_, err := metadata.Parse(ErroTag{})
	if err == nil {
		t.Error("Esperado erro ao parsear ErroTag (size:abc), mas obteve nil")
	} else if !strings.Contains(err.Error(), "erro ao parsear 'size'") { // Verifica se o erro é sobre 'size'
		t.Errorf("Erro inesperado ao parsear ErroTag: %v", err)
	} else {
		t.Logf("Obteve erro esperado ao parsear tag inválida: %v", err)
	}
}
