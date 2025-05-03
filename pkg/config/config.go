// pkg/config/config.go
package config

import "time"

// PoolConfig define as configurações do pool de conexões.
type PoolConfig struct {
	MaxIdleConns    int           `mapstructure:"maxIdleConns"`
	MaxOpenConns    int           `mapstructure:"maxOpenConns"`
	ConnMaxLifetime time.Duration `mapstructure:"connMaxLifetime"` // Ex: "1h", "30m"
}

// DatabaseConfig define as configurações de conexão com o banco.
type DatabaseConfig struct {
	Dialect string     `mapstructure:"dialect" validate:"required"` // Ex: "mysql", "sqlite", "mongodb"
	DSN     string     `mapstructure:"dsn"     validate:"required"` // Data Source Name específico do dialeto
	Pool    PoolConfig `mapstructure:"pool"`
}

// LoggingConfig define as configurações de logging.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // Ex: "debug", "info", "warn", "error"
	Format string `mapstructure:"format"` // Ex: "text", "json"
}

// MigrationConfig define as configurações do sistema de migration.
type MigrationConfig struct {
	Directory string `mapstructure:"directory"` // Diretório onde os arquivos de migration estão localizados
	TableName string `mapstructure:"tableName"` // Nome da tabela de controle de migrations
}

// Config é a struct principal que agrega todas as configurações.
type Config struct {
	Database  DatabaseConfig  `mapstructure:"database"`
	Logging   LoggingConfig   `mapstructure:"logging"`
	Migration MigrationConfig `mapstructure:"migration"`
}

// NewDefaultConfig cria uma configuração com valores padrão.
func NewDefaultConfig() Config {
	// Returns the default configuration settings.
	return Config{
		Database: DatabaseConfig{
			// Dialect and DSN have no defaults, they are required user input.
			Pool: PoolConfig{
				MaxIdleConns:    10,
				MaxOpenConns:    100,
				ConnMaxLifetime: 1 * time.Hour,
			},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text", // or "json"
		},
		Migration: MigrationConfig{
			Directory: "migrations",
			TableName: "schema_migrations",
		},
	}
}
