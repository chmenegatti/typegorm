// driver/mysql/mysql.go
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings" // Para montar query params
	"sync"
	"time"

	// Import anônimo para registrar o driver "mysql".
	_ "github.com/go-sql-driver/mysql"
	// Importa o pacote raiz do TypeGorm.
	"github.com/chmenegatti/typegorm"
)

// Config define os parâmetros de conexão específicos para MySQL/MariaDB.
type Config struct {
	Host     string `json:"host" yaml:"host"`
	Port     int    `json:"port" yaml:"port"` // Default 3306
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	Database string `json:"database" yaml:"database"`
	// Parâmetros extras da DSN (ex: charset=utf8mb4, collation=utf8mb4_unicode_ci)
	// IMPORTANTE: parseTime=true é essencial para mapear colunas DATETIME/TIMESTAMP para time.Time
	Params map[string]string `json:"params" yaml:"params"`
}

// GetType implementa a interface typegorm.DriverTyper.
func (c Config) GetType() typegorm.DriverType {
	// Usamos MySQL como tipo genérico para ambos
	return typegorm.MySQL
}

// --- Verificações em tempo de compilação ---
var _ typegorm.DataSource = (*MySQLDataSource)(nil)
var _ typegorm.DriverTyper = Config{}

// MySQLDataSource implementa a interface typegorm.DataSource para MySQL/MariaDB.
type MySQLDataSource struct {
	config Config
	db     *sql.DB
	connMu sync.RWMutex
}

// init registra este driver MySQL/MariaDB no registro central do TypeGorm.
func init() {
	// Usamos MySQL como o tipo registrado
	typegorm.RegisterDriver(typegorm.MySQL, func() typegorm.DataSource {
		return &MySQLDataSource{}
	})
}

// NewDataSource é uma fábrica simples para este driver.
func NewDataSource() *MySQLDataSource {
	return &MySQLDataSource{}
}

// Connect implementa typegorm.DataSource.Connect.
func (s *MySQLDataSource) Connect(cfg typegorm.Config) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] mysql.Connect: Entrou na função, mutex adquirido.")

	if s.db != nil {
		fmt.Println("[LOG] mysql.Connect: Conexão já estabelecida.")
		return errors.New("mysql: conexão já estabelecida")
	}

	// Asserção de tipo para obter a config concreta.
	mysqlConfig, ok := cfg.(Config)
	if !ok {
		if ptrCfg, okPtr := cfg.(*Config); okPtr && ptrCfg != nil {
			mysqlConfig = *ptrCfg
			ok = true
		}
	}
	if !ok {
		fmt.Println("[LOG] mysql.Connect: Tipo de configuração inválido passado.")
		return fmt.Errorf("mysql: tipo de configuração inválido %T passado para o método Connect", cfg)
	}
	s.config = mysqlConfig // Armazena a config

	// Validação básica
	if s.config.Username == "" || s.config.Database == "" {
		// Host pode ser default (localhost), Port pode ser default (3306)
		return errors.New("mysql: Username e Database são obrigatórios na configuração")
	}
	host := s.config.Host
	if host == "" {
		host = "localhost"
	}
	port := s.config.Port
	if port == 0 {
		port = 3306
	}

	// Monta a DSN (Data Source Name) para MySQL: user:password@tcp(host:port)/dbname?param=value
	// Ex: "root:password@tcp(127.0.0.1:3306)/my_db?parseTime=true"
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		s.config.Username,
		s.config.Password, // Senha pode ser vazia
		host,
		port,
		s.config.Database,
	)

	// Adiciona parâmetros, garantindo parseTime=true
	params := s.config.Params
	if params == nil {
		params = make(map[string]string)
	}
	params["parseTime"] = "true" // Essencial para time.Time
	if _, hasCharset := params["charset"]; !hasCharset {
		params["charset"] = "utf8mb4" // Bom default para suportar emojis etc.
	}

	if len(params) > 0 {
		dsn += "?"
		paramParts := []string{}
		for k, v := range params {
			paramParts = append(paramParts, fmt.Sprintf("%s=%s", k, v)) // Assume que k, v não precisam de escape extra aqui
		}
		dsn += strings.Join(paramParts, "&")
	}

	fmt.Printf("[LOG] mysql.Connect: DSN Montado: %s\n", "[DSN OMITIDO POR SEGURANÇA - VERIFIQUE LOCALMENTE]") // Não logar DSN com senha!
	// fmt.Printf("[LOG] mysql.Connect: DSN Montado (DEBUG): %s\n", dsn) // Descomentar SÓ para debug local

	// Abre a conexão usando o driver "mysql".
	fmt.Println("[LOG] mysql.Connect: Chamando sql.Open()...")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Printf("[LOG] mysql.Connect: sql.Open() falhou: %v\n", err)
		return fmt.Errorf("mysql: falha ao preparar conexão: %w", err)
	}
	fmt.Println("[LOG] mysql.Connect: sql.Open() bem-sucedido.")

	// Configura o pool de conexões (ajustar conforme necessário)
	db.SetMaxOpenConns(50) // MySQL geralmente lida bem com mais conexões
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	s.db = db
	fmt.Println("[LOG] mysql.Connect: *sql.DB atribuído ao campo da struct.")

	// --- Verificação com Ping ---
	fmt.Println("[LOG] mysql.Connect: Chamando Ping() para verificação...")
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Chama diretamente no db, pois já temos o lock de escrita
	err = s.db.PingContext(pingCtx)
	if err != nil {
		fmt.Printf("[LOG] mysql.Connect: Ping() inicial falhou: %v\n", err)
		s.db.Close() // Garante limpeza
		s.db = nil
		return fmt.Errorf("mysql: falha ao verificar conexão após abrir (%s:%d): %w", host, port, err)
	}
	fmt.Println("[LOG] mysql.Connect: Ping() inicial bem-sucedido.")
	// --- Fim da Verificação com Ping ---

	fmt.Printf("[LOG] mysql.Connect: Conexão configurada com sucesso para %s:%d/%s\n", host, port, s.config.Database)
	fmt.Println("[LOG] mysql.Connect: Saindo da função, mutex liberado.")
	return nil
}

