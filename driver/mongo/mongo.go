// driver/mongo/mongo.go
package mongo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	// Importa tipos e funções do driver oficial MongoDB e do TypeGorm

	"github.com/chmenegatti/typegorm"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Config define os parâmetros de conexão para MongoDB.
// A forma mais comum é via URI, mas pode incluir opções separadas.
type Config struct {
	// URI da string de conexão (ex: "mongodb://user:pass@host:port/db?authSource=admin")
	URI string `json:"uri" yaml:"uri"`
	// Nome do banco de dados padrão a ser usado. Se vazio, pode ser pego da URI.
	DatabaseName string `json:"database" yaml:"database"`

	// TODO: Adicionar campos para opções de Client (Timeout, PoolSize, etc.) se necessário
	// ConnectTimeout time.Duration
	// MaxPoolSize    uint64
}

// GetType retorna o tipo deste driver.
func (c Config) GetType() typegorm.DriverType {
	return typegorm.Mongo // Usa a constante definida em datasource.go
}

// --- Verificações em tempo de compilação ---
var _ typegorm.DocumentStore = (*MongoDataSource)(nil)
var _ typegorm.DocumentStoreConfig = Config{}
var _ typegorm.DriverTyper = Config{} // <-- Verifica DriverTyper

// MongoDataSource implementa a interface typegorm.DocumentStore para MongoDB.
type MongoDataSource struct {
	config       Config
	client       *mongo.Client      // Cliente principal do MongoDB
	database     *mongo.Database    // Instância do banco de dados padrão
	connMu       sync.RWMutex       // Protege client/database durante Connect/Disconnect
	connectCtx   context.Context    // Contexto usado para conectar
	connectCtxCf context.CancelFunc // Função para cancelar o contexto de conexão
}

func init() {
	// Registra a fábrica deste driver no registro central de Document Stores
	typegorm.RegisterDocumentStoreDriver(typegorm.Mongo, func() typegorm.DocumentStore {
		return NewDataSource() // Ou &MongoDataSource{}
	})
}

// NewDataSource é uma fábrica simples para este driver.
func NewDataSource() *MongoDataSource {
	return &MongoDataSource{}
}

// Connect implementa typegorm.DocumentStore.Connect.
func (s *MongoDataSource) Connect(cfg typegorm.DocumentStoreConfig) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] mongo.Connect: Entrou na função, mutex adquirido.")

	if s.client != nil {
		fmt.Println("[LOG] mongo.Connect: Conexão já estabelecida.")
		return errors.New("mongo: conexão já estabelecida (cliente existente)")
	}

	// Asserção de tipo para a config concreta
	mongoConfig, ok := cfg.(Config)
	if !ok {
		if ptrCfg, okPtr := cfg.(*Config); okPtr && ptrCfg != nil {
			mongoConfig = *ptrCfg
			ok = true
		}
	}
	if !ok {
		fmt.Println("[LOG] mongo.Connect: Tipo de configuração inválido passado.")
		return fmt.Errorf("mongo: tipo de configuração inválido %T passado para o método Connect", cfg)
	}
	s.config = mongoConfig // Armazena a config

	if s.config.URI == "" {
		return errors.New("mongo: URI é obrigatório na configuração")
	}

	fmt.Printf("[LOG] mongo.Connect: Usando URI: %s\n", "[URI OMITIDO POR SEGURANÇA]")
	// fmt.Printf("[LOG] mongo.Connect: Usando URI (DEBUG): %s\n", s.config.URI) // Cuidado em produção

	// Configura opções do cliente a partir da URI
	clientOptions := options.Client().ApplyURI(s.config.URI)

	// Define um contexto para a operação de conexão (com timeout)
	// Armazenamos o contexto e sua função de cancelamento para usar no Disconnect
	// Usar um timeout generoso para conexão inicial
	s.connectCtx, s.connectCtxCf = context.WithTimeout(context.Background(), 20*time.Second)

	// Conecta ao MongoDB
	fmt.Println("[LOG] mongo.Connect: Chamando mongo.Connect()...")
	client, err := mongo.Connect(s.connectCtx, clientOptions)
	if err != nil {
		fmt.Printf("[LOG] mongo.Connect: mongo.Connect() falhou: %v\n", err)
		s.connectCtxCf() // Cancela o contexto se falhar
		return fmt.Errorf("mongo: falha ao conectar com o servidor: %w", err)
	}
	fmt.Println("[LOG] mongo.Connect: mongo.Connect() bem-sucedido.")

	// --- Verificação com Ping ---
	// É crucial verificar se a conexão foi realmente estabelecida
	fmt.Println("[LOG] mongo.Connect: Chamando Ping() para verificação...")
	// Usar um contexto separado para o Ping, derivado do de conexão ou um novo
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second) // Timeout mais curto para ping
	defer pingCancel()
	// Usa ReadPreference Primary para garantir que estamos falando com o nó primário (importante para consistência inicial)
	err = client.Ping(pingCtx, readpref.Primary())
	if err != nil {
		fmt.Printf("[LOG] mongo.Connect: Ping() inicial falhou: %v\n", err)
		// Tentamos desconectar mesmo se o ping falhar para liberar recursos parciais
		_ = client.Disconnect(s.connectCtx) // Ignora erro de disconnect aqui
		s.connectCtxCf()                    // Cancela o contexto de conexão
		return fmt.Errorf("mongo: falha ao fazer ping no servidor após conectar: %w", err)
	}
	fmt.Println("[LOG] mongo.Connect: Ping() inicial bem-sucedido.")
	// --- Fim da Verificação com Ping ---

	s.client = client // Armazena o cliente conectado

	// Define o banco de dados padrão
	dbName := s.config.DatabaseName
	if dbName == "" {
		// Tenta obter do clientOptions (se foi parseado da URI)
		// Isso é um pouco complexo, uma alternativa é exigir DatabaseName na config
		// Por simplicidade, vamos exigir por enquanto ou deixar nulo.
		fmt.Println("[LOG] mongo.Connect: Nome do banco de dados padrão não fornecido explicitamente na config.")
		// s.database será nil, o usuário precisará chamar client.Database("nome")
	} else {
		s.database = client.Database(dbName)
		fmt.Printf("[LOG] mongo.Connect: Banco de dados padrão definido como '%s'.\n", dbName)
	}

	fmt.Println("[LOG] mongo.Connect: Conexão MongoDB configurada com sucesso.")
	fmt.Println("[LOG] mongo.Connect: Saindo da função, mutex liberado.")
	return nil
}

