// pkg/config/load_test.go
package config

import (
	"log" // Import log for test-side debugging if needed
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a temporary config file
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test_config.yaml")
	err := os.WriteFile(tempFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to write temp config file")
	return tempFile
}

// Test that defaults are applied when required fields are provided (via env).
func TestLoadConfig_DefaultsApplied(t *testing.T) {
	log.Println("--- Running TestLoadConfig_DefaultsApplied ---")
	// Provide required fields via env to pass validation
	t.Setenv("TYPEGORM_DATABASE_DIALECT", "sqlite")
	t.Setenv("TYPEGORM_DATABASE_DSN", "file::memory:?cache=shared")

	// Ensure other fields with defaults are NOT set via env using t.Setenv
	t.Setenv("TYPEGORM_DATABASE_POOL_MAXIDLECONNS", "")
	t.Setenv("TYPEGORM_DATABASE_POOL_MAXOPENCONNS", "")
	t.Setenv("TYPEGORM_DATABASE_POOL_CONNMAXLIFETIME", "")
	t.Setenv("TYPEGORM_LOGGING_LEVEL", "")
	t.Setenv("TYPEGORM_LOGGING_FORMAT", "")
	t.Setenv("TYPEGORM_MIGRATION_DIRECTORY", "")
	t.Setenv("TYPEGORM_MIGRATION_TABLENAME", "")

	cfg, err := LoadConfig("") // Load without specifying a config file path
	require.NoError(t, err, "Loading config with required fields via env should not error")

	// Assert default values for non-required fields
	defaults := NewDefaultConfig()
	assert.Equal(t, defaults.Database.Pool.MaxIdleConns, cfg.Database.Pool.MaxIdleConns, "Default MaxIdleConns mismatch")
	assert.Equal(t, defaults.Database.Pool.MaxOpenConns, cfg.Database.Pool.MaxOpenConns, "Default MaxOpenConns mismatch")
	assert.Equal(t, defaults.Database.Pool.ConnMaxLifetime, cfg.Database.Pool.ConnMaxLifetime, "Default ConnMaxLifetime mismatch")
	assert.Equal(t, defaults.Logging.Level, cfg.Logging.Level, "Default Logging.Level mismatch")
	assert.Equal(t, defaults.Logging.Format, cfg.Logging.Format, "Default Logging.Format mismatch")
	assert.Equal(t, defaults.Migration.Directory, cfg.Migration.Directory, "Default Migration.Directory mismatch")
	assert.Equal(t, defaults.Migration.TableName, cfg.Migration.TableName, "Default Migration.TableName mismatch")

	// Assert required fields were loaded from env
	assert.Equal(t, "sqlite", cfg.Database.Dialect, "Dialect from env mismatch")
	assert.Equal(t, "file::memory:?cache=shared", cfg.Database.DSN, "DSN from env mismatch")
}

// Test validation error when required fields are missing or empty.
func TestLoadConfig_Error_MissingRequiredFields(t *testing.T) {
	log.Println("--- Running TestLoadConfig_Error_MissingRequiredFields ---")
	// Ensure required env vars are empty using t.Setenv for guaranteed cleanup
	t.Setenv("TYPEGORM_DATABASE_DIALECT", "")
	t.Setenv("TYPEGORM_DATABASE_DSN", "")
	// Also clear others that might interfere
	t.Setenv("TYPEGORM_LOGGING_LEVEL", "")

	// Attempt to load without config file and with empty required env vars
	cfg, err := LoadConfig("") // Assign cfg as well to check if needed

	// Assert that an error occurred - THIS IS THE KEY ASSERTION
	require.Error(t, err, "An error was expected when required fields are missing or empty, but got nil")

	// If require.Error passes, check the error content
	if err != nil {
		assert.Contains(t, err.Error(), "invalid configuration:", "Error message prefix mismatch")
		// Check for specific field validation errors (using Namespace)
		assert.Contains(t, err.Error(), "Field 'Config.Database.Dialect' failed validation on 'required'", "Validation message for Dialect missing")
		assert.Contains(t, err.Error(), "Field 'Config.Database.DSN' failed validation on 'required'", "Validation message for DSN missing")
	} else {
		// Log the config if error was nil unexpectedly
		log.Printf("[Test Error] LoadConfig returned nil error unexpectedly. Config was: %+v", cfg)
	}
}

// Test successful loading from a file.
func TestLoadConfig_FromFile(t *testing.T) {
	log.Println("--- Running TestLoadConfig_FromFile ---")
	configContent := `
database:
  dialect: "mysql"
  dsn: "user:pass@tcp(host:3306)/db?parseTime=true"
  pool:
    maxOpenConns: 50
    connMaxLifetime: "30m"
logging:
  level: "debug"
migration:
  directory: "/app/db/migrations"
  tableName: "custom_migrations"
`
	configFile := createTempConfigFile(t, configContent)
	// Clear env vars to ensure values come from the file
	t.Setenv("TYPEGORM_DATABASE_DIALECT", "")
	t.Setenv("TYPEGORM_DATABASE_DSN", "")
	t.Setenv("TYPEGORM_DATABASE_POOL_MAXOPENCONNS", "")
	t.Setenv("TYPEGORM_DATABASE_POOL_CONNMAXLIFETIME", "")
	t.Setenv("TYPEGORM_LOGGING_LEVEL", "")
	t.Setenv("TYPEGORM_MIGRATION_DIRECTORY", "")
	t.Setenv("TYPEGORM_MIGRATION_TABLENAME", "")

	cfg, err := LoadConfig(configFile)
	require.NoError(t, err)

	// Assert values loaded from file
	assert.Equal(t, "mysql", cfg.Database.Dialect)
	assert.Equal(t, "user:pass@tcp(host:3306)/db?parseTime=true", cfg.Database.DSN)
	assert.Equal(t, 50, cfg.Database.Pool.MaxOpenConns)
	assert.Equal(t, 30*time.Minute, cfg.Database.Pool.ConnMaxLifetime)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "/app/db/migrations", cfg.Migration.Directory)
	assert.Equal(t, "custom_migrations", cfg.Migration.TableName)

	// Assert defaults were kept where not overridden
	defaults := NewDefaultConfig()
	assert.Equal(t, defaults.Database.Pool.MaxIdleConns, cfg.Database.Pool.MaxIdleConns)
	assert.Equal(t, defaults.Logging.Format, cfg.Logging.Format)
}

