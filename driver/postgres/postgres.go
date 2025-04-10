package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/chmenegatti/typegorm"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Config struct {
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"` // Usar int para porta
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	Database string `json:"database" yaml:"database"`
	SSLMode  string `json:"sslmode" yaml:"sslmode"` // Ex: "disable", "require", "verify-full"
	// Adicionar outros parâmetros se necessário (ex: Timezone, ConnectTimeout)
	Params  map[string]string `json:"params" yaml:"params"`   // Parâmetros extras da query string
	Options map[string]string `json:"options" yaml:"options"` // Opções adicionais para a conexão
}

// GetType implementa a interface typegorm.DriverTyper.
// GetType implementa a interface typegorm.DriverTyper.
func (c Config) GetType() typegorm.DriverType {
	return typegorm.Postgres
}

// --- Verificações em tempo de compilação ---
// Garante que PostgresDataSource implementa typegorm.DataSource.
var _ typegorm.DataSource = (*PostgresDataSource)(nil)

// Garante que Config implementa typegorm.DriverTyper.
var _ typegorm.DriverTyper = Config{} // Verifica contra o tipo valor

// PostgresDataSource implementa a interface typegorm.DataSource para PostgreSQL.
type PostgresDataSource struct {
	config Config
	db     *sql.DB
	connMu sync.RWMutex // Protege o acesso concorrente a `db`.
}

// init registra este driver PostgreSQL no registro central do TypeGorm.
func init() {
	// Registra a função fábrica que cria novas instâncias de PostgresDataSource.
	typegorm.RegisterDriver(typegorm.Postgres, func() typegorm.DataSource {
		// Esta função deve retornar uma instância zerada;
		// Connect será chamado nela posteriormente.
		return &PostgresDataSource{}
	})
	// O log de registro agora é feito centralmente em typegorm.RegisterDriver
}

// NewDataSource é uma fábrica simples para este driver específico.
// Pode ser útil para uso direto ou testes.
func NewDataSource() *PostgresDataSource {
	return &PostgresDataSource{}
}

// Connect estabelece uma conexão com o banco de dados PostgreSQL usando a configuração fornecida.
// Retorna um erro se a conexão falhar.
// Connect implementa typegorm.DataSource.Connect.
func (s *PostgresDataSource) Connect(cfg typegorm.Config) error {
	s.connMu.Lock()         // Adquire Lock (escrita)
	defer s.connMu.Unlock() // Garante liberação no retorno
	fmt.Println("[LOG] postgres.Connect: Entrou na função, mutex adquirido.")

	if s.db != nil {
		fmt.Println("[LOG] postgres.Connect: Conexão já estabelecida.")
		return errors.New("postgres: conexão já estabelecida")
	}

	// Asserção de tipo para obter a config concreta.
	pgConfig, ok := cfg.(Config)
	// ... (lógica de type assertion continua igual) ...
	if !ok {
		fmt.Println("[LOG] postgres.Connect: Tipo de configuração inválido passado.")
		return fmt.Errorf("postgres: tipo de configuração inválido %T passado para o método Connect", cfg)
	}
	s.config = pgConfig

	// Validação da config
	if s.config.Host == "" || s.config.Port == 0 || s.config.Username == "" || s.config.Database == "" {
		return errors.New("postgres: Host, Port, Username, e Database são obrigatórios na configuração")
	}

	// Monta a DSN (Data Source Name)
	dsn := url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(s.config.Username, s.config.Password),
		Host:   fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Path:   s.config.Database,
	}
	query := dsn.Query()
	if s.config.SSLMode != "" {
		query.Set("sslmode", s.config.SSLMode)
	} else {
		query.Set("sslmode", "disable")
	}
	for k, v := range s.config.Params {
		query.Set(k, v)
	}
	dsn.RawQuery = query.Encode()
	dsnStr := dsn.String()
	fmt.Printf("[LOG] postgres.Connect: DSN Montado: %s\n", dsnStr)

	// Abre a conexão usando o driver "pgx".
	fmt.Println("[LOG] postgres.Connect: Chamando sql.Open()...")
	db, err := sql.Open("pgx", dsnStr)
	if err != nil {
		fmt.Printf("[LOG] postgres.Connect: sql.Open() falhou: %v\n", err)
		return fmt.Errorf("postgres: falha ao preparar conexão: %w", err)
	}
	fmt.Println("[LOG] postgres.Connect: sql.Open() bem-sucedido.")

	// Configura o pool de conexões
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// Atribui à struct ANTES de tentar o Ping interno
	s.db = db
	fmt.Println("[LOG] postgres.Connect: *sql.DB atribuído ao campo da struct.")

	// --- Verificação com Ping Interno (CORRIGIDO) ---
	// Acesso direto a s.db aqui é seguro, pois já temos o Lock de escrita.
	// NÃO chamar s.Ping() ou s.getDBInstance() daqui de dentro.
	if s.db == nil {
		// Checagem extra (improvável, mas segura)
		fmt.Println("[LOG] postgres.Connect: s.db é nil após sql.Open, não pode pingar.")
		return errors.New("postgres: falha interna, db é nil após sql.Open bem-sucedido")
	}

	fmt.Println("[LOG] postgres.Connect: Chamando db.PingContext() interno para verificação...")
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel() // Cancela o contexto do ping ao sair de Connect

	err = s.db.PingContext(pingCtx) // <--- CHAMA DIRETAMENTE NO s.db
	if err != nil {
		fmt.Printf("[LOG] postgres.Connect: Ping() interno falhou: %v\n", err)
		// Erro ocorreu após atribuir s.db, mas antes de retornar sucesso.
		// O objeto dataSource não será retornado pela fábrica Connect.
		// O defer Unlock vai rodar. Considerar limpar s.db?
		// s.db = nil // Opcional: limpar em caso de falha no ping inicial?
		return fmt.Errorf("postgres: falha ao verificar conexão após abrir (%s:%d): %w", s.config.Host, s.config.Port, err)
	}
	fmt.Println("[LOG] postgres.Connect: Ping() interno bem-sucedido.")
	// --- Fim da Verificação com Ping Corrigida ---

	fmt.Printf("[LOG] postgres.Connect: Conexão configurada com sucesso para %s:%d/%s\n", s.config.Host, s.config.Port, s.config.Database)
	fmt.Println("[LOG] postgres.Connect: Saindo da função, mutex liberado.")
	return nil
}

