# TypeGorm: Framework ORM para Go Inspirado no TypeORM

## 1. Introdução

### Objetivo do Framework
O **TypeGorm** visa simplificar e padronizar a interação com bancos de dados relacionais e NoSQL em aplicações Go. O objetivo principal é abstrair a complexidade das operações de banco de dados, permitindo que os desenvolvedores se concentrem na lógica de negócios, ao mesmo tempo que oferece flexibilidade e poder para lidar com cenários complexos.

### Inspiração no TypeORM
TypeGorm se inspira fortemente no [TypeORM](https://typeorm.io/) do ecossistema TypeScript/JavaScript. As características principais que buscamos emular incluem:
* **Mapeamento Objeto-Relacional/Documento:** Definição de modelos de dados usando structs Go e metadados (via struct tags) para mapear para tabelas/coleções.
* **Suporte a Múltiplos Bancos de Dados:** Flexibilidade para trabalhar com diferentes SGBDs (SQL e NoSQL) através de uma API unificada.
* **Padrões Repository e Active Record (Opcional):** Oferecer diferentes maneiras de interagir com os dados.
* **Query Builder Robusto:** Uma interface fluente para construir consultas SQL/NoSQL complexas de forma programática e segura.
* **Gerenciamento de Relações:** Suporte explícito para relações OneToOne, OneToMany, ManyToOne e ManyToMany.
* **Migrations:** Ferramentas para gerenciar a evolução do esquema do banco de dados.
* **Transactions:** API para executar múltiplas operações atomicamente.

### Importância de um ORM
Em Go, embora o pacote `database/sql` forneça uma base sólida, ele exige muito código repetitivo para operações CRUD, mapeamento de resultados e gerenciamento de conexões. Um ORM como o TypeGorm aumenta a produtividade ao:
* **Reduzir Boilerplate:** Automatiza tarefas comuns de acesso a dados.
* **Garantir Consistência:** Aplica padrões consistentes em toda a aplicação.
* **Melhorar a Manutenibilidade:** Facilita a refatoração e a evolução do código de acesso a dados.
* **Abstrair Diferenças de Banco de Dados:** Permite (até certo ponto) trocar de banco de dados com menos esforço.
* **Prevenir Erros Comuns:** Ajuda a evitar vulnerabilidades como SQL Injection através de queries parametrizadas.

## 2. Estrutura do Projeto

O código do TypeGorm será organizado em pacotes modulares para clareza e manutenibilidade. Uma estrutura de diretórios sugerida seria:

```
typegorm/
├── connection/       # Gerenciamento de conexões e pooling
│   ├── manager.go
│   └── pool.go
├── dialect/          # Lógica específica de cada banco de dados (SQL variants, query syntax)
│   ├── mysql.go
│   ├── postgres.go
│   ├── sqlite.go
│   ├── oracle.go
│   └── common.go     # Interfaces e lógicas comuns
├── driver/           # Adapters para os drivers de banco de dados Go
│   ├── mysql/
│   ├── postgres/
│   ├── mongo/
│   ├── oracle/
│   ├── sqlite/
│   └── redis/
├── entity/           # Reflexão, parsing de struct tags, metadados de entidades
│   ├── metadata.go
│   └── parser.go
├── migration/        # Ferramentas e execução de migrações
│   ├── cli/          # (Opcional) Ferramenta de linha de comando
│   ├── migration.go
│   └── runner.go
├── querybuilder/     # Implementação do Query Builder fluente
│   ├── builder.go
│   ├── expression.go # Representação de expressões (WHERE, JOIN, etc.)
│   └── result.go     # Processamento de resultados
├── repository/       # Implementação do padrão Repository (Data Mapper)
│   └── repository.go
├── schema/           # Construção e sincronização de esquemas
│   ├── builder.go
│   └── sync.go
├── transaction/      # Gerenciamento de transações
│   └── transaction.go
├── typegorm.go       # Ponto de entrada principal da API
├── error.go          # Definições de erros customizados
└── go.mod
└── go.sum
```

### Arquitetura Geral
A arquitetura se baseará em:
1.  **Camada de Conexão:** Abstrai a configuração e o gerenciamento de conexões para diferentes bancos.
2.  **Dialetos/Drivers:** Adapta a sintaxe e o comportamento específico de cada banco de dados. Usa drivers Go subjacentes (ex: `go-sql-driver/mysql`, `pgx`, `mongo-go-driver`, `godror`, `go-sqlite3`, `go-redis/redis`).
3.  **Metadados de Entidade:** Usa reflexão e struct tags para entender como mapear structs Go para o banco de dados.
4.  **Executor de Consultas:** Responsável por traduzir operações (CRUD, Query Builder) em comandos SQL/NoSQL e executá-los.
5.  **Query Builder:** Oferece uma API fluente para construir consultas complexas.
6.  **Gerenciador de Migração:** Compara o estado do modelo com o esquema do banco e gera/aplica migrações.

## 3. Conexão com Bancos de Dados

O TypeGorm fornecerá uma API unificada para configurar conexões.

```go
package main

import (
    "log"
    "github.com/your-repo/typegorm" // Caminho hipotético
    "github.com/your-repo/typegorm/driver/mysql"
    "github.com/your-repo/typegorm/driver/postgres"
    "github.com/your-repo/typegorm/driver/mongo"
    "github.com/your-repo/typegorm/driver/oracle"
    "github.com/your-repo/typegorm/driver/sqlite"
    "github.com/your-repo/typegorm/driver/redis"
)

func main() {
    // Exemplo MySQL/MariaDB
    mysqlConn, err := typegorm.Connect(mysql.Config{
        Host:     "localhost",
        Port:     3306,
        Username: "user",
        Password: "password",
        Database: "my_db",
        Options: map[string]string{
            "parseTime": "true",
        },
    })
    if err != nil { log.Fatal(err) }
    defer mysqlConn.Close()

    // Exemplo PostgreSQL
    pgConn, err := typegorm.Connect(postgres.Config{
        Host:     "localhost",
        Port:     5432,
        Username: "user",
        Password: "password",
        Database: "my_db",
        SSLMode:  "disable",
    })
    // ...

    // Exemplo MongoDB
    mongoConn, err := typegorm.Connect(mongo.Config{
        URI: "mongodb://user:password@localhost:27017/my_db?authSource=admin",
    })
    // ...

     // Exemplo Oracle
    oracleConn, err := typegorm.Connect(oracle.Config{
         Username: "user",
         Password: "password",
         ConnectString: "localhost:1521/ORCL", // Exemplo de connect string
     })
    // ...

    // Exemplo SQLite
    sqliteConn, err := typegorm.Connect(sqlite.Config{
        Database: "./my_app.db",
    })
    // ...

    // Exemplo Redis
    redisConn, err := typegorm.Connect(redis.Config{
        Addr:     "localhost:6379",
        Password: "", // Sem senha
        DB:       0,  // Banco de dados padrão
    })
    // ...


    // Gerenciamento de Conexões e Pooling
    // Para bancos SQL, o TypeGorm utilizará o pooling de conexões
    // integrado do `database/sql`. Para NoSQL (Mongo, Redis),
    // os drivers subjacentes geralmente gerenciam seus próprios pools.
    // A configuração pode permitir ajustar tamanhos de pool (MaxOpenConns, MaxIdleConns).
}
```

### Gerenciamento de Conexões
* A função `typegorm.Connect` retorna uma interface `DataSource` ou similar.
* Implementa pooling (especialmente para SQL DBs usando `database/sql.DB`).
* Permite configurar timeouts, número máximo de conexões, etc.
* Gerencia a lógica de reconexão (opcionalmente).

## 4. Operações CRUD

O TypeGorm oferecerá interfaces para operações básicas, provavelmente através de um `Repository` ou diretamente via `DataSource`.

```go
package main

import (
    "context"
    "time"
    "github.com/your-repo/typegorm"
)

type User struct {
    ID        uint      `typegorm:"primaryKey;autoIncrement"`
    Name      string    `typegorm:"column:user_name;unique"`
    Email     string    `typegorm:"unique"`
    CreatedAt time.Time `typegorm:"createdAt"`
    UpdatedAt time.Time `typegorm:"updatedAt"`
}

func main() {
    db := /* obter DataSource de typegorm.Connect */
    userRepo := typegorm.GetRepository[User](db) // Usando Generics (Go 1.18+)

    ctx := context.Background()

    // CREATE
    newUser := User{Name: "Alice", Email: "alice@example.com"}
    err := userRepo.Save(ctx, &newUser) // Save pode fazer Insert ou Update
    // Ou: err := userRepo.Insert(ctx, &newUser)
    if err != nil { /* handle error */ }
    // newUser.ID agora está preenchido (se autoIncrement)

    // READ (Find One)
    foundUser, err := userRepo.FindOne(ctx, typegorm.Where{"Email = ?": "alice@example.com"})
    if err != nil { /* handle error */ }
    log.Printf("Found: %+v\n", foundUser)

    // READ (Find Multiple)
    allUsers, err := userRepo.Find(ctx, typegorm.FindOptions{
        Where: typegorm.Where{"Name LIKE ?": "A%"},
        Order: "CreatedAt DESC",
        Limit: 10,
        Offset: 0,
    })
    if err != nil { /* handle error */ }

    // UPDATE
    foundUser.Name = "Alice Smith"
    err = userRepo.Save(ctx, foundUser) // Save detecta a chave primária e faz Update
    // Ou: result, err := userRepo.Update(ctx, typegorm.Where{"ID = ?": foundUser.ID}, map[string]interface{}{"Name": "Alice Smith"})
    if err != nil { /* handle error */ }

    // DELETE
    err = userRepo.Delete(ctx, foundUser) // Deleta pelo objeto (usando PK)
    // Ou: result, err := userRepo.Delete(ctx, typegorm.Where{"Email = ?": "alice@example.com"})
    if err != nil { /* handle error */ }
}
```

## 5. Suporte a Comandos SQL/Query Builder

### Query Builder
Uma API fluente para construir consultas complexas de forma segura e legível.

```go
package main

import (
    "context"
    "github.com/your-repo/typegorm"
)

type Post struct {
    ID      uint   `typegorm:"primaryKey"`
    Title   string
    UserID  uint
    User    User `typegorm:"relation:many-to-one;joinColumn:user_id"` // Relação
}
// User struct definida como antes...

func main() {
    db := /* obter DataSource */
    ctx := context.Background()
    var results []struct { // Resultado customizado
        UserName string `db:"user_name"`
        PostCount int   `db:"post_count"`
    }

    // Exemplo de Query Builder
    qb := typegorm.GetQueryBuilder[User](db) // Ou db.QueryBuilder()

    err := qb.Select("u.user_name", "COUNT(p.id) as post_count").
        From("users", "u"). // Alias 'u'
        InnerJoin("posts", "p", "u.id = p.user_id"). // InnerJoin(tabela, alias, condição)
        Where("u.user_name LIKE ?", "A%").
        GroupBy("u.id", "u.user_name").
        Having("COUNT(p.id) > ?", 1).
        OrderBy("post_count", "DESC").
        Limit(10).
        Offset(0).
        Scan(ctx, &results) // Executa a query e mapeia para a struct

    if err != nil { /* handle error */ }
    log.Printf("Query Results: %+v\n", results)

    // Query parametrizada é implícita no Where, Having, etc.
    // Valores são passados separadamente para o driver, prevenindo SQL Injection.
}
```

### Transactions
Suporte para agrupar operações atomicamente.

```go
package main

import (
    "context"
    "errors"
    "github.com/your-repo/typegorm"
)
// User, Post structs ...

func main() {
    db := /* obter DataSource */
    ctx := context.Background()
    userRepo := typegorm.GetRepository[User](db)
    postRepo := typegorm.GetRepository[Post](db)

    err := db.Transaction(ctx, func(tx typegorm.TransactionManager) error {
        // Obter repositórios que operam dentro da transação
        txUserRepo := typegorm.GetRepository[User](tx)
        txPostRepo := typegorm.GetRepository[Post](tx)

        // Operação 1
        newUser := User{Name: "Bob", Email: "bob@example.com"}
        if err := txUserRepo.Save(ctx, &newUser); err != nil {
            return err // Retornar erro causa rollback
        }

        // Operação 2
        newPost := Post{Title: "Bob's First Post", UserID: newUser.ID}
        if err := txPostRepo.Save(ctx, &newPost); err != nil {
            return err // Rollback
        }

        // Simular um erro para causar rollback
        // if true { return errors.New("simulated error during transaction") }

        return nil // Retornar nil causa commit
    })

    if err != nil {
        log.Printf("Transaction failed: %v", err) // Transação sofreu rollback
    } else {
        log.Println("Transaction successful!") // Transação commitada
    }
}
```

## 6. Mapeamento e Models

A definição de modelos usa structs Go e struct tags `typegorm`.

```go
package model

import (
    "time"
    "github.com/your-repo/typegorm/types" // Pacote para tipos customizados (ex: JSON)
)

type Profile struct {
    ID     uint   `typegorm:"primaryKey"`
    Bio    string `typegorm:"type:text"`
    UserID uint   // Chave estrangeira implícita ou explícita
}

type User struct {
    ID        uint      `typegorm:"primaryKey;autoIncrement"` // Chave primária, auto-incremento
    Name      string    `typegorm:"column:full_name;size:100;not null"` // Nome da coluna, tamanho, não nulo
    Email     string    `typegorm:"unique;index:user_email_idx"` // Índice único e nomeado
    Age       int       `typegorm:"default:18"` // Valor padrão
    IsActive  bool      `typegorm:"default:true"`
    Metadata  types.JSON `typegorm:"type:jsonb"` // Suporte a JSON/JSONB (depende do DB)
    CreatedAt time.Time `typegorm:"createdAt"` // Timestamp de criação automático
    UpdatedAt time.Time `typegorm:"updatedAt"` // Timestamp de atualização automático

    // Relações
    Profile   *Profile `typegorm:"relation:one-to-one;joinColumn:profile_id;reference:id"` // Relação 1:1
    Posts     []*Post  `typegorm:"relation:one-to-many;mappedBy:User"` // Relação 1:N (lado "um")
    Groups    []*Group `typegorm:"relation:many-to-many;joinTable:user_groups"` // Relação N:N
}

type Post struct {
    ID        uint      `typegorm:"primaryKey"`
    Title     string
    Content   string    `typegorm:"type:text"`
    UserID    uint      // Chave estrangeira para User
    User      *User     `typegorm:"relation:many-to-one;joinColumn:user_id"` // Relação N:1 (lado "muitos")
    CreatedAt time.Time `typegorm:"createdAt"`
}

type Group struct {
    ID    uint   `typegorm:"primaryKey"`
    Name  string `typegorm:"unique"`
    Users []*User `typegorm:"relation:many-to-many;mappedBy:Groups"` // Lado inverso do N:N
}

// Tabela de junção para User <-> Group (pode ser gerada automaticamente)
// type UserGroups struct {
//     UserID  uint `typegorm:"primaryKey"`
//     GroupID uint `typegorm:"primaryKey"`
// }
```

### Tags Comuns:
* `primaryKey`: Marca o campo como chave primária.
* `autoIncrement`: Indica que a chave primária é auto-incrementada pelo banco.
* `column:<nome>`: Especifica o nome da coluna no banco.
* `type:<tipo>`: Especifica o tipo de dado no banco (ex: `varchar(100)`, `text`, `jsonb`, `timestamp`).
* `size:<tamanho>`: Para tipos como `varchar`.
* `unique`: Define uma restrição UNIQUE.
* `index` / `index:<nome>`: Cria um índice simples ou nomeado.
* `not null`: Define uma restrição NOT NULL.
* `default:<valor>`: Define um valor padrão para a coluna.
* `createdAt`, `updatedAt`: Campos especiais para timestamps automáticos.
* `relation:<tipo>`: Define o tipo de relação (`one-to-one`, `one-to-many`, `many-to-one`, `many-to-many`).
* `joinColumn:<fk_coluna>`: Especifica a coluna da chave estrangeira (em relações *ToOne).
* `joinTable:<tabela_juncao>`: Especifica a tabela de junção (em relações ManyToMany).
* `mappedBy:<campo_remoto>`: Indica o campo na entidade relacionada que mapeia de volta (em OneToMany, ManyToMany inverso).
* `reference:<col_ref>`: Coluna referenciada pela FK (geralmente a PK remota).
* `onDelete:<acao>`, `onUpdate:<acao>`: Define ações referenciais (CASCADE, SET NULL, etc.).

## 7. Ferramentas de Migração

Um componente crucial para gerenciar mudanças no esquema do banco de dados.

### Funcionalidades:
1.  **Geração de Migração:**
    * Comando CLI: `typegorm migration:generate -n CreateUserTable`
    * Compara as entidades Go definidas com o estado atual do banco de dados (ou a última migração).
    * Gera um arquivo de migração (ex: `migrations/1678886400_CreateUserTable.go`).
    * O arquivo contém funções `Up()` e `Down()` com código Go para executar SQL DDL (ou usando um Schema Builder do ORM).
    ```go
    // migrations/1678886400_CreateUserTable.go
    package migrations

    import "github.com/your-repo/typegorm/migration"

    func init() {
        migration.Register(1678886400, "CreateUserTable", Up_1678886400, Down_1678886400)
    }

    func Up_1678886400(runner migration.Runner) error {
        return runner.Exec(`
            CREATE TABLE users (
                id INT AUTO_INCREMENT PRIMARY KEY,
                full_name VARCHAR(100) NOT NULL,
                email VARCHAR(255) UNIQUE,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
            );
        `)
        // Ou usando um Schema Builder:
        // return runner.SchemaBuilder().CreateTable("users", func(table schema.Table) {
        //     table.Increments("id").Primary()
        //     table.String("full_name", 100).NotNull()
        //     table.String("email").Unique()
        //     table.Timestamps()
        // })
    }

    func Down_1678886400(runner migration.Runner) error {
        return runner.Exec(`DROP TABLE users;`)
        // Ou: return runner.SchemaBuilder().DropTable("users")
    }
    ```
2.  **Execução de Migração:**
    * Comando CLI: `typegorm migration:run` (aplica todas as pendentes) ou `typegorm migration:up`
    * Executa as funções `Up()` das migrações pendentes em ordem.
    * Registra as migrações executadas em uma tabela especial no banco (ex: `typegorm_migrations`).
3.  **Reversão de Migração:**
    * Comando CLI: `typegorm migration:revert` (reverte a última) ou `typegorm migration:down`
    * Executa a função `Down()` da última migração aplicada.
    * Remove o registro da migração da tabela de controle.

## 8. Considerações de Desempenho

* **Consultas Otimizadas:** O Query Builder deve gerar SQL eficiente. Evitar `SELECT *` por padrão; permitir seleção explícita de colunas.
* **Indexação:** Facilitar a definição de índices via struct tags. A ferramenta de migração deve criar/remover índices.
* **Lazy Loading vs Eager Loading:**
    * **Lazy Loading:** Relações não são carregadas até serem acessadas explicitamente. Requer consultas adicionais (problema N+1 se não for cuidadoso).
    * **Eager Loading:** Relações são carregadas junto com a entidade principal usando JOINs. Configurar via Query Builder ou tags.
    * TypeGorm deve suportar ambos os modos, com Eager Loading sendo preferível para evitar N+1 quando os dados relacionados são sempre necessários.
* **Caching:** Possibilidade de adicionar uma camada de cache (ex: Redis) para consultas frequentes ou entidades. Isso pode ser um módulo separado ou integrado.
* **Batching:** Suporte para operações em lote (Bulk Insert/Update/Delete) para melhor performance em grandes volumes de dados.
* **Pooling de Conexões:** Configuração adequada do pool de conexões é vital.
* **Benchmarking:** O projeto deve incluir benchmarks (`testing` B*) para operações chave (CRUD, queries complexas) em diferentes bancos de dados suportados. Ferramentas de profiling (pprof) devem ser usadas para identificar gargalos.

## 9. Documentação e Exemplos

* **Documentação `godoc`:** Todos os pacotes e funções públicas devem ter comentários claros e completos seguindo o padrão `godoc`.
* **Website de Documentação:** (Recomendado) Um site dedicado (usando Hugo, MkDocs, Docusaurus) com:
    * Guia de Início Rápido.
    * Tutoriais detalhados (configuração, CRUD, relações, query builder, migrações).
    * Referência completa da API (tags, funções, interfaces).
    * Explicação de conceitos (Lazy/Eager Loading, Transactions).
    * Guias específicos por banco de dados.
* **Repositório de Exemplos:** Uma pasta `/examples` no repositório com projetos Go pequenos e funcionais demonstrando o uso do TypeGorm em diferentes cenários.

## 10. Considerações Finais

### Benefícios do TypeGorm
* **Produtividade Acelerada:** Reduz drasticamente o código necessário para interagir com bancos de dados em Go.
* **Código Mais Legível e Manutenível:** Abstrai SQL e lógica de mapeamento.
* **Segurança:** Prevenção de SQL Injection através de parametrização automática.
* **Flexibilidade:** Suporte a múltiplos bancos de dados SQL e NoSQL populares.
* **Ecossistema Familiar:** A inspiração no TypeORM pode facilitar a adoção por desenvolvedores vindos do mundo Node.js/TypeScript.
* **Tipagem Forte:** Aproveita o sistema de tipos do Go para segurança em tempo de compilação (embora a reflexão introduza alguma dinâmica).

### Visão para Futuras Melhorias
* **Suporte a Mais Bancos de Dados:** Adicionar suporte para SQL Server, CockroachDB, etc.
* **Melhorias no Query Builder:** Funções de janela, CTEs (Common Table Expressions).
* **Eventos/Hooks:** Permitir interceptar operações (BeforeInsert, AfterUpdate, etc.).
* **Soft Deletes:** Suporte nativo para exclusão lógica (marcar como deletado em vez de remover).
* **Replicação Read/Write:** Suporte para configurar conexões separadas para leitura e escrita.
* **Integração com Observabilidade:** Métricas (Prometheus), Tracing (OpenTelemetry).
* **Schema Synchronization (Opcional):** Ferramenta para sincronizar automaticamente o schema do banco com os modelos (útil em desenvolvimento, mas perigoso em produção).
* **Melhorias na Geração de Migração:** Geração de SQL mais inteligente e detecção de renomeações.

---

## Extras

### Links Úteis:
* **Go Official Documentation:** [https://go.dev/doc/](https://go.dev/doc/)
* **Go `database/sql` Tutorial:** [http://go-database-sql.org/](http://go-database-sql.org/)
* **TypeORM Documentation:** [https://typeorm.io/](https://typeorm.io/)
* **Drivers Go:**
    * MySQL: [https://github.com/go-sql-driver/mysql](https://github.com/go-sql-driver/mysql)
    * PostgreSQL (pgx): [https://github.com/jackc/pgx](https://github.com/jackc/pgx)
    * MongoDB: [https://github.com/mongodb/mongo-go-driver](https://github.com/mongodb/mongo-go-driver)
    * Oracle (godror): [https://github.com/godror/godror](https://github.com/godror/godror)
    * SQLite: [https://github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)
    * Redis: [https://github.com/go-redis/redis](https://github.com/go-redis/redis)

### Integração Contínua e Testes Automatizados:
* **CI:** Usar GitHub Actions, GitLab CI ou similar para:
    * Rodar `go build`, `go vet`, `staticcheck`.
    * Executar testes unitários e de integração (`go test -race ./...`).
    * Testar contra múltiplos bancos de dados (usando Docker ou serviços de CI).
    * Publicar releases.
* **Testes:**
    * **Unitários:** Testar lógica isolada (parsing de tags, construção de query sem executar).
    * **Integração:** Testar operações CRUD, migrações, transações contra bancos de dados reais (gerenciados via Docker nos testes). Cobrir todos os dialetos suportados.

---

Este documento fornece uma base sólida para o desenvolvimento do **TypeGorm**. É um projeto ambicioso que requer um esforço considerável, especialmente para suportar múltiplos bancos de dados de forma robusta e completa como o TypeORM original. A chave será focar na qualidade da API, na robustez da implementação e na clareza da documentação.