// Test successful loading from environment variables.
func TestLoadConfig_FromEnvVars(t *testing.T) {
	log.Println("--- Running TestLoadConfig_FromEnvVars ---")
	// Set environment variables
	t.Setenv("TYPEGORM_DATABASE_DIALECT", "postgres")
	t.Setenv("TYPEGORM_DATABASE_DSN", "postgres://user:pw@host:5432/dbname?sslmode=disable")
	t.Setenv("TYPEGORM_DATABASE_POOL_MAXIDLECONNS", "7")
	t.Setenv("TYPEGORM_LOGGING_LEVEL", "warn")
	t.Setenv("TYPEGORM_LOGGING_FORMAT", "json")
	// Don't set others to test defaults

	cfg, err := LoadConfig("") // Load without config file
	require.NoError(t, err)

	// Assert values loaded from environment variables
	assert.Equal(t, "postgres", cfg.Database.Dialect)
	assert.Equal(t, "postgres://user:pw@host:5432/dbname?sslmode=disable", cfg.Database.DSN)
	assert.Equal(t, 7, cfg.Database.Pool.MaxIdleConns)
	assert.Equal(t, "warn", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)

	// Assert defaults for values not set via env
	defaults := NewDefaultConfig()
	assert.Equal(t, defaults.Database.Pool.MaxOpenConns, cfg.Database.Pool.MaxOpenConns)
	assert.Equal(t, defaults.Database.Pool.ConnMaxLifetime, cfg.Database.Pool.ConnMaxLifetime)
	assert.Equal(t, defaults.Migration.Directory, cfg.Migration.Directory)
	assert.Equal(t, defaults.Migration.TableName, cfg.Migration.TableName)
}