// Close implementa typegorm.DataSource.Close.
func (s *MySQLDataSource) Close() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] mysql.Close: Entrou na função, mutex adquirido.")
	if s.db == nil {
		fmt.Println("[LOG] mysql.Close: Conexão não estabelecida ou já fechada.")
		return errors.New("mysql: conexão não estabelecida ou já fechada")
	}
	fmt.Println("[LOG] mysql.Close: Chamando s.db.Close()...")
	err := s.db.Close()
	s.db = nil
	if err != nil {
		fmt.Printf("[LOG] mysql.Close: s.db.Close() falhou: %v\n", err)
		return fmt.Errorf("mysql: erro ao fechar conexão: %w", err)
	}
	fmt.Println("[LOG] mysql.Close: s.db.Close() bem-sucedido.")
	fmt.Println("[LOG] mysql.Close: Saindo da função, mutex liberado.")
	return nil
}

// Ping implementa typegorm.DataSource.Ping.
func (s *MySQLDataSource) Ping(ctx context.Context) error {
	fmt.Println("[LOG] mysql.Ping: Entrou na função.")
	db, err := s.getDBInstance() // Usa helper
	if err != nil {
		fmt.Printf("[LOG] mysql.Ping: Erro ao obter instância do DB: %v\n", err)
		return err
	}
	fmt.Printf("[LOG] mysql.Ping: Instância do DB obtida: %p\n", db)

	fmt.Println("[LOG] mysql.Ping: Chamando db.PingContext()...")
	err = db.PingContext(ctx)
	if err != nil {
		fmt.Printf("[LOG] mysql.Ping: db.PingContext() falhou: %v\n", err)
		return err // Retorna o erro original
	}
	fmt.Println("[LOG] mysql.Ping: db.PingContext() bem-sucedido.")
	return nil
}

// GetDriverType implementa typegorm.DataSource.GetDriverType.
func (s *MySQLDataSource) GetDriverType() typegorm.DriverType {
	return typegorm.MySQL // Retorna o tipo genérico MySQL
}

// GetDB implementa typegorm.DataSource.GetDB.
func (s *MySQLDataSource) GetDB() (*sql.DB, error) {
	return s.getDBInstance()
}

// GetNativeConnection implementa typegorm.DataSource.GetNativeConnection.
func (s *MySQLDataSource) GetNativeConnection() (interface{}, error) {
	// A interface principal para este driver é *sql.DB.
	return s.getDBInstance()
}

// --- Implementação dos Métodos de Query/Tx/Prepare (Delegação Simples) ---

func (s *MySQLDataSource) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, query, args...)
}
func (s *MySQLDataSource) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, query, args...)
}
func (s *MySQLDataSource) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db, err := s.getDBInstance()
	if err != nil {
		fmt.Printf("[WARN] mysql.QueryRowContext: %v\n", err)
		return nil
	}
	return db.QueryRowContext(ctx, query, args...)
}
func (s *MySQLDataSource) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.BeginTx(ctx, opts)
}
func (s *MySQLDataSource) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	db, err := s.getDBInstance()
	if err != nil {
		return nil, err
	}
	return db.PrepareContext(ctx, query)
}

// --- Função Auxiliar Interna ---
func (s *MySQLDataSource) getDBInstance() (*sql.DB, error) {
	s.connMu.RLock()
	db := s.db
	s.connMu.RUnlock()
	if db == nil {
		return nil, errors.New("mysql: conexão não estabelecida")
	}
	return db, nil
}