// Disconnect implementa typegorm.DocumentStore.Disconnect.
func (s *MongoDataSource) Disconnect(ctx context.Context) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] mongo.Disconnect: Entrou na função, mutex adquirido.")

	if s.client == nil {
		fmt.Println("[LOG] mongo.Disconnect: Cliente não conectado ou já desconectado.")
		// Não é necessariamente um erro se chamado múltiplas vezes
		return nil // errors.New("mongo: cliente não conectado")
	}

	fmt.Println("[LOG] mongo.Disconnect: Chamando client.Disconnect()...")
	err := s.client.Disconnect(ctx) // Usa o contexto passado para o timeout do disconnect

	// Cancela o contexto que foi usado para conectar (se ainda não cancelado)
	if s.connectCtxCf != nil {
		s.connectCtxCf()
		s.connectCtxCf = nil
		s.connectCtx = nil
	}

	s.client = nil   // Limpa a referência
	s.database = nil // Limpa a referência

	if err != nil {
		fmt.Printf("[LOG] mongo.Disconnect: client.Disconnect() falhou: %v\n", err)
		return fmt.Errorf("mongo: erro ao desconectar: %w", err)
	}

	fmt.Println("[LOG] mongo.Disconnect: Desconexão bem-sucedida.")
	fmt.Println("[LOG] mongo.Disconnect: Saindo da função, mutex liberado.")
	return nil
}

// Ping implementa typegorm.DocumentStore.Ping.
func (s *MongoDataSource) Ping(ctx context.Context) error {
	fmt.Println("[LOG] mongo.Ping: Entrou na função.")
	s.connMu.Lock() // Lock aqui para ler s.client de forma segura
	client := s.client
	s.connMu.Unlock()

	if client == nil {
		fmt.Println("[LOG] mongo.Ping: Cliente não conectado.")
		return errors.New("mongo: cliente não conectado")
	}

	fmt.Println("[LOG] mongo.Ping: Chamando client.Ping()...")
	err := client.Ping(ctx, readpref.Primary()) // Ping no primário
	if err != nil {
		fmt.Printf("[LOG] mongo.Ping: client.Ping() falhou: %v\n", err)
		return fmt.Errorf("mongo: ping falhou: %w", err)
	}

	fmt.Println("[LOG] mongo.Ping: client.Ping() bem-sucedido.")
	return nil
}

// Client retorna o cliente *mongo.Client nativo.
func (s *MongoDataSource) Client() interface{} {
	s.connMu.RLock() // Apenas leitura do ponteiro
	defer s.connMu.RUnlock()
	return s.client // Retorna nil se não conectado
}

// Database retorna a instância *mongo.Database padrão configurada.
// Pode retornar nil se DatabaseName não foi fornecido na config e Connect não pôde inferir.
func (s *MongoDataSource) Database() interface{} {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.database // Retorna nil se não configurado
}
