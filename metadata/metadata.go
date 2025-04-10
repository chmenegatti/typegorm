// metadata/metadata.go
package metadata

import (
	"database/sql"
	"reflect"
	"time"
)

// EntityMetadata armazena metadados sobre uma entidade (struct Go mapeada).
type EntityMetadata struct {
	Name              string                     // Nome da struct Go (ex: "Usuario")
	TableName         string                     // Nome da tabela/coleção no banco (ex: "usuarios")
	Type              reflect.Type               // O reflect.Type da struct original
	Columns           []*ColumnMetadata          // Lista de metadados de todas as colunas mapeadas
	ColumnsByName     map[string]*ColumnMetadata // Mapa [NomeDoCampoGo] -> *ColumnMetadata para acesso rápido
	ColumnsByDBName   map[string]*ColumnMetadata // Mapa [NomeDaColunaDB] -> *ColumnMetadata para acesso rápido
	PrimaryKeyColumns []*ColumnMetadata          // Coluna(s) que compõem a chave primária

	// Ponteiros para colunas especiais (se existirem)
	CreatedAtColumn *ColumnMetadata
	UpdatedAtColumn *ColumnMetadata
	DeletedAtColumn *ColumnMetadata

	// TODO: Informações sobre Índices, Relações, etc.
}

// ColumnMetadata armazena metadados sobre um campo de struct mapeado para uma coluna.
type ColumnMetadata struct {
	Entity *EntityMetadata // Referência de volta para a entidade pai

	FieldName  string       // Nome do campo na struct Go (ex: "NomeCompleto")
	FieldType  reflect.Type // O reflect.Type do campo Go
	FieldIndex int          // Índice do campo na struct (útil para reflect.Value.Field)
	GoType     string       // Representação string do tipo Go (ex: "string", "int", "time.Time")

	ColumnName      string // Nome da coluna no banco (ex: "nome_completo")
	DBType          string // Tipo explícito definido no banco (ex: "VARCHAR(100)", "TEXT", "JSONB")
	IsPrimaryKey    bool   // É parte da chave primária?
	IsAutoIncrement bool   // O valor é gerado/incrementado pelo banco?
	IsNullable      bool   // A coluna permite valores NULL? (Inferido/Definido)
	IsUnique        bool   // A coluna tem uma restrição UNIQUE?
	DefaultValue    string // Valor DEFAULT definido no banco (como string)
	Size            int    // Tamanho (para VARCHAR, etc.)
	Precision       int    // Precisão (para DECIMAL/NUMERIC)
	Scale           int    // Escala (para DECIMAL/NUMERIC)

	// Flags para colunas especiais
	IsCreatedAt bool // É a coluna de timestamp de criação?
	IsUpdatedAt bool // É a coluna de timestamp de atualização?
	IsDeletedAt bool // É a coluna de timestamp para soft delete?

	// TODO: Informações sobre Índices específicos da coluna, Relações
	IndexName       string // Nome do índice simples nesta coluna (se houver)
	UniqueIndexName string // Nome do índice único nesta coluna (se houver)
}

// --- Tipos Go comuns para referência ---
var (
	stringType      = reflect.TypeOf("")
	intType         = reflect.TypeOf(int(0))
	int8Type        = reflect.TypeOf(int8(0))
	int16Type       = reflect.TypeOf(int16(0))
	int32Type       = reflect.TypeOf(int32(0))
	int64Type       = reflect.TypeOf(int64(0))
	uintType        = reflect.TypeOf(uint(0))
	uint8Type       = reflect.TypeOf(uint8(0))
	uint16Type      = reflect.TypeOf(uint16(0))
	uint32Type      = reflect.TypeOf(uint32(0))
	uint64Type      = reflect.TypeOf(uint64(0))
	float32Type     = reflect.TypeOf(float32(0))
	float64Type     = reflect.TypeOf(float64(0))
	boolType        = reflect.TypeOf(false)
	timeType        = reflect.TypeOf(time.Time{})
	bytesType       = reflect.TypeOf([]byte{})
	nullStringType  = reflect.TypeOf(sql.NullString{})
	nullInt64Type   = reflect.TypeOf(sql.NullInt64{})
	nullFloat64Type = reflect.TypeOf(sql.NullFloat64{})
	nullBoolType    = reflect.TypeOf(sql.NullBool{})
	nullTimeType    = reflect.TypeOf(sql.NullTime{})
)