// Test correct precedence: Env > File > Default.
func TestLoadConfig_Precedence_EnvOverFileOverDefault(t *testing.T) {
	log.Println("--- Running TestLoadConfig_Precedence_EnvOverFileOverDefault ---")
	// File sets some values
	configContent := `
database:
  dialect: "sqlite-from-file" # Will be overridden by Env
  dsn: "file:from_file.db?cache=shared" # Will be overridden by Env
  pool:
    maxOpenConns: 20 # From File (not set in Env)
logging:
  level: "debug" # Will be overridden by Env
migration:
  directory: "migrations-from-file" # From File (not set in Env)
`
	configFile := createTempConfigFile(t, configContent)

	// Env vars override some file values and set others
	t.Setenv("TYPEGORM_DATABASE_DIALECT", "sqlite-from-env")           // Override file
	t.Setenv("TYPEGORM_DATABASE_DSN", "file:from_env.db?cache=shared") // Override file
	t.Setenv("TYPEGORM_LOGGING_LEVEL", "error")                        // Override file
	t.Setenv("TYPEGORM_MIGRATION_TABLENAME", "env_table")              // New value from Env

	// Clear env vars we don't want to set to test file/defaults using t.Setenv
	t.Setenv("TYPEGORM_DATABASE_POOL_MAXIDLECONNS", "")
	t.Setenv("TYPEGORM_DATABASE_POOL_MAXOPENCONNS", "") // Clear to ensure file value (20) is used
	t.Setenv("TYPEGORM_DATABASE_POOL_CONNMAXLIFETIME", "")
	t.Setenv("TYPEGORM_LOGGING_FORMAT", "")
	t.Setenv("TYPEGORM_MIGRATION_DIRECTORY", "") // Clear to ensure file value is used

	cfg, err := LoadConfig(configFile) // Load WITH the file
	require.NoError(t, err)

	// Assert Precedence
	assert.Equal(t, "sqlite-from-env", cfg.Database.Dialect, "Precedence: Env > File")
	assert.Equal(t, "file:from_env.db?cache=shared", cfg.Database.DSN, "Precedence: Env > File")
	assert.Equal(t, "error", cfg.Logging.Level, "Precedence: Env > File")
	assert.Equal(t, "env_table", cfg.Migration.TableName, "Precedence: Env (new)")
	assert.Equal(t, 20, cfg.Database.Pool.MaxOpenConns, "Precedence: File (not in env)")
	assert.Equal(t, "migrations-from-file", cfg.Migration.Directory, "Precedence: File (not in env)")

	// Assert Defaults for values not set anywhere
	defaults := NewDefaultConfig()
	assert.Equal(t, defaults.Database.Pool.MaxIdleConns, cfg.Database.Pool.MaxIdleConns, "Precedence: Default")
	assert.Equal(t, defaults.Database.Pool.ConnMaxLifetime, cfg.Database.Pool.ConnMaxLifetime, "Precedence: Default")
	assert.Equal(t, defaults.Logging.Format, cfg.Logging.Format, "Precedence: Default")
}

// Test error when a SPECIFIED config file is not found.
func TestLoadConfig_Error_SpecifiedFileNotFound(t *testing.T) {
	log.Println("--- Running TestLoadConfig_Error_SpecifiedFileNotFound ---")
	nonExistentPath := filepath.Join(t.TempDir(), "non_existent_config.yaml")
	_, err := LoadConfig(nonExistentPath)

	require.Error(t, err, "Expected error when loading non-existent specified file")
	assert.Contains(t, err.Error(), "error reading specified config file", "Error message prefix mismatch")
	assert.Contains(t, err.Error(), "non_existent_config.yaml", "Error message should mention the file path")
}