// Close implementa typegorm.DataSource.Close.
func (s *PostgresDataSource) Close() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] postgres.Close: Entrou na função, mutex adquirido.")
	if s.db == nil {
		fmt.Println("[LOG] postgres.Close: Conexão não estabelecida ou já fechada.")
		return errors.New("postgres: conexão não estabelecida ou já fechada")
	}
	fmt.Println("[LOG] postgres.Close: Chamando s.db.Close()...")
	err := s.db.Close()
	s.db = nil
	if err != nil {
		fmt.Printf("[LOG] postgres.Close: s.db.Close() falhou: %v\n", err)
		return fmt.Errorf("postgres: erro ao fechar conexão: %w", err)
	}
	fmt.Println("[LOG] postgres.Close: s.db.Close() bem-sucedido.")
	fmt.Println("[LOG] postgres.Close: Saindo da função, mutex liberado.")
	return nil
}

// Ping implementa typegorm.DataSource.Ping.
func (s *PostgresDataSource) Ping(ctx context.Context) error {
	fmt.Println("[LOG] postgres.Ping: Entrou na função.")
	db, err := s.getDBInstance() // Usa helper
	if err != nil {
		fmt.Printf("[LOG] postgres.Ping: Erro ao obter instância do DB: %v\n", err)
		return err
	}
	fmt.Printf("[LOG] postgres.Ping: Instância do DB obtida: %p\n", db)

	fmt.Println("[LOG] postgres.Ping: Chamando db.PingContext()...")
	err = db.PingContext(ctx)
	if err != nil {
		// Log detalhado do erro de ping
		fmt.Printf("[LOG] postgres.Ping: db.PingContext() falhou: %v\n", err)
		return err // Retorna o erro original
	}
	fmt.Println("[LOG] postgres.Ping: db.PingContext() bem-sucedido.")
	return nil
}

// GetDriverType retorna o tipo do driver como string.
func (s *PostgresDataSource) GetDriverType() typegorm.DriverType {
	return typegorm.Postgres
}

// GetDB retorna a instância do banco de dados SQL.
func (s *PostgresDataSource) GetDB() (*sql.DB, error) {
	return s.getDBInstance()
}

// GetNativeConnection retorna a conexão nativa do driver subjacente.
func (s *PostgresDataSource) GetNativeConnection() (any, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db, nil // Retorna a conexão nativa do driver subjacente
}

// --- Implementação dos Métodos de Query/Tx/Prepare (Delegação) ---

// ExecContext executa uma instrução SQL sem retorno de resultado.
func (s *PostgresDataSource) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	db, err := s.getDBInstance() // Usa helper
	if err != nil {
		return nil, err // Retorna erro se conexão não estabelecida
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.ExecContext(ctx, query, args...)
}

// QueryContext executa uma consulta SQL que retorna múltiplas linhas.
func (s *PostgresDataSource) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db, err := s.getDBInstance() // Usa helper
	if err != nil {
		return nil, err // Retorna erro se conexão não estabelecida
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.QueryContext(ctx, query, args...)
}

// QueryRowContext executa uma consulta SQL que retorna no máximo uma linha.
func (s *PostgresDataSource) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db, err := s.getDBInstance()
	if err != nil {
		fmt.Printf("[WARN] postgres.QueryRowContext: Chamado quando conexão não está estabelecida (getDBInstance error: %v)\n", err)
		// Retornar nil é uma opção, mas pode esconder o erro. Deixar o *sql.DB fazer o seu trabalho
		// (que pode retornar um *sql.Row que guarda o erro) é geralmente melhor.
		// Se db for nil aqui, a chamada db.QueryRowContext abaixo causaria panic.
		// A checagem em getDBInstance previne isso retornando erro antes.
		// TODO: Retornar um *sql.Row que contenha o erro, se possível. Por agora, nil.
		return nil
	}
	return db.QueryRowContext(ctx, query, args...)
}

// BeginTx inicia uma transação nativa do banco de dados.
func (s *PostgresDataSource) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	db, err := s.getDBInstance() // Usa helper
	if err != nil {
		return nil, err // Retorna erro se conexão não estabelecida
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.BeginTx(ctx, opts)
}

// PrepareContext cria um statement preparado para reutilização.
func (s *PostgresDataSource) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	db, err := s.getDBInstance() // Usa helper
	if err != nil {
		return nil, err // Retorna erro se conexão não estabelecida
	}
	// Delega diretamente ao método do *sql.DB subjacente
	return db.PrepareContext(ctx, query)
}

// --- Função Auxiliar Interna ---
func (s *PostgresDataSource) getDBInstance() (*sql.DB, error) {
	s.connMu.RLock()
	db := s.db
	s.connMu.RUnlock()
	if db == nil {
		return nil, errors.New("postgres: conexão não estabelecida")
	}
	return db, nil
}
