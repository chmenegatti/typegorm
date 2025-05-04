// pkg/typegorm.go
package typegorm // Ou o nome do módulo raiz, se preferir

import (
	"fmt"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/dialects" // Importa o registro
	"github.com/chmenegatti/typegorm/pkg/schema"
	// Importa as interfaces
	// Drivers específicos serão importados pelo usuário via blank import _
)

func Open(cfg config.Config) (*DB, error) {
	dialectName := cfg.Database.Dialect
	if dialectName == "" {
		return nil, fmt.Errorf("database dialect not specified in configuration")
	}

	// 1. Get DataSource Factory
	factory := dialects.Get(dialectName)
	if factory == nil {
		return nil, fmt.Errorf("unsupported or unregistered dialect: '%s'. Ensure the driver package was blank imported", dialectName)
	}

	// 2. Create and Connect DataSource
	ds := factory()
	if ds == nil {
		return nil, fmt.Errorf("internal error: factory for dialect '%s' returned nil DataSource", dialectName)
	}
	err := ds.Connect(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect data source for dialect '%s': %w", dialectName, err)
	}

	// 3. Create Schema Parser (using default naming strategy for now)
	// TODO: Allow configuration of naming strategy
	parser := schema.NewParser(nil)

	// 4. Create and return the DB handle
	db := NewDB(ds, parser, cfg) // Pass ds, parser, and cfg

	fmt.Printf("TypeGORM DB handle created successfully for dialect '%s'.\n", dialectName)
	return db, nil
}