// Test behavior when NO file path is specified and the default config file is NOT found.
// LoadConfig should NOT return a file-not-found error in this case,
// BUT it SHOULD still return a VALIDATION error if required fields aren't provided via env.
func TestLoadConfig_Error_DefaultFileNotFoundButValidationFails(t *testing.T) {
	log.Println("--- Running TestLoadConfig_Error_DefaultFileNotFoundButValidationFails ---")
	// Simulate an environment without a default config file
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	errCd := os.Chdir(tempDir) // Change to a clean directory
	require.NoError(t, errCd)
	t.Cleanup(func() { os.Chdir(originalDir) }) // Change back after test

	// Ensure required env vars are empty using t.Setenv
	t.Setenv("TYPEGORM_DATABASE_DIALECT", "")
	t.Setenv("TYPEGORM_DATABASE_DSN", "")

	// Call LoadConfig WITHOUT specifying a path
	cfg, err := LoadConfig("")

	// Assert that a VALIDATION error occurred
	require.Error(t, err, "Expected validation error when default config file is missing and required fields are empty, but got nil")

	if err != nil {
		assert.Contains(t, err.Error(), "invalid configuration:", "Error message prefix mismatch")
		assert.NotContains(t, err.Error(), "error reading", "Error message should NOT be about reading a file")
		assert.Contains(t, err.Error(), "Dialect' failed validation on 'required'", "Validation message for Dialect missing")
		assert.Contains(t, err.Error(), "DSN' failed validation on 'required'", "Validation message for DSN missing")
	} else {
		log.Printf("[Test Error] LoadConfig returned nil error unexpectedly. Config was: %+v", cfg)
	}
}

// Test validation error when a loaded file has invalid/missing fields.
func TestLoadConfig_Error_ValidationFailedFromFile(t *testing.T) {
	log.Println("--- Running TestLoadConfig_Error_ValidationFailedFromFile ---")
	// File is missing the required 'dsn' field
	configContent := `
database:
  dialect: "mysql" # DSN is missing
logging:
  level: "info"
`
	configFile := createTempConfigFile(t, configContent)
	t.Setenv("TYPEGORM_DATABASE_DIALECT", "") // Ensure env doesn't provide it either
	t.Setenv("TYPEGORM_DATABASE_DSN", "")

	cfg, err := LoadConfig(configFile)
	require.Error(t, err, "Expected validation error for incomplete file")

	if err != nil {
		assert.Contains(t, err.Error(), "invalid configuration:", "Error message prefix mismatch")
		assert.Contains(t, err.Error(), "Field 'Config.Database.DSN' failed validation on 'required'", "Validation message for missing DSN")
		// Dialect was provided in the file, so it should not be in the error message
		assert.NotContains(t, err.Error(), "Dialect' failed validation on 'required'", "Dialect was provided, should not cause validation error")
	} else {
		log.Printf("[Test Error] LoadConfig returned nil error unexpectedly. Config was: %+v", cfg)
	}
}

// Test error when the config file is malformed (invalid YAML).
func TestLoadConfig_Error_MalformedFile(t *testing.T) {
	log.Println("--- Running TestLoadConfig_Error_MalformedFile ---")
	// Invalid YAML (e.g., incorrect indentation or syntax)
	configContent := `
database:
  dialect: mysql" # Unclosed string causes syntax error lower down potentially
logging: level: debug # Invalid mapping here
`
	configFile := createTempConfigFile(t, configContent)

	_, err := LoadConfig(configFile)
	require.Error(t, err, "Expected error loading malformed file")

	// Assert that the error comes from the file reading/parsing phase
	// *** CORRECTION APPLIED HERE ***
	assert.Contains(t, err.Error(), "error reading specified config file", "Error message should indicate config file read/parse failure")

	// Optionally, check if the underlying YAML parser error is included
	assert.Contains(t, err.Error(), "yaml:", "Error message should contain underlying yaml parser error detail")

	// Ensure it does NOT contain the decoding error message, as reading failed first
	assert.NotContains(t, err.Error(), "error decoding configuration", "Error should be from reading, not decoding")
}
