// pkg/config/load.go
package config

import (
	"fmt"
	"log" // Import log for temporary debugging
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// LoadConfig loads the TypeGORM configuration from various sources.
// Precedence order: Environment Variables > Config File > Default Values.
// Validates the resulting configuration.
func LoadConfig(configPath string) (Config, error) {
	// 1. Create a new local Viper instance
	v := viper.New()

	// Get the struct with default values defined in NewDefaultConfig()
	// These serve as the base before being potentially overridden.
	cfg := NewDefaultConfig()

	// 2. Configure the local Viper instance to read environment variables
	v.SetEnvPrefix("TYPEGORM")                         // Prefix for environment variables (e.g., TYPEGORM_DATABASE_DSN)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_")) // Map keys with dots (database.dsn) to env var format (DATABASE_DSN)
	v.AutomaticEnv()                                   // Automatically read matching environment variables

	// 3. Read the configuration file
	if configPath != "" {
		// If a path was EXPLICITLY provided by the user
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			// If the user specified a file, an error reading it should be returned.
			return cfg, fmt.Errorf("error reading specified config file '%s': %w", configPath, err)
		}
		log.Printf("[LoadConfig DEBUG] Read specified config file: %s\n", configPath) // Debug log
	} else {
		// If NO path was provided, try reading default config files (optionally)
		v.SetConfigName("typegorm")        // Name of the file to look for (without extension)
		v.SetConfigType("yaml")            // Type of the config file
		v.AddConfigPath(".")               // Look in the current directory (.)
		v.AddConfigPath("$HOME/.typegorm") // Look in ~/.typegorm/
		v.AddConfigPath("/etc/typegorm/")  // Look in /etc/typegorm/

		// Attempt to read the default config file.
		// Ignore 'file not found' errors (viper.ConfigFileNotFoundError),
		// as using a default file is optional. Other errors (e.g., permissions) might be relevant.
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				// Return error only if it's something other than 'file not found'
				return cfg, fmt.Errorf("error reading default config file: %w", err)
			}
			// If the error is viper.ConfigFileNotFoundError, just ignore it and continue.
			log.Println("[LoadConfig DEBUG] Default config file not found or not used.") // Debug log
		} else {
			log.Printf("[LoadConfig DEBUG] Read default config file from: %s\n", v.ConfigFileUsed()) // Debug log
		}
	}

	// 4. Populate the 'cfg' struct with values read by Viper
	// Viper merges sources (file, env) onto the 'v' instance.
	// Unmarshal attempts to place these values into the 'cfg' struct,
	// overwriting the defaults that were already there.
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("error decoding configuration: %w", err)
	}

	// 4.1 (Explicit Reinforcement Post-Unmarshal)
	// Ensures environment variables have the correct precedence, especially
	// if Unmarshal or AutomaticEnv have quirks.
	// Uses v.IsSet() to check if the key was defined by any source
	// (including env vars) and v.Get* to get the value (respecting precedence).
	log.Println("[LoadConfig DEBUG] Applying explicit reinforcement...") // Debug log
	if v.IsSet("database.dialect") {
		val := v.GetString("database.dialect")
		log.Printf("[LoadConfig DEBUG] Reinforcing database.dialect: IsSet=true, Value=%q\n", val) // Debug log
		cfg.Database.Dialect = val
	} else {
		log.Println("[LoadConfig DEBUG] Reinforcing database.dialect: IsSet=false") // Debug log
	}
	if v.IsSet("database.dsn") {
		val := v.GetString("database.dsn")
		log.Printf("[LoadConfig DEBUG] Reinforcing database.dsn: IsSet=true, Value=%q\n", val) // Debug log
		cfg.Database.DSN = val
	} else {
		log.Println("[LoadConfig DEBUG] Reinforcing database.dsn: IsSet=false") // Debug log
	}
	// Apply for other relevant fields...
	if v.IsSet("logging.level") {
		cfg.Logging.Level = v.GetString("logging.level")
	}
	if v.IsSet("logging.format") {
		cfg.Logging.Format = v.GetString("logging.format")
	}
	if v.IsSet("database.pool.maxidleconns") {
		cfg.Database.Pool.MaxIdleConns = v.GetInt("database.pool.maxidleconns")
	}
	if v.IsSet("database.pool.maxopenconns") {
		cfg.Database.Pool.MaxOpenConns = v.GetInt("database.pool.maxopenconns")
	}
	if v.IsSet("database.pool.connmaxlifetime") {
		durationVal := v.GetDuration("database.pool.connmaxlifetime")
		if durationVal > 0 {
			cfg.Database.Pool.ConnMaxLifetime = durationVal
		} else {
			durationStr := v.GetString("database.pool.connmaxlifetime")
			if parsedDuration, err := time.ParseDuration(durationStr); err == nil {
				cfg.Database.Pool.ConnMaxLifetime = parsedDuration
			}
		}
	}
	if v.IsSet("migration.directory") {
		cfg.Migration.Directory = v.GetString("migration.directory")
	}
	if v.IsSet("migration.tablename") {
		cfg.Migration.TableName = v.GetString("migration.tablename")
	}
	log.Println("[LoadConfig DEBUG] Finished reinforcement.") // Debug log

	// 5. Validate the final 'cfg' struct (after all sources have been applied)
	validate := validator.New()
	log.Println("[LoadConfig DEBUG] Performing validation...") // Debug log
	if err := validate.Struct(cfg); err != nil {               // If validation FAILS, err is non-nil
		log.Printf("[LoadConfig DEBUG] Validation FAILED: %v\n", err) // Debug log
		var validationErrors []string
		// Try converting the error to ValidationErrors to get details
		if vErrs, ok := err.(validator.ValidationErrors); ok {
			for _, vErr := range vErrs {
				fieldName := vErr.Namespace() // e.g., Config.Database.DSN
				tag := vErr.Tag()             // e.g., required
				// Format a helpful error message (English)
				msg := fmt.Sprintf("Field '%s' failed validation on '%s'", fieldName, tag)
				validationErrors = append(validationErrors, msg)
			}
		} else {
			// If the error is not ValidationErrors type, just include the general message
			validationErrors = append(validationErrors, err.Error())
		}
		// Return a combined error indicating validation failure
		return cfg, fmt.Errorf("invalid configuration: %s", strings.Join(validationErrors, "; "))
	}
	log.Println("[LoadConfig DEBUG] Validation PASSED.") // Debug log

	// 6. Return the successfully loaded and validated configuration
	return cfg, nil // Returns nil error if validation passed
}
