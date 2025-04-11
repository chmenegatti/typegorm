// datasource.go
package typegorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

// DriverType representa o tipo do driver do banco de dados.
type DriverType string

// Constantes para os tipos de driver suportados.
const (
	MySQL     DriverType = "mysql"
	Postgres  DriverType = "postgres"
	SQLite    DriverType = "sqlite"
	Oracle    DriverType = "oracle"
	Mongo     DriverType = "mongo"
	Redis     DriverType = "redis"
	SQLServer DriverType = "sqlserver"
	// Adicionar outros tipos conforme necessário
)

// Config é uma interface vazia (marcador) para representar structs de configuração
// específicas de cada driver.
type Config interface{}

// DriverTyper é implementada por structs de configuração para declarar
// para qual driver de banco de dados elas se destinam.
type DriverTyper interface {
	GetType() DriverType
}

// DataSource representa uma conexão ativa e configurada com um banco de dados.
// Esta é a interface principal que o resto do TypeGorm usará para
// interagir com o banco de dados, abstraindo detalhes específicos do driver.
type DataSource interface {
	// Gerenciamento da Conexão
	Connect(cfg Config) error       // Estabelece a conexão real usando a config.
	Close() error                   // Encerra a conexão com o banco de dados.
	Ping(ctx context.Context) error // Verifica se a conexão está ativa.
	GetDriverType() DriverType      // Retorna o tipo do driver desta DataSource.

	// Acesso Subjacente (Escape Hatches)
	GetDB() (*sql.DB, error)                   // Retorna o objeto *sql.DB subjacente para bancos SQL.
	GetNativeConnection() (interface{}, error) // Retorna a conexão/cliente nativo do driver subjacente.

	// Métodos de Execução de Query
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) // Executa comandos sem retorno de linhas (INSERT, UPDATE, DELETE).
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) // Executa consultas que retornam múltiplas linhas (SELECT).
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row        // Executa consultas que retornam no máximo uma linha (SELECT ... LIMIT 1).

	// Gerenciamento de Transações
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) // Inicia uma transação nativa do banco.

	// Prepared Statements (Declarações Preparadas)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) // Cria um statement preparado para reutilização.
}

// --- Registro de Drivers ---

// DriverFactory define a assinatura para uma função que cria uma nova instância,
// não inicializada, de uma implementação específica de DataSource.
type DriverFactory func() DataSource

var (
	// driverRegistry armazena as fábricas para cada tipo de driver registrado.
	// O acesso a este mapa deve ser protegido por registryMutex.
	driverRegistry = make(map[DriverType]DriverFactory)
	// registryMutex protege o acesso concorrente a driverRegistry.
	registryMutex sync.RWMutex // RWMutex permite múltiplas leituras simultâneas.
)

// RegisterDriver registra uma DriverFactory para um determinado nome de tipo de driver.
// Esta função deve ser chamada pelos pacotes de driver durante sua fase init().
// É seguro para uso concorrente.
func RegisterDriver(name DriverType, factory DriverFactory) {
	registryMutex.Lock() // Bloqueio de escrita para modificar o mapa
	defer registryMutex.Unlock()

	if factory == nil {
		panic(fmt.Sprintf("typegorm: A fábrica (factory) para RegisterDriver do driver %q é nula", name))
	}
	if _, registered := driverRegistry[name]; registered {
		panic(fmt.Sprintf("typegorm: O driver %q já está registrado", name))
	}

	driverRegistry[name] = factory
	fmt.Printf("[LOG-typegorm] Fábrica de driver registrada para: %s\n", name) // Log de registro
}

// --- Fábrica de Conexão ---

// Connect é a função de alto nível que os usuários chamam para estabelecer uma conexão com o banco.
// Determina o driver correto baseado no tipo da config (usando a interface DriverTyper),
// recupera a fábrica do driver correspondente, cria uma instância de DataSource,
// e chama seu método Connect.
func Connect(cfg Config) (DataSource, error) {
	if cfg == nil {
		return nil, errors.New("typegorm: configuração (cfg) não pode ser nula")
	}

	// --- Obtém DriverType via Interface ---
	typer, ok := cfg.(DriverTyper)
	if !ok {
		// Usa o verbo %T para informação útil do tipo no erro
		return nil, fmt.Errorf("typegorm: tipo de configuração %T não implementa a interface typegorm.DriverTyper", cfg)
	}
	driverType := typer.GetType()
	// --- Fim da Verificação da Interface ---

	fmt.Printf("[LOG-typegorm] Connect: Determinado tipo de driver %q a partir do método GetType() da config\n", driverType)

	// Obtém a fábrica para o tipo de driver determinado
	registryMutex.RLock() // Bloqueio de leitura para acessar o registro
	factory, ok := driverRegistry[driverType]
	registryMutex.RUnlock() // Libera bloqueio de leitura

	if !ok {
		// Verifica se o pacote do driver pode não ter sido importado
		// A importação requer o uso do identificador branco: _ "caminho/para/driver"
		return nil, fmt.Errorf("typegorm: driver %q não registrado (esqueceu de importar o pacote do driver com `_` para efeitos colaterais?)", driverType)
	}

	// Cria uma nova instância de DataSource usando a fábrica
	dataSource := factory()
	if dataSource == nil {
		// Não deve acontecer se RegisterDriver impedir fábricas nulas
		return nil, fmt.Errorf("typegorm: fábrica para driver %q retornou DataSource nulo", driverType)
	}
	fmt.Printf("[LOG-typegorm] Connect: Instância de DataSource criada via fábrica para %q.\n", driverType)

	// Chama o método Connect na implementação específica do DataSource
	fmt.Printf("[LOG-typegorm] Connect: Chamando dataSource.Connect(%+v)...\n", cfg)
	err := dataSource.Connect(cfg)
	if err != nil {
		// Não encapsula o erro aqui, deixa o erro específico do driver passar
		return nil, err
	}

	fmt.Printf("[LOG-typegorm] Connect: dataSource.Connect() bem-sucedido para %q.\n", driverType)
	return dataSource, nil
}
