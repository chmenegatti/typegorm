// documentstore.go
package typegorm

import (
	"context"
	"fmt"
	"sync" // Para mutex do registro
	// Só para referência nos comentários/docs (opcional)
)

// DocumentStoreConfig interface marcador
type DocumentStoreConfig any

// DocumentStore interface (permanece igual)
type DocumentStore interface {
	Connect(cfg DocumentStoreConfig) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
	Client() any   // *mongo.Client
	Database() any // *mongo.Database
}

// --- Registro de Drivers de Document Store ---

// DocumentStoreFactory define a assinatura para criar instâncias de DocumentStore.
type DocumentStoreFactory func() DocumentStore

var (
	// documentStoreRegistry armazena as fábricas para cada tipo de driver NoSQL/Document.
	documentStoreRegistry = make(map[DriverType]DocumentStoreFactory)
	docRegistryMutex      sync.RWMutex
)

// RegisterDocumentStoreDriver registra uma fábrica para um tipo de driver de Document Store.
// Chamado pelos pacotes de driver (ex: mongo) em seu init(). Seguro para concorrência.
func RegisterDocumentStoreDriver(name DriverType, factory DocumentStoreFactory) {
	docRegistryMutex.Lock()
	defer docRegistryMutex.Unlock()

	if factory == nil {
		panic(fmt.Sprintf("typegorm: Fábrica para RegisterDocumentStoreDriver do driver %q é nula", name))
	}
	if _, registered := documentStoreRegistry[name]; registered {
		panic(fmt.Sprintf("typegorm: Driver de Document Store %q já registrado", name))
	}

	documentStoreRegistry[name] = factory
	fmt.Printf("[LOG-typegorm] Fábrica de driver DocumentStore registrada para: %s\n", name)
}

// --- Fábrica de Conexão para Document Stores (Atualizada) ---

// ConnectDocumentStore conecta usando o registro de drivers.
func ConnectDocumentStore(cfg DocumentStoreConfig) (DocumentStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("typegorm: configuration DocumentStoreConfig não pode ser nula")
	}

	// --- Obtém DriverType via Interface DriverTyper ---
	// Assume que a config implementa DriverTyper (como fizemos para SQL)
	typer, ok := cfg.(DriverTyper) // Reutilizando DriverTyper por consistência
	if !ok {
		// Se DocumentStoreConfig não implementar DriverTyper, não sabemos qual driver usar!
		return nil, fmt.Errorf("typegorm: tipo de configuração %T não implementa a interface typegorm.DriverTyper", cfg)
	}
	driverType := typer.GetType()
	// --- Fim Verificação Interface ---

	fmt.Printf("[LOG-typegorm] ConnectDocumentStore: Determinado tipo de driver %q via GetType() da config\n", driverType)

	// Obtém a fábrica do registro
	docRegistryMutex.RLock()
	factory, ok := documentStoreRegistry[driverType]
	docRegistryMutex.RUnlock()

	if !ok {
		// O pacote do driver pode não ter sido importado com '_'
		return nil, fmt.Errorf("typegorm: driver de Document Store %q não registrado (esqueceu de importar o pacote do driver com `_`?)", driverType)
	}

	// Cria a instância usando a fábrica registrada
	docStore := factory()
	if docStore == nil {
		return nil, fmt.Errorf("typegorm: fábrica para driver %q retornou DocumentStore nulo", driverType)
	}
	fmt.Printf("[LOG-typegorm] ConnectDocumentStore: Instância de DocumentStore criada via fábrica para %q.\n", driverType)

	// Chama o Connect interno da instância criada
	fmt.Printf("[LOG-typegorm] ConnectDocumentStore: Chamando docStore.Connect(%+v)...\n", cfg)
	err := docStore.Connect(cfg) // Passa a config original
	if err != nil {
		return nil, err // Deixa o erro do driver passar
	}

	fmt.Printf("[LOG-typegorm] ConnectDocumentStore: docStore.Connect() bem-sucedido para %q.\n", driverType)
	return docStore, nil
}
