// pkg/dialects/registry.go
package dialects

import (
	"sync"

	"github.com/chmenegatti/typegorm/pkg/dialects/common"
)

// DataSourceFactory é uma função que cria e retorna uma nova instância (não conectada)
// de um DataSource para um dialeto específico.
type DataSourceFactory func() common.DataSource

var (
	driversMu sync.RWMutex // Mutex para proteger acesso concorrente ao mapa
	drivers   = make(map[string]DataSourceFactory)
)

// Register torna um driver DataSource disponível pelo nome fornecido.
// Se Register for chamado duas vezes para o mesmo nome, ele entra em pânico.
// A função factory deve retornar uma nova instância de DataSource a cada chamada.
func Register(name string, factory DataSourceFactory) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if factory == nil {
		panic("dialects: Register factory is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("dialects: Register called twice for driver " + name)
	}
	drivers[name] = factory
}

// Get retrieves a DataSourceFactory pelo nome do dialeto.
// Retorna nil se o dialeto não for encontrado.
func Get(name string) DataSourceFactory {
	driversMu.RLock()
	defer driversMu.RUnlock()
	factory := drivers[name]
	// Não retorna a factory diretamente do mapa para evitar que o chamador
	// modifique o mapa interno (embora improvável com RWMutex).
	// Retornar o valor diretamente é seguro aqui.
	return factory
}

// RegisteredDrivers retorna uma lista dos nomes de todos os drivers registrados.
func RegisteredDrivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	list := make([]string, 0, len(drivers))
	for name := range drivers {
		list = append(list, name)
	}
	return list
}
