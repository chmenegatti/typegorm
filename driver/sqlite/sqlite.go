// driver/sqlite/sqlite.go
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	// Import anônimo para registrar o driver "sqlite3" no pacote database/sql.
	_ "github.com/mattn/go-sqlite3"
	// Importa o pacote raiz do TypeGorm.
	"github.com/chmenegatti/typegorm"
)

// Config define os parâmetros de conexão específicos para SQLite.
type Config struct {
	// Database é o caminho para o arquivo do banco de dados SQLite.
	Database string `json:"database" yaml:"database"`
	// Options permite passar parâmetros adicionais na DSN (ex: _journal=WAL).
	Options map[string]string `json:"options" yaml:"options"`
}

// GetType implementa a interface typegorm.DriverTyper.
// Retorna o tipo de driver para esta struct de configuração.
func (c Config) GetType() typegorm.DriverType {
	return typegorm.SQLite
}

// --- Verificações em tempo de compilação ---
// Garante que SQLiteDataSource implementa typegorm.DataSource.
var _ typegorm.DataSource = (*SQLiteDataSource)(nil)

// Garante que Config implementa typegorm.DriverTyper.
var _ typegorm.DriverTyper = Config{} // Verifica contra o tipo valor

// SQLiteDataSource implementa a interface typegorm.DataSource para SQLite.
type SQLiteDataSource struct {
	config Config
	db     *sql.DB
	connMu sync.RWMutex // Protege o acesso concorrente a `db`.
}

// init registra este driver SQLite no registro central do TypeGorm.
func init() {
	// Registra a função fábrica que cria novas instâncias de SQLiteDataSource.
	typegorm.RegisterDriver(typegorm.SQLite, func() typegorm.DataSource {
		// Esta função deve retornar uma instância zerada;
		// Connect será chamado nela posteriormente.
		return &SQLiteDataSource{}
	})
	// O log de registro agora é feito centralmente em typegorm.RegisterDriver
}

// NewDataSource é uma fábrica simples para este driver específico.
// Pode ser útil para uso direto ou testes.
func NewDataSource() *SQLiteDataSource {
	return &SQLiteDataSource{}
}

// Connect implementa typegorm.DataSource.Connect.
func (s *SQLiteDataSource) Connect(cfg typegorm.Config) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] sqlite.Connect: Entrou na função, mutex adquirido.")

	if s.db != nil {
		fmt.Println("[LOG] sqlite.Connect: Conexão já estabelecida.")
		return errors.New("sqlite: conexão já estabelecida")
	}

	// Asserção de tipo ainda é necessária aqui para obter os dados concretos da config.
	sqliteConfig, ok := cfg.(Config)
	if !ok {
		if ptrCfg, okPtr := cfg.(*Config); okPtr && ptrCfg != nil {
			sqliteConfig = *ptrCfg
			ok = true
		}
	}
	if !ok {
		fmt.Println("[LOG] sqlite.Connect: Tipo de configuração inválido passado para o método Connect.")
		// Este erro não deveria ocorrer se typegorm.Connect funcionou, mas é bom verificar.
		return fmt.Errorf("sqlite: tipo de configuração inválido %T passado para o método Connect", cfg)
	}
	s.config = sqliteConfig

	if s.config.Database == "" {
		fmt.Println("[LOG] sqlite.Connect: Caminho do banco de dados está vazio.")
		return errors.New("sqlite: caminho do banco de dados (Database) não pode ser vazio na config")
	}

	// Monta o Data Source Name (DSN) para SQLite.
	dsn := s.config.Database
	isFirstOpt := true
	if len(s.config.Options) > 0 {
		dsn += "?"
		for k, v := range s.config.Options {
			if !isFirstOpt {
				dsn += "&"
			}
			dsn += fmt.Sprintf("%s=%s", k, v)
			isFirstOpt = false
		}
	}
	fmt.Printf("[LOG] sqlite.Connect: DSN Montado: %s\n", dsn)

	// Abre a conexão (prepara o pool, não conecta imediatamente).
	fmt.Println("[LOG] sqlite.Connect: Chamando sql.Open()...")
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		fmt.Printf("[LOG] sqlite.Connect: sql.Open() falhou: %v\n", err)
		return fmt.Errorf("sqlite: falha ao preparar conexão: %w", err)
	}
	fmt.Println("[LOG] sqlite.Connect: sql.Open() bem-sucedido.")

	// Configura o pool de conexões (Importante para SQLite!).
	db.SetMaxOpenConns(1) // Geralmente 1 para evitar "database is locked".
	s.db = db
	fmt.Println("[LOG] sqlite.Connect: *sql.DB atribuído ao campo da struct.")

	// --- Verificação com Ping (Removida daqui para simplificar, pode ser feita externamente) ---
	// A verificação inicial é importante, mas pode ser feita após `typegorm.Connect` retornar.
	// Remover o Ping daqui simplifica a lógica do Connect interno.

	fmt.Printf("[LOG] sqlite.Connect: Conexão configurada com sucesso para %s\n", s.config.Database)
	fmt.Println("[LOG] sqlite.Connect: Saindo da função, mutex liberado.")
	return nil
}

