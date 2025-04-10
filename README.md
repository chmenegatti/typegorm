# ✨ TypeGorm ✨ - Um ORM Brasileiro para Go 🇧🇷

[![Status](https://img.shields.io/badge/status-em--desenvolvimento-yellow)](https://github.com/chmenegatti/typegorm)
[![Go Reference](https://pkg.go.dev/badge/github.com/chmenegatti/typegorm.svg)](https://pkg.go.dev/github.com/chmenegatti/typegorm)
<!-- Adicionar outros badges depois (Build, Coverage, etc.) -->

**Simplificando a interação com bancos de dados em Go, com um toque brasileiro!**

---

## 🚀 O que é o TypeGorm?

O TypeGorm é um framework ORM (Object-Relational Mapper) e ODM (Object-Document Mapper) para a linguagem Go, **atualmente em desenvolvimento ativo**. Nosso objetivo é fornecer uma camada de abstração poderosa e fácil de usar para interagir com diversos bancos de dados, tanto SQL quanto NoSQL.

A inspiração vem do popular [TypeORM](https://typeorm.io/) do mundo TypeScript/JavaScript, buscando trazer uma experiência de desenvolvimento similar, focada na produtividade e na clareza, para o ecossistema Go.

**Status Atual:** A base do projeto está estabelecida, permitindo conexões e execução de comandos SQL básicos para os drivers suportados através da interface `DataSource`. As funcionalidades de ORM mais avançadas (mapeamento de modelos, CRUD automático, relações, migrations, query builder) estão planejadas para as próximas fases.

## 🎯 Objetivos

* **Simplicidade:** Reduzir o código boilerplate necessário para operações comuns de banco de dados.
* **Produtividade:** Permitir que desenvolvedores foquem na lógica de negócios, não nos detalhes de SQL ou APIs de drivers.
* **Flexibilidade:** Suporte a múltiplos bancos de dados (SQL e NoSQL) através de uma API consistente.
* **Segurança:** Prevenção de SQL Injection através do uso implícito de queries parametrizadas.
* **Tipagem Forte:** Aproveitar o sistema de tipos do Go sempre que possível.
* **Comunidade Brasileira:** Fomentar o uso e a contribuição da comunidade de desenvolvedores Go no Brasil.

## ✨ Funcionalidades

### Implementadas ✅

* **Gerenciamento de Conexão Unificado:**
    * API `typegorm.Connect` para conectar a diferentes bancos.
    * Sistema de registro de drivers.
* **Interface `DataSource`:**
    * Abstração para interação básica com o banco.
    * Métodos `Connect`, `Close`, `Ping`.
    * Métodos para execução direta de SQL: `ExecContext`, `QueryContext`, `QueryRowContext`.
    * Métodos para transações: `BeginTx`.
    * Métodos para prepared statements: `PrepareContext`.
* **Drivers Suportados:**
    * SQLite (`github.com/mattn/go-sqlite3`)
    * PostgreSQL (`github.com/jackc/pgx/v5/stdlib`)
    * MySQL / MariaDB (`github.com/go-sql-driver/mysql`)

### Planejadas 🔧

* **Mapeamento Objeto-Relacional/Documento:**
    * Definição de entidades via Structs Go e Tags (`typegorm:"..."`).
    * Geração automática de esquema (opcional).
    * Suporte a tipos customizados.
* **Operações CRUD:** Métodos `Save`, `Find`, `FindOne`, `Delete`, etc. baseados em entidades.
* **Relações:** Suporte a `OneToOne`, `OneToMany`, `ManyToOne`, `ManyToMany`.
* **Query Builder Fluente:** API para construir consultas complexas de forma programática e segura.
* **Migrations:** Ferramentas para gerenciar a evolução do schema do banco de dados.
* **Drivers Adicionais:**
    * SQL Server (`github.com/microsoft/go-mssqldb`)
    * MongoDB (`go.mongodb.org/mongo-driver`)
    * Redis (`github.com/go-redis/redis`)
    * Oracle (`github.com/godror/godror`)
* **Listeners/Subscribers:** Hooks para eventos do ciclo de vida das entidades.
* **Caching:** Estratégias para cache de consultas.
* **Soft Delete:** Suporte integrado para exclusão lógica.

## ⚙️ Instalação

Para adicionar o TypeGorm ao seu projeto Go:

```bash
go get github.com/chmenegatti/typegorm
```

Você também precisará importar os pacotes dos drivers específicos que pretende usar, utilizando o identificador branco (_), para que eles possam se registrar durante a inicialização.


```bash
go get github.com/mattn/go-sqlite3        # Exemplo para SQLite
go get github.com/jackc/pgx/v5/stdlib      # Exemplo para PostgreSQL (via pgx)
go get github.com/go-sql-driver/mysql    # Exemplo para MySQL/MariaDB
```

🏁 Começando a Usar (Exemplos Atuais com DataSource)
O uso atual foca na obtenção de uma DataSource e na execução de operações básicas de banco de dados através de seus métodos.

```go
package main

import (
	"context"
	"database/sql" // Para sql.ErrNoRows e sql.TxOptions
	"errors"
	"fmt"
	"log"
	"time"

	// 1. Importa o pacote raiz do TypeGorm
	"github.com/chmenegatti/typegorm"

	// 2. Importa as CONFIGURAÇÕES do driver desejado
	"github.com/chmenegatti/typegorm/driver/postgres"
	// "github.com/chmenegatti/typegorm/driver/mysql"
	// "github.com/chmenegatti/typegorm/driver/sqlite"

	// 3. Importa os DRIVERS com '_' para registrar (efeito colateral do init())
	_ "github.com/chmenegatti/typegorm/driver/postgres" // Para Postgres
	// _ "github.com/chmenegatti/typegorm/driver/mysql"    // Para MySQL
	// _ "github.com/chmenegatti/typegorm/driver/sqlite"   // Para SQLite
)

func main() {
	fmt.Println("🚀 Iniciando exemplo TypeGorm...")

	// --- Configuração da Conexão (Exemplo com PostgreSQL) ---
	// Use a struct de Config do pacote do driver específico
	pgConfig := postgres.Config{
		Host:     "localhost", // Ou leia de env vars
		Port:     5432,
		Username: "postgres",
		Password: "password",
		Database: "testdb",
		SSLMode:  "disable",
	}

	// --- Conexão ---
	fmt.Println("Conectando ao banco de dados...")
	// Use typegorm.Connect passando a struct de configuração
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second) // Contexto com timeout para conexão/ping inicial
	defer cancel()

	dataSource, err := typegorm.Connect(pgConfig)
	if err != nil {
		log.Fatalf("Falha ao conectar: %v", err)
	}
	fmt.Println("✅ Conectado com sucesso!")

	// Garante que a conexão seja fechada ao final
	defer func() {
		fmt.Println("Fechando conexão...")
		if err := dataSource.Close(); err != nil {
			log.Printf("⚠️ Erro ao fechar conexão: %v", err)
		} else {
			fmt.Println("🔌 Conexão fechada.")
		}
	}()

	// --- Exemplo 1: Ping ---
	fmt.Println("Pingando o banco...")
	if err := dataSource.Ping(ctx); err != nil {
		log.Fatalf("Falha no Ping: %v", err)
	}
	fmt.Println("✅ Ping bem-sucedido!")

	// --- Exemplo 2: ExecContext (Criar Tabela e Inserir) ---
	fmt.Println("Executando ExecContext...")
	// Placeholders ($1, $2 no PG; ? no MySQL/SQLite) são gerenciados pelo driver subjacente
	_, err = dataSource.ExecContext(ctx, `DROP TABLE IF EXISTS exemplo_typegorm;`) // Limpeza
	if err != nil { log.Printf("⚠️ Aviso ao dropar tabela (pode não existir): %v", err) }

	createSQL := `CREATE TABLE exemplo_typegorm (id SERIAL PRIMARY KEY, nome TEXT, valor INT);` // PG syntax
	// createSQL := `CREATE TABLE exemplo_typegorm (id INT AUTO_INCREMENT PRIMARY KEY, nome TEXT, valor INT);` // MySQL syntax
	_, err = dataSource.ExecContext(ctx, createSQL)
	if err != nil {
		log.Fatalf("Falha no ExecContext (CREATE): %v", err)
	}

	insertSQL := `INSERT INTO exemplo_typegorm (nome, valor) VALUES ($1, $2), ($3, $4);` // PG syntax
	// insertSQL := `INSERT INTO exemplo_typegorm (nome, valor) VALUES (?, ?), (?, ?);` // MySQL/SQLite syntax
	result, err := dataSource.ExecContext(ctx, insertSQL, "Item A", 100, "Item B", 200)
	if err != nil {
		log.Fatalf("Falha no ExecContext (INSERT): %v", err)
	}
	rowsAffected, _ := result.RowsAffected()
	fmt.Printf("✅ ExecContext (CREATE/INSERT) bem-sucedido. Linhas afetadas no INSERT: %d\n", rowsAffected)

	// --- Exemplo 3: QueryContext (Selecionar Múltiplas Linhas) ---
	fmt.Println("Executando QueryContext...")
	querySQL := `SELECT id, nome, valor FROM exemplo_typegorm WHERE valor >= $1 ORDER BY id;` // PG syntax
	// querySQL := `SELECT id, nome, valor FROM exemplo_typegorm WHERE valor >= ? ORDER BY id;` // MySQL/SQLite syntax
	rows, err := dataSource.QueryContext(ctx, querySQL, 150)
	if err != nil {
		log.Fatalf("Falha no QueryContext: %v", err)
	}
	defer rows.Close() // Muito importante fechar rows!

	fmt.Println("Resultados do QueryContext:")
	for rows.Next() {
		var id int
		var nome string
		var valor int
		if err := rows.Scan(&id, &nome, &valor); err != nil {
			log.Printf("⚠️ Erro no Scan: %v", err)
			continue
		}
		fmt.Printf("  - ID: %d, Nome: %s, Valor: %d\n", id, nome, valor)
	}
	if err := rows.Err(); err != nil { // Verifica erro após o loop
		log.Printf("⚠️ Erro durante iteração de rows: %v", err)
	}
	fmt.Println("✅ QueryContext finalizado.")

	// --- Exemplo 4: QueryRowContext (Selecionar Uma Linha) ---
	fmt.Println("Executando QueryRowContext...")
	var nomeItemA string
	queryRowSQL := `SELECT nome FROM exemplo_typegorm WHERE id = $1;` // PG syntax
	// queryRowSQL := `SELECT nome FROM exemplo_typegorm WHERE id = ?;` // MySQL/SQLite syntax
	row := dataSource.QueryRowContext(ctx, queryRowSQL, 1) // Busca ID 1
	err = row.Scan(&nomeItemA)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Println("QueryRowContext: Nenhum registro encontrado (ErrNoRows).")
		} else {
			log.Printf("⚠️ Falha no QueryRowContext/Scan: %v", err)
		}
	} else {
		fmt.Printf("✅ QueryRowContext bem-sucedido. Nome do Item 1: %s\n", nomeItemA)
	}

	// --- Exemplo 5: Transação (BeginTx) ---
	fmt.Println("Executando Transação...")
	tx, err := dataSource.BeginTx(ctx, nil) // Inicia transação com opções padrão
	if err != nil {
		log.Fatalf("Falha ao iniciar transação (BeginTx): %v", err)
	}

	// Defer Rollback para garantir que seja chamado em caso de erro/panic
	txFinalizado := false // Flag para controlar o defer
	defer func() {
		if !txFinalizado && tx != nil { // Só faz rollback se não foi commitado/revertido explicitamente
			fmt.Println("Defer: Tentando Rollback da transação não finalizada...")
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				log.Printf("⚠️ Erro no Rollback do defer: %v", rbErr)
			}
		}
	}()

	// Operações dentro da transação
	updateSQL := `UPDATE exemplo_typegorm SET valor = valor + 1 WHERE id = $1;` // PG syntax
	// updateSQL := `UPDATE exemplo_typegorm SET valor = valor + 1 WHERE id = ?;` // MySQL/SQLite syntax
	_, err = tx.ExecContext(ctx, updateSQL, 1) // Adiciona 1 ao valor do Item A
	if err != nil {
		log.Printf("Erro na transação (UPDATE): %v. Revertendo...", err)
		tx.Rollback() // Reverte explicitamente
		txFinalizado = true
		return // Aborta a função main neste exemplo simples
	}

	// Se tudo deu certo, faz commit
	fmt.Println("Commitando transação...")
	if err = tx.Commit(); err != nil {
		txFinalizado = true // Mesmo falhando no commit, a tx está "finalizada"
		log.Fatalf("Falha ao commitar transação: %v", err)
	}
	txFinalizado = true // Marca como finalizada com sucesso
	fmt.Println("✅ Transação commitada com sucesso!")


	fmt.Println("🎉 Exemplo TypeGorm finalizado.")
}

```

## 💾 Bancos de Dados Suportados
Atualmente, o TypeGorm suporta os seguintes bancos de dados, com drivers específicos para cada um. A tabela abaixo resume o status de implementação de cada driver:
| Banco de Dados	| Driver Go Usado	| Status |
|------------------|------------------|----------------|
SQLite	| mattn/go-sqlite3	| ✅ Implementado |
PostgreSQL |	jackc/pgx/v5/stdlib	| ✅ Implementado |
MySQL/MariaDB	| go-sql-driver/mysql |	✅ Implementado |
SQL Server	| microsoft/go-mssqldb |	🔧 Planejado |
MongoDB	| go.mongodb.org/mongo-driver |	🔧 Planejado |
Redis |	go-redis/redis |	🔧 Planejado |
Oracle |	godror/godror |	🔧 Planejado |

## 🤝 Contribuição
Contribuições são muito bem-vindas! Como o projeto está no início, há muitas oportunidades para ajudar. Sinta-se à vontade para abrir Issues para bugs ou sugestões de funcionalidades, ou Pull Requests com melhorias.

**Comunicação**: Português é preferencial para Issues e discussões, mas Inglês também é aceito.

**Código**: Comentários de código devem ser em Português Brasileiro.

**Documentação**: A documentação deve ser escrita em Português Brasileiro, com Inglês como opção secundária.

**Estilo de Código**: Siga as convenções de estilo do Go. Use `go fmt` para formatar o código antes de enviar um Pull Request.

**Testes**: Adicione testes para novas funcionalidades ou correções de bugs.


## 📜 Licença
Este projeto é licenciado sob a Licença MIT. <!-- Você precisará criar um arquivo LICENSE com o texto da licença MIT -->