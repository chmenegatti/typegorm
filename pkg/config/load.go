// pkg/config/load.go (Correção das mensagens de erro)
package config

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// LoadConfig loads configuration from files, environment variables, and defaults.
// configPath: optional path to a specific configuration file.
// If configPath is empty, searches for "typegorm.yaml" in standard locations.
func LoadConfig(configPath string) (Config, error) {
	v := viper.New()
	cfg := NewDefaultConfig() // Start with defaults

	// 1. Set defaults in Viper
	v.SetDefault("database.pool.maxIdleConns", cfg.Database.Pool.MaxIdleConns)
	v.SetDefault("database.pool.maxOpenConns", cfg.Database.Pool.MaxOpenConns)
	v.SetDefault("database.pool.connMaxLifetime", cfg.Database.Pool.ConnMaxLifetime)
	v.SetDefault("logging.level", cfg.Logging.Level)
	v.SetDefault("logging.format", cfg.Logging.Format)
	v.SetDefault("migration.directory", cfg.Migration.Directory)

	// 2. Configure reading from Environment Variables
	v.SetEnvPrefix("TYPEGORM")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 3. Configure reading from Configuration File
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("typegorm")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.typegorm")
	}

	// Attempt to read the configuration file
	if err := v.ReadInConfig(); err != nil {
		// Error is only fatal if it's not "file not found" AND a specific configPath was given
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok || configPath != "" {
			// Use English error message here
			return cfg, fmt.Errorf("error reading configuration file: %w", err)
		}
		// Log or print that config file was not found (optional)
		// fmt.Println("Configuration file not found, using defaults/env vars.")
	}

	// 4. Unmarshal the configuration into the Config struct
	if err := v.Unmarshal(&cfg); err != nil {
		// Use English error message here
		return cfg, fmt.Errorf("error unmarshaling configuration: %w", err)
	}

	// 5. Validate the configuration struct
	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		var validationErrors []string
		for _, err := range err.(validator.ValidationErrors) {
			validationErrors = append(validationErrors, fmt.Sprintf("Field '%s' failed validation on '%s'", err.Namespace(), err.Tag()))
		}
		// Use English error message here
		return cfg, fmt.Errorf("invalid configuration: %s", strings.Join(validationErrors, "; "))
	}

	// 6. Return the loaded and validated configuration
	return cfg, nil
}