// Close implementa typegorm.DataSource.Close.
func (s *SQLiteDataSource) Close() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] sqlite.Close: Entrou na função, mutex adquirido.")

	if s.db == nil {
		fmt.Println("[LOG] sqlite.Close: Conexão não estabelecida ou já fechada.")
		return errors.New("sqlite: conexão não estabelecida ou já fechada")
	}
	fmt.Println("[LOG] sqlite.Close: Chamando s.db.Close()...")
	err := s.db.Close()
	dbRef := s.db
	s.db = nil // Limpa a referência *após* tentar fechar.
	if err != nil {
		fmt.Printf("[LOG] sqlite.Close: s.db.Close() falhou: %v (db ref: %p)\n", err, dbRef)
		return fmt.Errorf("sqlite: erro ao fechar conexão: %w", err)
	}
	fmt.Printf("[LOG] sqlite.Close: s.db.Close() bem-sucedido. (db ref: %p)\n", dbRef)
	fmt.Println("[LOG] sqlite.Close: Saindo da função, mutex liberado.")
	return nil
}

// Ping implementa typegorm.DataSource.Ping.
func (s *SQLiteDataSource) Ping(ctx context.Context) error {
	fmt.Println("[LOG] sqlite.Ping: Entrou na função.")
	// Usa helper para obter DB e checar nil de forma segura
	db, err := s.getDBInstance()
	if err != nil {
		fmt.Printf("[LOG] sqlite.Ping: Erro ao obter instância do DB: %v\n", err)
		return err
	}
	fmt.Printf("[LOG] sqlite.Ping: Instância do DB obtida: %p\n", db)

	fmt.Println("[LOG] sqlite.Ping: Chamando db.PingContext()...")
	err = db.PingContext(ctx) // Reatribui err
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Println("[LOG] sqlite.Ping: db.PingContext() falhou: context deadline exceeded.")
		} else {
			fmt.Printf("[LOG] sqlite.Ping: db.PingContext() falhou: %v\n", err)
		}
		return err // Retorna o erro original
	}
	fmt.Println("[LOG] sqlite.Ping: db.PingContext() bem-sucedido.")
	return nil
}

// GetDriverType implementa typegorm.DataSource.GetDriverType.
func (s *SQLiteDataSource) GetDriverType() typegorm.DriverType {
	return typegorm.SQLite
}

// GetDB implementa typegorm.DataSource.GetDB.
func (s *SQLiteDataSource) GetDB() (*sql.DB, error) {
	// Reutiliza o helper para consistência e segurança de concorrência
	return s.getDBInstance()
}

// GetNativeConnection implementa typegorm.DataSource.GetNativeConnection.
func (s *SQLiteDataSource) GetNativeConnection() (interface{}, error) {
	// Para SQLite via database/sql, a conexão "nativa" mais relevante é o *sql.DB.
	return s.getDBInstance()
}

// --- Implementação dos Novos Métodos da DataSource ---

// ExecContext executa uma query sem retornar linhas.
func (s *SQLiteDataSource) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	db, err := s.getDBInstance() // Usa helper para obter DB e checar nil
	if err != nil {
		return nil, err // Retorna erro se conexão não estabelecida
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.ExecContext(ctx, query, args...)
}

// QueryContext executa uma query que retorna linhas.
func (s *SQLiteDataSource) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.QueryContext(ctx, query, args...)
}

// QueryRowContext executa uma query que deve retornar no máximo uma linha.
func (s *SQLiteDataSource) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db, err := s.getDBInstance()
	if err != nil {
		// Como QueryRowContext não retorna erro diretamente, a melhor abordagem
		// é talvez retornar nil ou deixar o panic ocorrer se db for nil, indicando erro de programação.
		// Optamos por retornar nil para evitar panic, mas o usuário deve idealmente verificar a conexão antes.
		// TODO: Considerar uma forma de retornar um *sql.Row que contenha o erro interno.
		fmt.Printf("[WARN] sqlite.QueryRowContext: Chamado quando conexão não está estabelecida (getDBInstance error: %v), retornando nil *sql.Row\n", err)
		return nil
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.QueryRowContext(ctx, query, args...)
}

// BeginTx inicia uma transação.
func (s *SQLiteDataSource) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.BeginTx(ctx, opts)
}

// PrepareContext cria um statement preparado.
func (s *SQLiteDataSource) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.PrepareContext(ctx, query)
}

// --- Função Auxiliar Interna ---

// getDBInstance é uma função auxiliar interna para obter seguramente a instância *sql.DB
// e verificar se a conexão está estabelecida. Usa RLock para leitura.
func (s *SQLiteDataSource) getDBInstance() (*sql.DB, error) {
	s.connMu.RLock() // Bloqueio de leitura para acessar s.db
	db := s.db
	s.connMu.RUnlock()

	if db == nil {
		return nil, errors.New("sqlite: conexão não estabelecida")
	}
	return db, nil
}
