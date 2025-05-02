// pkg/config/load.go
package config

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// LoadConfig carrega a configuração a partir de arquivos, env vars e defaults.
// configPath: caminho opcional para um arquivo de configuração específico.
// Se configPath for vazio, procura por "typegorm.yaml" nos diretórios padrão.
func LoadConfig(configPath string) (Config, error) {
	v := viper.New()
	cfg := NewDefaultConfig() // Começa com os padrões

	// 1. Definir padrões no Viper (para que possam ser sobrescritos)
	v.SetDefault("database.pool.maxIdleConns", cfg.Database.Pool.MaxIdleConns)
	v.SetDefault("database.pool.maxOpenConns", cfg.Database.Pool.MaxOpenConns)
	v.SetDefault("database.pool.connMaxLifetime", cfg.Database.Pool.ConnMaxLifetime)
	v.SetDefault("logging.level", cfg.Logging.Level)
	v.SetDefault("logging.format", cfg.Logging.Format)
	v.SetDefault("migration.directory", cfg.Migration.Directory)
	// Não definimos padrão para dialect e dsn, pois são obrigatórios

	// 2. Configurar leitura de Variáveis de Ambiente
	v.SetEnvPrefix("TYPEGORM") // Ex: TYPEGORM_DATABASE_DIALECT=mysql
	v.AutomaticEnv()
	// Permite que TYPEGORM_DATABASE_DSN sobrescreva database.dsn
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 3. Configurar leitura do Arquivo de Configuração
	if configPath != "" {
		// Usar arquivo de configuração específico fornecido via flag/param
		v.SetConfigFile(configPath)
	} else {
		// Procurar por arquivo de configuração nos diretórios padrão
		v.SetConfigName("typegorm")        // Nome do arquivo (sem extensão)
		v.SetConfigType("yaml")            // Extensão/tipo do arquivo
		v.AddConfigPath(".")               // Procurar no diretório atual
		v.AddConfigPath("$HOME/.typegorm") // Procurar em ~/.typegorm/
		// Poderia adicionar /etc/typegorm/ também, se apropriado
	}

	// Tentar ler o arquivo de configuração
	if err := v.ReadInConfig(); err != nil {
		// Erro só é fatal se não for "arquivo não encontrado" E um configPath específico foi dado
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok || configPath != "" {
			return cfg, fmt.Errorf("erro ao ler arquivo de configuração: %w", err)
		}
		// Se o arquivo não foi encontrado, mas não era obrigatório, tudo bem.
		// As configurações virão dos defaults ou env vars.
		fmt.Println("Arquivo de configuração não encontrado, usando defaults/env vars.") // Logar isso adequadamente depois
	}

	// 4. Fazer o Unmarshal das configurações lidas para a struct Config
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("erro ao fazer unmarshal da configuração: %w", err)
	}

	// 5. Validar a struct de configuração
	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		// Transforma erros de validação em algo mais legível
		var validationErrors []string
		for _, err := range err.(validator.ValidationErrors) {
			validationErrors = append(validationErrors, fmt.Sprintf("Campo '%s' falhou na validação '%s'", err.Namespace(), err.Tag()))
		}
		return cfg, fmt.Errorf("configuração inválida: %s", strings.Join(validationErrors, "; "))
	}

	// 6. Retornar a configuração carregada e validada
	return cfg, nil
}
