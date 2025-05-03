// pkg/config/config.go
package config

import "time"

// PoolConfig define as configurações do pool de conexões.
// PoolConfig holds connection pool settings.
type PoolConfig struct {
	// MaxIdleConns is the maximum number of connections in the idle connection pool.
	MaxIdleConns int `mapstructure:"maxIdleConns"`

	// MaxOpenConns is the maximum number of open connections to the database.
	// If MaxOpenConns is <= 0, then there is no limit on the number of open connections.
	MaxOpenConns int `mapstructure:"maxOpenConns"`

	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// Expired connections may be closed lazily before reuse.
	// If <= 0, connections are not closed due to a connection's age.
	ConnMaxLifetime time.Duration `mapstructure:"connMaxLifetime"`

	// ConnMaxIdleTime is the maximum amount of time a connection may be idle.
	// Expired connections may be closed lazily before reuse.
	// If <= 0, connections are not closed due to a connection's idle time.
	// *** ADDED THIS FIELD ***
	ConnMaxIdleTime time.Duration `mapstructure:"connMaxIdleTime"`
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
