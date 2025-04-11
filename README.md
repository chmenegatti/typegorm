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

Que maravilha! Todos os testes passando, incluindo o CRUD básico com soft delete e os drivers SQL e MongoDB iniciais, é um progresso fantástico! 🚀

Você tem razão, com várias peças se encaixando, é bom ter uma lista clara das próximas etapas para mantermos o foco e a organização no desenvolvimento do nosso TypeGorm brasileiro.

Aqui está um resumo do que fizemos e uma lista sugerida para os próximos passos:

---

## 🗺️ Roteiro TypeGorm (Abril de 2025)

**Fundação e Conexão:**

* [✅] Estrutura básica do projeto Go (`go mod init`).
* [✅] Interface `DataSource` (para SQL) definida.
* [✅] Interface `DocumentStore` (para NoSQL/Mongo) definida.
* [✅] Sistema de Registro de Drivers (SQL e NoSQL via `init` e `Register...Driver`).
* [✅] Fábricas de Conexão (`typegorm.Connect` e `typegorm.ConnectDocumentStore`) usando `DriverTyper`.
* [✅] **Drivers SQL:**
    * [✅] SQLite (Implementado e Testado)
    * [✅] PostgreSQL (Implementado e Testado)
    * [✅] MySQL / MariaDB (Implementado e Testado)
* [✅] **Drivers NoSQL:**
    * [✅] MongoDB (Implementado e Testado - Conexão/Ping/Operações básicas via driver nativo)

**Metadados e Mapeamento:**

* [✅] Definição das Structs de Metadados (`EntityMetadata`, `ColumnMetadata`).
* [✅] Parser de Tags (`metadata.Parse`) implementado com:
    * [✅] Leitura de tags `typegorm:"..."`.
    * [✅] Parsing de tags comuns (pk, column, type, size, unique, index, default, etc).
    * [✅] Tratamento de colunas especiais (`createdAt`, `updatedAt`, `deletedAt`).
    * [✅] Inferência de nome de tabela/coluna (convenção snake\_case).
    * [✅] Inferência de nulidade básica.
    * [✅] Cache de metadados implementado.
    * [✅] **Parser Testado e Validado**.

**Operações ORM (SQL - Camada Inicial):**

* [✅] Função `typegorm.Insert` implementada (usa metadados, trata autoIncrement PK, createdAt/updatedAt).
* [✅] Função `typegorm.FindByID` implementada (usa metadados, scan dinâmico, trata `sql.ErrNoRows`, respeita soft delete).
* [✅] Função `typegorm.Update` implementada (usa metadados, atualiza todos os campos não-PK, trata `updatedAt`).
* [✅] Função `typegorm.Delete` implementada (com suporte a Hard e Soft Delete baseado em `deletedAt`).
* [✅] **Testes de CRUD (Insert, FindByID, Update, Delete/SoftDelete) implementados e passando.**

---

## 🎯 Próximas Etapas Sugeridas:

Aqui estão as próximas fases lógicas, em uma ordem sugerida (mas podemos ajustar!):

1.  👉 **Implementar `typegorm.Find` (Busca Múltipla - SQL):**
    * Criar uma função `Find(ctx, ds, slicePtr, options...)` que busca múltiplos registros.
    * `slicePtr` seria um ponteiro para um slice da struct (ex: `&[]Usuario{}`).
    * `options` poderia ser uma struct ou argumentos variádicos para definir filtros (`WHERE`), ordenação (`ORDER BY`), limite (`LIMIT`) e offset (`OFFSET`).
    * Internamente, construiria a query `SELECT`, usaria `ds.QueryContext`, iteraria sobre os `rows`, e faria o `Scan` dinâmico para preencher o slice.
    * **Por que agora?** Completa o conjunto básico de operações de leitura (FindByID, Find) usando a infraestrutura atual antes de avançar para abstrações maiores.

2.  **Definir e Parsear Relações:**
    * Atualizar `metadata.go` para incluir informações sobre relações (`OneToOne`, `OneToMany`, `ManyToMany`) em `EntityMetadata` / `ColumnMetadata`.
    * Atualizar `metadata/parser.go` para reconhecer e parsear tags de relacionamento (ex: `relation:`, `joinColumn:`, `mappedBy:`, `joinTable:`).
    * Escrever testes para o parsing das relações.
    * **Por que depois do Find?** Permite focar primeiro em operações de tabela única antes de introduzir a complexidade das junções e carregamento de dados relacionados.

3.  **Iniciar o Query Builder:**
    * Começar a projetar a API fluente (ex: `typegorm.QueryBuilder(ds).Model(&Usuario{}).Select(...).Where(...).OrderBy(...).Limit(...)`).
    * Implementar a construção de queries SQL baseada nos metadados e nas chamadas da API fluente.
    * Integrar com `GetOne()` (similar a `FindByID`), `GetMany()` (similar a `Find`), `Exec()` (para Updates/Deletes via QB).
    * **Por que depois das Relações?** O QB se beneficia muito de ter os metadados de relacionamento para construir JOINs automaticamente.

4.  **Implementar Driver SQL Server:**
    * Seguir o padrão dos outros drivers SQL (criar `driver/sqlserver`, Config, DataSource, registro, testes).
    * **Por que aqui?** Pode ser feito a qualquer momento, mas talvez seja bom ter mais funcionalidades do ORM antes de adicionar outro driver SQL similar.

5.  **Implementar Operações ORM para MongoDB:**
    * Adaptar/criar funções como `Insert`, `FindByID`, `Find`, `Update`, `Delete` que funcionem com a interface `DocumentStore` e usem a API do driver Mongo (`bson` para filtros/updates, `primitive.ObjectID` para IDs, etc.), possivelmente reutilizando os metadados (ou usando tags `bson`).
    * **Por que depois do QB SQL?** Permite focar em solidificar a experiência SQL ORM primeiro.

6.  **Migrations:**
    * Projetar e implementar a ferramenta de linha de comando (`typegorm migrate ...`).
    * Lógica para comparar metadados com o schema do banco e gerar/executar SQL DDL.

7.  **Drivers Adicionais (Redis, Oracle):** Adicionar conforme necessário/demandado.

8.  **Funcionalidades Avançadas:** Caching, Listeners, etc.

## 💾 Bancos de Dados Suportados
Atualmente, o TypeGorm suporta os seguintes bancos de dados, com drivers específicos para cada um. A tabela abaixo resume o status de implementação de cada driver:
| Banco de Dados	| Driver Go Usado	| Status |
|------------------|------------------|----------------|
SQLite	| mattn/go-sqlite3	| ✅ Implementado |
PostgreSQL |	jackc/pgx/v5/stdlib	| ✅ Implementado |
MySQL/MariaDB	| go-sql-driver/mysql |	✅ Implementado |
SQL Server	| microsoft/go-mssqldb |	✅ Implementado |
MongoDB	| go.mongodb.org/mongo-driver |	✅ Implementado |
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