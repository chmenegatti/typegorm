// pkg/typegorm.go
package typegorm // Ou o nome do módulo raiz, se preferir

import (
	"fmt"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/dialects"        // Importa o registro
	"github.com/chmenegatti/typegorm/pkg/dialects/common" // Importa as interfaces
	// Drivers específicos serão importados pelo usuário via blank import _
)

// Open inicializa e retorna um DataSource com base na configuração fornecida.
// Ele seleciona dinamicamente o driver apropriado que foi registrado.
func Open(cfg config.Config) (common.DataSource, error) {
	dialectName := cfg.Database.Dialect
	if dialectName == "" {
		return nil, fmt.Errorf("dialeto não especificado na configuração")
	}

	// 1. Procurar a factory do dialeto no registro
	factory := dialects.Get(dialectName)
	if factory == nil {
		return nil, fmt.Errorf("dialeto desconhecido ou não registrado: '%s'. Verifique se importou o driver com blank import (_)", dialectName)
	}

	// 2. Criar a instância do DataSource usando a factory
	ds := factory()
	if ds == nil {
		// A factory não deveria retornar nil, mas checamos por segurança.
		return nil, fmt.Errorf("a factory para o dialeto '%s' retornou um DataSource nil", dialectName)
	}

	// 3. Conectar o DataSource usando a configuração específica do banco
	err := ds.Connect(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar usando o dialeto '%s': %w", dialectName, err)
	}

	// 4. Retornar o DataSource conectado e pronto para uso
	return ds, nil
}
