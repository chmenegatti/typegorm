// driver/sqlserver/sqlserver.go
package sqlserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url" // Para montar a URL de conexão
	"sync"
	"time"

	// Import anônimo para registrar o driver "sqlserver".
	_ "github.com/microsoft/go-mssqldb"
	// Importa o pacote raiz do TypeGorm.
	"github.com/chmenegatti/typegorm"
)

// Adiciona o DriverType para SQL Server (precisa ser adicionado em datasource.go também)
const SQLServer typegorm.DriverType = "sqlserver"

// Config define os parâmetros de conexão específicos para SQL Server.
type Config struct {
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"`         // Default 1433
	Username string `json:"username" yaml:"username"` // Opcional para Windows Auth
	Password string `json:"password" yaml:"password"` // Opcional para Windows Auth
	Database string `json:"database" yaml:"database"`
	// Parâmetros extras da connection string (ex: encrypt=disable, trustServerCertificate=true)
	Params map[string]string `json:"params" yaml:"params"`
}

// GetType implementa a interface typegorm.DriverTyper.
func (c Config) GetType() typegorm.DriverType {
	return SQLServer // Retorna o tipo específico
}

// --- Verificações em tempo de compilação ---
var _ typegorm.DataSource = (*SQLServerDataSource)(nil)
var _ typegorm.DriverTyper = Config{}

// SQLServerDataSource implementa a interface typegorm.DataSource para SQL Server.
type SQLServerDataSource struct {
	config Config
	db     *sql.DB
	connMu sync.RWMutex
}

// init registra este driver SQL Server no registro central do TypeGorm.
func init() {
	// Registra usando a constante local SQLServer
	typegorm.RegisterDriver(SQLServer, func() typegorm.DataSource {
		return &SQLServerDataSource{}
	})
}

// NewDataSource é uma fábrica simples para este driver.
func NewDataSource() *SQLServerDataSource {
	return &SQLServerDataSource{}
}

// Connect implementa typegorm.DataSource.Connect.
func (s *SQLServerDataSource) Connect(cfg typegorm.Config) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] sqlserver.Connect: Entrou na função, mutex adquirido.")

	if s.db != nil {
		fmt.Println("[LOG] sqlserver.Connect: Conexão já estabelecida.")
		return errors.New("sqlserver: conexão já estabelecida")
	}

	// Asserção de tipo.
	sqlServerConfig, ok := cfg.(Config)
	if !ok {
		if ptrCfg, okPtr := cfg.(*Config); okPtr && ptrCfg != nil {
			sqlServerConfig = *ptrCfg
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("sqlserver: tipo config inválido %T", cfg)
	}
	s.config = sqlServerConfig

	// Validação básica
	if s.config.Host == "" {
		return errors.New("sqlserver: Host é obrigatório")
	}
	port := s.config.Port
	if port == 0 {
		port = 1433
	} // Porta padrão SQL Server

	// Monta a Connection String URL: sqlserver://user:pass@host:port?database=db&param=value
	connURL := &url.URL{
		Scheme: "sqlserver",
		Host:   fmt.Sprintf("%s:%d", s.config.Host, port),
	}
	// Adiciona usuário e senha se fornecidos (necessário para SQL Authentication)
	if s.config.Username != "" {
		if s.config.Password != "" {
			connURL.User = url.UserPassword(s.config.Username, s.config.Password)
		} else {
			connURL.User = url.User(s.config.Username)
		}
	} // Se user/pass vazios, assume Windows Authentication (Integrated Security) no Windows

	// Adiciona parâmetros da query string
	query := url.Values{}
	if s.config.Database != "" {
		query.Set("database", s.config.Database)
	}
	// Parâmetros comuns para desenvolvimento/teste (ajuste para produção!)
	if _, ok := s.config.Params["encrypt"]; !ok {
		query.Set("encrypt", "disable") // Ou "false". Use "true" em produção.
	}
	// if _, ok := s.config.Params["trustServerCertificate"]; !ok {
	//     query.Set("trustServerCertificate", "true") // Cuidado em produção!
	// }
	for k, v := range s.config.Params {
		query.Set(k, v) // Adiciona/sobrescreve params da config
	}
	connURL.RawQuery = query.Encode()
	connStr := connURL.String()

	fmt.Printf("[LOG] sqlserver.Connect: Connection String Montada: %s\n", "[ConnString Omitida por Segurança]")
	// fmt.Printf("[LOG] sqlserver.Connect: Connection String (DEBUG): %s\n", connStr) // Log apenas localmente

	// Abre a conexão usando o driver "sqlserver".
	fmt.Println("[LOG] sqlserver.Connect: Chamando sql.Open()...")
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		fmt.Printf("[LOG] sqlserver.Connect: sql.Open() falhou: %v\n", err)
		return fmt.Errorf("sqlserver: falha ao preparar conexão: %w", err)
	}
	fmt.Println("[LOG] sqlserver.Connect: sql.Open() bem-sucedido.")

	// Configura pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	s.db = db
	fmt.Println("[LOG] sqlserver.Connect: *sql.DB atribuído.")

	// --- Verificação com Ping ---
	fmt.Println("[LOG] sqlserver.Connect: Chamando Ping() para verificação...")
	pingCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second) // Timeout maior pode ser necessário
	defer cancel()
	err = s.db.PingContext(pingCtx) // Ping direto no db interno (já temos o Lock)
	if err != nil {
		fmt.Printf("[LOG] sqlserver.Connect: Ping() inicial falhou: %v\n", err)
		s.db.Close()
		s.db = nil
		return fmt.Errorf("sqlserver: falha ao verificar conexão após abrir (%s:%d): %w", s.config.Host, port, err)
	}
	fmt.Println("[LOG] sqlserver.Connect: Ping() inicial bem-sucedido.")
	// --- Fim Ping ---

	fmt.Printf("[LOG] sqlserver.Connect: Conexão configurada com sucesso para %s:%d\n", s.config.Host, port)
	fmt.Println("[LOG] sqlserver.Connect: Saindo da função, mutex liberado.")
	return nil
}

