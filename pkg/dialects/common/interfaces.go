// pkg/dialects/common/interfaces.go
package common

import (
	"context"             // Usar context para timeouts, cancelamento, etc.
	"database/sql/driver" // Reutilizar interfaces de valor quando possível
	"io"
	"time"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/schema" // Importar o placeholder
)

// MigrationRecord represents a migration entry in the database.
// Useful for returning structured data from GetAppliedMigrationsSQL results.
type MigrationRecord struct {
	ID        string
	AppliedAt time.Time
}

// Dialect define as características e sintaxe específicas de um SGBD.
// Esta interface foca nas diferenças de sintaxe e tipos.
type Dialect interface {
	// Name retorna o nome único do dialeto (ex: "mysql", "sqlite", "mongodb").
	Name() string

	// Quote envolve um identificador (tabela, coluna) com as aspas corretas.
	// Ex: `ident` (MySQL), "ident" (Postgres/SQLServer), ou sem aspas.
	Quote(identifier string) string

	// BindVar retorna o placeholder para parâmetros em queries preparadas.
	// Ex: "?" (MySQL/SQLite), "$1", "$2" (Postgres). O índice é base 1.
	BindVar(i int) string

	// GetDataType mapeia um tipo Go (com metadados do schema.Field) para
	// uma string de tipo de dados do banco de dados.
	// Ex: field{GoType: string, Size: 255} -> "VARCHAR(255)" (MySQL)
	// Ex: field{GoType: int, IsPrimary: true} -> "INTEGER PRIMARY KEY AUTOINCREMENT" (SQLite)
	GetDataType(field *schema.Field) (string, error)

	// HasIndex verifica se um índice com o nome especificado existe.
	// (Pode ser movido para uma interface de SchemaManager depois)
	// HasIndex(conn Connection, tableName string, indexName string) (bool, error)

	// Outras especificidades podem ser adicionadas:
	// - Tratamento de cláusulas específicas (LIMIT/OFFSET, RETURNING).
	// - Verificação de capacidades (suporte a JSON, CTEs, etc.).
	// - Geração de SQL para operações DDL (CREATE TABLE, ADD COLUMN).

	// CreateSchemaMigrationsTableSQL returns the SQL statement to create the schema migrations table
	// if it doesn't exist. The table should store migration IDs and timestamps.
	CreateSchemaMigrationsTableSQL(tableName string) string

	// GetAppliedMigrationsSQL returns the SQL statement to retrieve the IDs and applied timestamps
	// of all applied migrations, ordered consistently (e.g., by ID ASC).
	GetAppliedMigrationsSQL(tableName string) string

	// InsertMigrationSQL returns the SQL statement to insert a migration record (ID, AppliedAt).
	// It should use the correct BindVar placeholders.
	InsertMigrationSQL(tableName string) string

	// DeleteMigrationSQL returns the SQL statement to delete a migration record by its ID.
	// It should use the correct BindVar placeholder.
	DeleteMigrationSQL(tableName string) string
}

// DataSource representa a fonte de dados configurada, gerenciando conexões.
// É a interface principal para interagir com o banco. Análogo a `sql.DB`.
type DataSource interface {
	io.Closer // Para ds.Close()

	// Connect inicializa a conexão/pool com base na configuração.
	// Geralmente chamado internamente por typegorm.Open().
	Connect(cfg config.DatabaseConfig) error

	// Ping verifica a conectividade com o banco.
	Ping(ctx context.Context) error

	// BeginTx inicia uma nova transação.
	BeginTx(ctx context.Context, opts any) (Tx, error) // opts pode ser sql.TxOptions ou similar

	// Exec executa uma query que não retorna linhas (INSERT, UPDATE, DELETE).
	Exec(ctx context.Context, query string, args ...any) (Result, error)

	// QueryRow executa uma query que retorna no máximo uma linha.
	QueryRow(ctx context.Context, query string, args ...any) RowScanner

	// Query executa uma query que retorna múltiplas linhas.
	Query(ctx context.Context, query string, args ...any) (Rows, error)

	// Dialect retorna o dialeto associado a esta fonte de dados.
	Dialect() Dialect
}

// Tx representa uma transação de banco de dados ativa. Análogo a `sql.Tx`.
type Tx interface {
	// Commit confirma a transação.
	Commit() error

	// Rollback descarta a transação.
	Rollback() error

	// Exec executa uma query dentro da transação.
	Exec(ctx context.Context, query string, args ...any) (Result, error)

	// QueryRow executa uma query de uma linha dentro da transação.
	QueryRow(ctx context.Context, query string, args ...any) RowScanner

	// Query executa uma query de múltiplas linhas dentro da transação.
	Query(ctx context.Context, query string, args ...any) (Rows, error)
}

// Result representa o resultado de uma operação Exec. Análogo a `sql.Result`.
type Result interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

// Rows é um iterador sobre o resultado de uma query. Análogo a `sql.Rows`.
type Rows interface {
	io.Closer // Para rows.Close()

	Next() bool
	Scan(dest ...any) error // Mapeia colunas para os ponteiros em dest
	Columns() ([]string, error)
	Err() error
}

// RowScanner permite escanear o resultado de uma query de linha única. Análogo a `sql.Row`.
type RowScanner interface {
	Scan(dest ...any) error
}

// SQLValuer é uma interface para tipos que podem se converter em valores SQL.
// Reutiliza a interface padrão do Go. Útil para tipos customizados.
type SQLValuer interface {
	driver.Valuer
}
