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
    * [✅] SQL Server (Implementado e Testado)
* [✅] **Drivers NoSQL:**
    * [✅] MongoDB (Implementado e Testado - Conexão/Ping/Operações básicas via driver nativo)

**Metadados e Mapeamento:**

* [✅] Definição das Structs de Metadados (`EntityMetadata`, `ColumnMetadata`, `RelationMetadata`, etc.).
* [✅] **Parser de Tags (`metadata.Parse`) implementado com:**
    * [✅] Leitura de tags `typegorm:"..."`.
    * [✅] Parsing de tags comuns (pk, column, type, size, unique, index, default, etc).
    * [✅] Tratamento de colunas especiais (createdAt, updatedAt, deletedAt).
    * [✅] Inferência de nome de tabela/coluna (convenção snake_case).
    * [✅] Inferência de nulidade básica e para tipos especiais (ponteiros, `sql.Null*`).
    * [✅] Cache de metadados concorrente implementado.
    * [✅] **Parsing e Validação de tags de Relacionamento** (`relation:`, `joinColumn:`, `mappedBy:`, `joinTable:`).
    * [✅] **Validação robusta** de combinações de tags (conflitos, requisitos por tipo de relação).
    * [✅] **Agregação de múltiplos erros** de parsing/validação para feedback completo.
* [✅] **Parser Testado e Validado** (Testes unitários passando, incluindo validação de colunas, tipos, constraints, relações e múltiplos cenários de erro).

**Operações ORM (SQL - Camada Inicial):**

**Operações ORM (SQL - Camada Inicial):**

* [✅] Função `typegorm.Insert` implementada.
* [✅] Função `typegorm.FindByID` implementada.
* [✅] Função `typegorm.Update` implementada.
* [✅] Função `typegorm.Delete` implementada (Hard e Soft).
* [✅] Testes de CRUD (Insert, FindByID, Update, Delete/SoftDelete) implementados e passando.
* [✅] **Função `typegorm.Find` (Busca Múltipla)** implementada (com suporte a filtros/ordem/limite/offset básicos).
* [✅] **Utilização de Metadados de Relações** implementada (com estratégia inicial de carregamento, ex: JOINs ou Lazy Loading).
* [✅] **Testes de CRUD (Insert, FindByID, Update, Delete/SoftDelete, Find) implementados e passando.**

---

## 🎯 Próximas Etapas Sugeridas:

Aqui estão as próximas fases lógicas, em uma ordem sugerida (mas podemos ajustar!):

1.  **Iniciar o Query Builder:**
    * Começar a projetar a API fluente (ex: `typegorm.QueryBuilder(ds).Model(&Usuario{}).Select(...).Where(...).OrderBy(...).Limit(...)`).
    * Implementar a construção de queries SQL baseada nos metadados e nas chamadas da API fluente.
    * Integrar com `GetOne()` (similar a `FindByID`), `GetMany()` (similar a `Find`), `Exec()` (para Updates/Deletes via QB).
    * **Por que depois das Relações?** O QB se beneficia muito de ter os metadados de relacionamento para construir JOINs automaticamente.

2.  **Implementar Operações ORM para MongoDB:**
    * Adaptar/criar funções como `Insert`, `FindByID`, `Find`, `Update`, `Delete` que funcionem com a interface `DocumentStore` e usem a API do driver Mongo (`bson` para filtros/updates, `primitive.ObjectID` para IDs, etc.), possivelmente reutilizando os metadados (ou usando tags `bson`).
    * **Por que depois do QB SQL?** Permite focar em solidificar a experiência SQL ORM primeiro.

3.  **Migrations:**
    * Projetar e implementar a ferramenta de linha de comando (`typegorm migrate ...`).
    * Lógica para comparar metadados com o schema do banco e gerar/executar SQL DDL.

4.  **Drivers Adicionais (Redis, Oracle):** Adicionar conforme necessário/demandado.

5.  **Funcionalidades Avançadas:** Caching, Listeners, etc.

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