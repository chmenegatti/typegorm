// cmd/typegorm/migrate_status_test.go
package main

import (
	"bytes" // Para capturar a saída
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert" // Biblioteca de asserção (opcional mas útil)
	"github.com/stretchr/testify/require"
)

// Helper para executar um comando e capturar sua saída
func executeCommand(root *cobra.Command, args ...string) (string, string, error) {
	// Buffers para capturar stdout e stderr
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	// Redireciona a saída do comando
	root.SetOut(stdout)
	root.SetErr(stderr)

	// Define os argumentos para o comando (como se fossem da linha de comando)
	root.SetArgs(args)

	// Cria um novo rootCmd para garantir que flags não persistam entre testes (importante!)
	// Ou reseta as flags no rootCmd original se possível/mais fácil
	testRootCmd := rootCmd // Cuidado: pode precisar recriar ou resetar flags/estado

	// Execute o comando
	err := testRootCmd.Execute()

	return stdout.String(), stderr.String(), err
}

func TestMigrateStatusCommand(t *testing.T) {
	// 1. Setup: Criar um arquivo de config temporário válido
	tempDir := t.TempDir() // Cria um diretório temporário que é limpo após o teste
	tempConfigFile := filepath.Join(tempDir, "temp_config.yaml")
	configContent := `
	database:
		dialect: "sqlite"
		dsn: "file:test.db?cache=shared"
	migration:
		directory: "./test_migrations"
	`
	err := os.WriteFile(tempConfigFile, []byte(configContent), 0644)
	require.NoError(t, err, "Failed to write temp config file") // require falha o teste imediatamente

	// 2. Executar o comando 'migrate status' usando o arquivo de config temporário
	// Note que re-instanciamos rootCmd ou usamos uma função que o faz para isolar testes
	// Aqui, vamos assumir que executeCommand lida com isso ou que resetamos flags
	// Limpamos cfgFile global antes de cada teste ou o definimos via SetArgs/Flags diretamente
	cfgFile = "" // Resetar flag global ou evitar usá-la diretamente nos testes

	stdout, stderr, err := executeCommand(rootCmd, "migrate", "status", "--config", tempConfigFile)

	// 3. Asserts: Verificar a saída e erros
	assert.NoError(t, err, "Command execution failed") // assert permite continuar mesmo se falhar
	assert.Empty(t, stderr, "Stderr should be empty on success")

	// Verificar se a saída contém as mensagens esperadas do placeholder
	assert.Contains(t, stdout, "Running migrate status...", "Output should contain running message")
	assert.Contains(t, stdout, "Placeholder: Checking migration status...", "Output should contain placeholder message")
	assert.Contains(t, stdout, "Dialect (config): sqlite", "Output should reflect config") // Verifica se a config foi lida
	assert.Contains(t, stdout, "Migration status checked (placeholder).", "Output should contain success message")

}

func TestMigrateCreateCommandErrors(t *testing.T) {
	// Teste de erro: faltando argumento
	_, stderr, err := executeCommand(rootCmd, "migrate", "create")

	assert.Error(t, err, "Expected an error for missing argument")
	assert.Contains(t, stderr, `accepts 1 arg(s), received 0`, "Error message should indicate missing argument")

	// Adicionar mais testes de erro (config inválida, etc.)
}