// Close implementa typegorm.DataSource.Close.
func (s *SQLServerDataSource) Close() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] sqlserver.Close: Entrou.")
	if s.db == nil {
		return errors.New("sqlserver: conexão não estabelecida")
	}
	fmt.Println("[LOG] sqlserver.Close: Chamando s.db.Close()...")
	err := s.db.Close()
	s.db = nil
	if err != nil {
		fmt.Printf("[LOG] sqlserver.Close: Falha: %v\n", err)
		return fmt.Errorf("sqlserver: %w", err)
	}
	fmt.Println("[LOG] sqlserver.Close: Sucesso.")
	return nil
}

// Ping implementa typegorm.DataSource.Ping.
func (s *SQLServerDataSource) Ping(ctx context.Context) error {
	fmt.Println("[LOG] sqlserver.Ping: Entrou.")
	db, err := s.getDBInstance()
	if err != nil {
		return err
	}
	fmt.Println("[LOG] sqlserver.Ping: Chamando db.PingContext()...")
	err = db.PingContext(ctx)
	if err != nil {
		fmt.Printf("[LOG] sqlserver.Ping: Falha: %v\n", err)
		return fmt.Errorf("sqlserver: ping: %w", err)
	}
	fmt.Println("[LOG] sqlserver.Ping: Sucesso.")
	return nil
}

// GetDriverType implementa typegorm.DataSource.GetDriverType.
func (s *SQLServerDataSource) GetDriverType() typegorm.DriverType { return SQLServer }

// GetDB implementa typegorm.DataSource.GetDB.
func (s *SQLServerDataSource) GetDB() (*sql.DB, error) { return s.getDBInstance() }

// GetNativeConnection implementa typegorm.DataSource.GetNativeConnection.
func (s *SQLServerDataSource) GetNativeConnection() (any, error) { return s.getDBInstance() }

// --- Implementação Métodos Query/Tx/Prepare (Delegação) ---
func (s *SQLServerDataSource) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, q, args...)
}
func (s *SQLServerDataSource) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, q, args...)
}
func (s *SQLServerDataSource) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	db, err := s.getDBInstance()
	if err != nil {
		fmt.Printf("[WARN] sqlserver.QueryRow: %v\n", err)
		return nil
	}
	return db.QueryRowContext(ctx, q, args...)
}
func (s *SQLServerDataSource) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.BeginTx(ctx, opts)
}
func (s *SQLServerDataSource) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.PrepareContext(ctx, q)
}

// --- Função Auxiliar Interna ---
func (s *SQLServerDataSource) getDBInstance() (*sql.DB, error) {
	s.connMu.RLock()
	db := s.db
	s.connMu.RUnlock()
	if db == nil {
		return nil, errors.New("sqlserver: conexão não estabelecida")
	}
	return db, nil
}
