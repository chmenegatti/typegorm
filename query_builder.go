package typegorm // Ou o nome do seu pacote principal

import (
	"context"
	"errors" // Já vamos importar, pois usaremos para erros
	"fmt"
	"strings"
	"time"

	// Para formatação de erros
	"reflect"

	// Usaremos para manipulação de strings
	"github.com/chmenegatti/typegorm/metadata" // Ajuste o path se necessário
	// Importar a interface DataSource (se estiver em outro pacote)
	// Ex: "github.com/chmenegatti/typegorm/driver"
)

const (
	logPrefixInfo  = "[LOG-QB]"   // Informações gerais sobre o fluxo do QB
	logPrefixDebug = "[DEBUG-QB]" // Logs detalhados (ex: SQL gerado, argumentos) - Usaremos mais tarde
	logPrefixError = "[ERROR-QB]" // Erros encontrados durante a construção ou execução
	logPrefixWarn  = "[WARN-QB]"  // Avisos sobre condições potencialmente problemáticas
)

// QueryBuilder armazena o estado da construção da query SQL.
// Mantém a referência ao DataSource para execução e aos metadados
// do modelo alvo. Acumula erros de construção internamente.
type QueryBuilder struct {
	ctx        context.Context
	dataSource DataSource // Conexão/transação com o banco
	entityMeta *metadata.EntityMetadata
	modelType  reflect.Type // Tipo da struct base (ex: UsuarioRel)

	// Cláusulas SQL a serem construídas
	selectCols []string        // Nomes das colunas DB para SELECT (nil = todas)
	conditions []sqlCondition  // Condições para WHERE (unidas por AND)
	orderBy    []orderByClause // Cláusulas ORDER BY
	limit      int             // Valor do LIMIT (-1 se não definido)
	offset     int             // Valor do OFFSET (-1 se não definido)
	preload    map[string]bool // Relações a serem pré-carregadas (campo da struct Go como chave)

	// Estado interno
	buildErr error // Acumula erros durante a construção (validação, etc.)
}

// sqlCondition representa uma condição WHERE simples.
// Futuramente, pode evoluir para suportar OR, grupos, etc.
type sqlCondition struct {
	query     string // Fragmento SQL da condição (ex: "idade > ?", "status = ?")
	args      []any  // Argumentos para os placeholders no fragmento
	connector string // Conector lógico (AND/OR) - não usado ainda
}

// orderByClause representa uma cláusula ORDER BY para uma coluna.
type orderByClause struct {
	column    string // Nome da coluna no DB (já convertido de nome de campo Go)
	direction string // "ASC" ou "DESC"
}

// NewQuery inicia um novo QueryBuilder associado a um DataSource.
// É o ponto de entrada para construir uma nova consulta.
func NewQuery(ctx context.Context, ds DataSource) *QueryBuilder {
	// Cria a instância básica primeiro
	qb := &QueryBuilder{
		ctx:        ctx,
		dataSource: ds, // Armazena mesmo se for nil, o erro será tratado
		limit:      -1,
		offset:     -1,
		preload:    make(map[string]bool),
	}

	// Valida o DataSource após criar o QB para poder armazenar o erro nele
	if ds == nil {
		err := errors.New("NewQuery: DataSource não pode ser nil")
		qb.buildErr = err
		// Log do erro que impede a execução futura
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339)) // Adiciona timestamp
		// Não precisa logar INFO aqui, pois a construção falhou
	} else {
		// Log apenas informativo sobre a criação bem-sucedida (pode ser comentado se for muito verboso)
		// fmt.Printf("%s QueryBuilder iniciado com DataSource válido. [%s]\n", logPrefixInfo, time.Now().Format(time.RFC3339))
	}

	return qb
}

// Model especifica o modelo (struct) base para a query.
// É essencial chamar este método antes de usar funções que dependem
// de nomes de campos ou da tabela.
// Recebe uma instância ou ponteiro para a struct.
func (qb *QueryBuilder) Model(modelInstance interface{}) *QueryBuilder {
	// 1. Checa erro prévio
	if qb.buildErr != nil {
		// O erro já foi logado quando ocorreu
		return qb
	}

	// 2. Valida nil
	if modelInstance == nil {
		err := errors.New("Model: modelInstance não pode ser nil")
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Reflect e validação de tipo
	val := reflect.ValueOf(modelInstance)
	originalTypeStr := fmt.Sprintf("%T", modelInstance) // Guarda nome do tipo para logs

	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			err := fmt.Errorf("Model: modelInstance (ponteiro %s) não pode ser nil", originalTypeStr)
			qb.buildErr = err
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return qb
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		err := fmt.Errorf("Model: esperado struct ou ponteiro para struct, obteve %s", originalTypeStr)
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}
	modelType := val.Type()

	// Log informativo antes de chamar o Parse (o Parse já loga o cache)
	// fmt.Printf("%s Definindo modelo %s, buscando metadados... [%s]\n", logPrefixInfo, modelType.Name(), time.Now().Format(time.RFC3339))

	// 4. Chama o metadata.Parse
	meta, err := metadata.Parse(modelInstance)
	if err != nil {
		// Guarda o erro no builder E loga a falha
		wrappedErr := fmt.Errorf("Model: erro ao carregar metadados para %s: %w", modelType.Name(), err)
		qb.buildErr = wrappedErr
		// Logamos o erro que veio do Parse diretamente para mais detalhes
		fmt.Printf("%s Falha ao carregar metadados para %s: %v [%s]\n", logPrefixError, modelType.Name(), err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 5. Sucesso! Armazena e loga
	qb.modelType = modelType
	qb.entityMeta = meta
	fmt.Printf("%s Modelo definido: %s (Tabela: %s) [%s]\n", logPrefixInfo, modelType.Name(), meta.TableName, time.Now().Format(time.RFC3339))

	// 6. Retorna o builder
	return qb
}

// Select define explicitamente quais colunas buscar do banco de dados.
// Aceita nomes dos campos da struct Go como argumentos (ex: "ID", "NomeUsuario").
// Se este método não for chamado, o builder buscará todas as colunas mapeadas
// da entidade principal (`qb.entityMeta.Columns`) por padrão quando executar a query.
// Chamar Select múltiplas vezes anexa as novas colunas às já selecionadas.
// Ex: qb.Select("ID", "Nome").Select("Email") resulta em buscar as colunas de banco
//
//	correspondentes a ID, Nome e Email.
func (qb *QueryBuilder) Select(goFieldNames ...string) *QueryBuilder {
	// 1. Checa erro prévio na construção
	if qb.buildErr != nil {
		return qb
	}

	// 2. Garante que Model() foi chamado, pois precisamos dos metadados
	if qb.entityMeta == nil {
		err := errors.New("Select: Model() deve ser chamado antes de Select()")
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Inicializa a slice `selectCols` na primeira chamada a `Select`.
	// Se `selectCols` for `nil`, significa "selecionar todas as colunas".
	// Se não for `nil`, significa "selecionar apenas as colunas nesta slice".
	if qb.selectCols == nil {
		qb.selectCols = make([]string, 0, len(goFieldNames))
		// Log opcional indicando que a seleção explícita começou
		// fmt.Printf("%s Iniciando seleção explícita de colunas para %s. [%s]\n", logPrefixInfo, qb.entityMeta.Name, time.Now().Format(time.RFC3339))
	}

	// 4. Itera sobre os nomes dos campos Go fornecidos
	processedDbCols := make([]string, 0, len(goFieldNames)) // Para log no final
	for _, fieldName := range goFieldNames {
		// Validação básica do nome do campo
		trimmedFieldName := strings.TrimSpace(fieldName)
		if trimmedFieldName == "" {
			err := errors.New("Select: nome do campo não pode ser vazio")
			qb.buildErr = err
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return qb // Para no primeiro erro
		}

		// 5. Busca o metadado da coluna usando o nome do campo Go
		colMeta, ok := qb.entityMeta.ColumnsByName[trimmedFieldName]
		if !ok {
			// Erro: Campo não encontrado ou não é uma coluna mapeada.
			// Damos uma mensagem um pouco melhor se for uma relação.
			errMsg := ""
			if _, isRelation := qb.entityMeta.RelationsByName[trimmedFieldName]; isRelation {
				errMsg = fmt.Sprintf("Select: campo '%s' em %s é uma relação, não uma coluna. Use Preload() para carregar relações.", trimmedFieldName, qb.entityMeta.Name)
			} else {
				errMsg = fmt.Sprintf("Select: campo '%s' não encontrado ou não é uma coluna mapeada em %s", trimmedFieldName, qb.entityMeta.Name)
			}
			err := errors.New(errMsg)
			qb.buildErr = err
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return qb // Para no primeiro erro
		}

		// 6. Sucesso! Adiciona o NOME DA COLUNA DO BANCO DE DADOS (`colMeta.ColumnName`)
		//    à nossa slice `selectCols`.
		qb.selectCols = append(qb.selectCols, colMeta.ColumnName)
		processedDbCols = append(processedDbCols, colMeta.ColumnName) // Adiciona para o log
	}

	// Log informando quais colunas (nomes do DB) foram adicionadas nesta chamada
	if len(processedDbCols) > 0 {
		fmt.Printf("%s Colunas adicionadas ao SELECT: %v [%s]\n", logPrefixInfo, processedDbCols, time.Now().Format(time.RFC3339))
	}

	// 7. Retorna o builder para encadeamento
	return qb
}

// Where adiciona uma condição à cláusula WHERE da query SQL, conectada por AND.
// Múltiplas chamadas a Where ou OrWhere são adicionadas sequencialmente.
// A forma como são unidas ("AND" ou "OR") depende de qual método foi usado para adicioná-las.
// Ex: qb.Where("a=?", 1).Where("b=?", 2) -> WHERE (a=?) AND (b=?)
func (qb *QueryBuilder) Where(queryFragment string, args ...interface{}) *QueryBuilder {
	// 1. Checa erro prévio
	if qb.buildErr != nil {
		return qb
	}
	// 2. Validações básicas
	trimmedQuery := strings.TrimSpace(queryFragment)
	if trimmedQuery == "" {
		err := errors.New("Where: queryFragment não pode ser vazio")
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Cria a struct da condição com conector "AND"
	condition := sqlCondition{
		query:     trimmedQuery,
		args:      args,
		connector: "AND", // Esta condição se conectará à anterior com AND
	}

	// 4. Adiciona a condição à lista
	qb.conditions = append(qb.conditions, condition)

	// 5. Log informativo (indicando que é AND)
	fmt.Printf("%s Condição AND WHERE adicionada: \"%s\" (Args: %d) [%s]\n", logPrefixInfo, trimmedQuery, len(args), time.Now().Format(time.RFC3339))

	// 6. Retorna o builder
	return qb
}

// OrWhere adiciona uma condição à cláusula WHERE da query SQL, conectada por OR.
// Funciona de forma similar ao Where, mas usará OR para conectar com a condição anterior.
// Ex: qb.Where("a=?", 1).OrWhere("b=?", 2) -> WHERE (a=?) OR (b=?)
//
// IMPORTANTE: Use placeholders '?' e passe valores via `args` para segurança.
// Cuidado com a precedência de operadores SQL (AND geralmente é avaliado antes de OR)
// ao misturar Where e OrWhere sem um mecanismo de agrupamento explícito (futuro).
func (qb *QueryBuilder) OrWhere(queryFragment string, args ...interface{}) *QueryBuilder {
	// 1. Checa erro prévio
	if qb.buildErr != nil {
		return qb
	}
	// 2. Validações básicas (idênticas ao Where)
	trimmedQuery := strings.TrimSpace(queryFragment)
	if trimmedQuery == "" {
		err := errors.New("OrWhere: queryFragment não pode ser vazio")
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Cria a struct da condição com conector "OR"
	condition := sqlCondition{
		query:     trimmedQuery,
		args:      args,
		connector: "OR", // Esta condição se conectará à anterior com OR
	}

	// 4. Adiciona a condição à lista
	qb.conditions = append(qb.conditions, condition)

	// 5. Log informativo (indicando que é OR)
	fmt.Printf("%s Condição OR WHERE adicionada: \"%s\" (Args: %d) [%s]\n", logPrefixInfo, trimmedQuery, len(args), time.Now().Format(time.RFC3339))

	// 6. Retorna o builder
	return qb
}

// OrderBy adiciona uma cláusula ORDER BY à query.
// Aceita o nome do campo da struct Go (ex: "NomeUsuario", "DataCriacao") pelo qual ordenar.
// O segundo argumento opcional 'direction' especifica a direção: "ASC" (padrão) ou "DESC".
// A comparação da direção é case-insensitive (aceita "desc", "DESC", "Desc").
// Múltiplas chamadas a OrderBy adicionam critérios de ordenação secundários.
// A ordem das chamadas importa.
// Ex: qb.OrderBy("DataCriacao", "DESC").OrderBy("NomeUsuario")
//
//	Resulta em SQL: ORDER BY data_criacao DESC, nome_usuario ASC
func (qb *QueryBuilder) OrderBy(goFieldName string, direction ...string) *QueryBuilder {
	// 1. Checa erro prévio na construção
	if qb.buildErr != nil {
		return qb
	}

	// 2. Garante que Model() foi chamado, pois precisamos dos metadados para mapear o campo
	if qb.entityMeta == nil {
		err := errors.New("OrderBy: Model() deve ser chamado antes de OrderBy()")
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Valida o nome do campo e obtém o nome da coluna no banco
	trimmedFieldName := strings.TrimSpace(goFieldName)
	if trimmedFieldName == "" {
		err := errors.New("OrderBy: nome do campo não pode ser vazio")
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// Procura o campo nos metadados das colunas
	colMeta, ok := qb.entityMeta.ColumnsByName[trimmedFieldName]
	if !ok {
		// Se não achou, dá um erro claro, diferenciando se é relação ou campo inexistente
		errMsg := ""
		if _, isRelation := qb.entityMeta.RelationsByName[trimmedFieldName]; isRelation {
			errMsg = fmt.Sprintf("OrderBy: campo '%s' em %s é uma relação, não uma coluna. Não é possível ordenar diretamente por relações desta forma.", trimmedFieldName, qb.entityMeta.Name)
		} else {
			errMsg = fmt.Sprintf("OrderBy: campo '%s' não encontrado ou não é uma coluna mapeada em %s", trimmedFieldName, qb.entityMeta.Name)
		}
		err := errors.New(errMsg)
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}
	// Guarda o nome da coluna no banco de dados (ex: "data_criacao")
	dbColumnName := colMeta.ColumnName

	// 4. Determina a direção da ordenação (ASC ou DESC)
	orderDirection := "ASC" // Padrão é ASC
	if len(direction) > 0 {
		// Pega o primeiro argumento de direção, remove espaços e compara case-insensitive
		dirArg := strings.TrimSpace(direction[0])
		if strings.EqualFold(dirArg, "DESC") {
			orderDirection = "DESC"
		}
		// Qualquer outra coisa diferente de "DESC" (case-insensitive) resulta em ASC
	}

	// 5. Cria a struct 'orderByClause' com os dados corretos
	clause := orderByClause{
		column:    dbColumnName,   // Nome da coluna no DB
		direction: orderDirection, // "ASC" ou "DESC"
	}

	// 6. Adiciona a cláusula à lista de ordenações do builder
	qb.orderBy = append(qb.orderBy, clause)

	// 7. Log informativo
	fmt.Printf("%s Cláusula ORDER BY adicionada: %s %s [%s]\n", logPrefixInfo, dbColumnName, orderDirection, time.Now().Format(time.RFC3339))

	// 8. Retorna o builder para encadeamento
	return qb
}

// Limit especifica o número máximo de registros a serem retornados pela query.
// Um valor negativo resultará em erro. Um valor de 0 é geralmente interpretado
// pelos bancos de dados como "retornar zero registros".
// Corresponde à cláusula LIMIT (ou equivalente) do SQL.
func (qb *QueryBuilder) Limit(limit int) *QueryBuilder {
	// 1. Checa erro prévio na construção
	if qb.buildErr != nil {
		return qb
	}

	// 2. Valida se o limit é não-negativo
	if limit < 0 {
		err := fmt.Errorf("Limit: valor (%d) não pode ser negativo", limit)
		qb.buildErr = err
		// Log do erro de validação
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Armazena o valor no QueryBuilder
	// O valor -1 (inicial) significa "sem limite definido". Qualquer valor >= 0 sobrescreve.
	qb.limit = limit

	// 4. Log informativo
	fmt.Printf("%s Cláusula LIMIT definida para: %d [%s]\n", logPrefixInfo, limit, time.Now().Format(time.RFC3339))

	// 5. Retorna o builder para encadeamento
	return qb
}

// Offset especifica o número de registros a serem pulados (deslocamento)
// antes de começar a retornar os registros. Quase sempre usado em conjunto com Limit.
// Um valor negativo resultará em erro. Um valor de 0 significa "começar do primeiro registro".
// Corresponde à cláusula OFFSET (ou equivalente) do SQL.
func (qb *QueryBuilder) Offset(offset int) *QueryBuilder {
	// 1. Checa erro prévio na construção
	if qb.buildErr != nil {
		return qb
	}

	// 2. Valida se o offset é não-negativo
	if offset < 0 {
		err := fmt.Errorf("Offset: valor (%d) não pode ser negativo", offset)
		qb.buildErr = err
		// Log do erro de validação
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Armazena o valor no QueryBuilder
	// O valor -1 (inicial) significa "sem offset definido". Qualquer valor >= 0 sobrescreve.
	qb.offset = offset

	// 4. Log informativo
	fmt.Printf("%s Cláusula OFFSET definida para: %d [%s]\n", logPrefixInfo, offset, time.Now().Format(time.RFC3339))

	// 5. Retorna o builder para encadeamento
	return qb
}

// Preload especifica quais relações (definidas na struct com tags `typegorm:"relation..."`)
// devem ser carregadas junto com a entidade principal (Eager Loading).
// Aceita nomes dos campos da struct Go que representam as relações.
//
// Ex: Buscar usuários e seus perfis relacionados (assumindo um campo `Profile` na struct User):
//
//	qb.Model(&User{}).Preload("Profile").GetMany(&users)
//
// Ex: Buscar posts e seus autores (assumindo campo `Author` na struct Post):
//
//	qb.Model(&Post{}).Preload("Author").GetMany(&posts)
//
// NOTAS IMPORTANTES:
//  1. Este método APENAS registra a *intenção* de pré-carregar a relação.
//     A lógica real de como buscar os dados (JOIN vs queries separadas) e como
//     mapeá-los para as structs será implementada posteriormente, principalmente
//     na função `buildSelectSQL` e nos métodos de scan (`GetOne`/`GetMany`).
//  2. VERSÃO INICIAL: Suporta apenas relações de primeiro nível (ex: "Profile", "Author").
//     Preload aninhado (ex: "Posts.Comments") NÃO é suportado ainda. Nomes com "."
//     resultarão em erro nesta fase.
func (qb *QueryBuilder) Preload(goRelationFieldNames ...string) *QueryBuilder {
	// 1. Checa erro prévio na construção
	if qb.buildErr != nil {
		return qb
	}

	// 2. Garante que Model() foi chamado (precisamos de entityMeta.RelationsByName)
	if qb.entityMeta == nil {
		err := errors.New("Preload: Model() deve ser chamado antes de Preload()")
		qb.buildErr = err
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return qb
	}

	// 3. Itera sobre os nomes dos campos de relação fornecidos
	processedRelations := make([]string, 0, len(goRelationFieldNames)) // Guarda nomes para log
	for _, fieldName := range goRelationFieldNames {
		trimmedFieldName := strings.TrimSpace(fieldName)
		if trimmedFieldName == "" {
			err := errors.New("Preload: nome da relação não pode ser vazio")
			qb.buildErr = err
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return qb // Para no primeiro erro
		}

		// 4. Validação - Rejeita preload aninhado por enquanto
		if strings.Contains(trimmedFieldName, ".") {
			// Implementaremos isso futuramente, se necessário
			err := fmt.Errorf("Preload: preload aninhado ('%s') ainda não é suportado", trimmedFieldName)
			qb.buildErr = err
			fmt.Printf("%s %v [%s]\n", logPrefixWarn, err, time.Now().Format(time.RFC3339)) // Usando WARN pois é limitação
			return qb
		}

		// 5. Verifica se o nome fornecido corresponde a uma RELAÇÃO válida nos metadados
		_, isRelation := qb.entityMeta.RelationsByName[trimmedFieldName]
		if !isRelation {
			// Não é uma relação conhecida. Verifica se é uma coluna para dar erro melhor.
			errMsg := ""
			if _, isColumn := qb.entityMeta.ColumnsByName[trimmedFieldName]; isColumn {
				errMsg = fmt.Sprintf("Preload: campo '%s' em %s é uma coluna mapeada, não uma relação. Use Select() para colunas.", trimmedFieldName, qb.entityMeta.Name)
			} else {
				errMsg = fmt.Sprintf("Preload: campo '%s' não encontrado ou não é uma relação mapeada em %s", trimmedFieldName, qb.entityMeta.Name)
			}
			err := errors.New(errMsg)
			qb.buildErr = err
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return qb // Para no primeiro erro
		}

		// 6. É uma relação válida! Adiciona ao mapa `preload`.
		// Usar um mapa (`map[string]bool`) garante que não teremos nomes duplicados.
		if qb.preload == nil { // Garante que o mapa está inicializado (embora NewQuery já faça isso)
			qb.preload = make(map[string]bool)
		}
		qb.preload[trimmedFieldName] = true                               // Marca a relação para preload
		processedRelations = append(processedRelations, trimmedFieldName) // Adiciona ao log
	}

	// 7. Log informando quais relações foram marcadas para preload nesta chamada
	if len(processedRelations) > 0 {
		fmt.Printf("%s Relações adicionadas para Preload: %v [%s]\n", logPrefixInfo, processedRelations, time.Now().Format(time.RFC3339))
	}

	// 8. Retorna o builder
	return qb
}

// buildSelectSQL constrói a string SQL SELECT e a lista de argumentos correspondentes
// com base no estado atual do QueryBuilder.
//
// Retorna a string SQL, a slice de argumentos para os placeholders '?', e um erro
// se alguma validação falhar durante a construção (ex: Model não chamado).
//
// NOTA: Esta é a implementação inicial e NÃO inclui JOINs para Preload ainda.
func (qb *QueryBuilder) buildSelectSQL() (sqlStr string, args []interface{}, err error) {
	// 1. Checa erros acumulados ou estado inválido essencial
	if qb.buildErr != nil {
		// Retorna o erro que já ocorreu durante a configuração (ex: campo inválido em OrderBy)
		return "", nil, fmt.Errorf("erro prévio na construção da query: %w", qb.buildErr)
	}
	if qb.entityMeta == nil {
		// Precisa ter chamado Model() para saber de qual tabela buscar
		err = errors.New("buildSelectSQL: Model() deve ser chamado antes de construir a query SQL")
		fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
		return "", nil, err
	}
	// DataSource foi validado em NewQuery, não precisa checar aqui de novo.

	// Usaremos strings.Builder para eficiência na concatenação do SQL
	var sb strings.Builder
	// Slice para acumular os argumentos na ordem correta dos placeholders '?'
	args = make([]interface{}, 0)

	// --- Construção da Query ---

	// 2. Cláusula SELECT
	sb.WriteString("SELECT ")
	selectColumns := []string{} // Slice para guardar os nomes das colunas a serem selecionadas

	if qb.selectCols == nil {
		// Caso padrão: .Select() não foi chamado, busca todas as colunas mapeadas da entidade principal.
		if len(qb.entityMeta.Columns) == 0 {
			err = fmt.Errorf("buildSelectSQL: modelo '%s' não possui colunas mapeadas", qb.entityMeta.Name)
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return "", nil, err
		}
		// Pega todas as colunas da entidade base
		for _, colMeta := range qb.entityMeta.Columns {
			// Qualificamos com o nome da tabela para evitar ambiguidade caso JOINs sejam adicionados futuramente.
			selectColumns = append(selectColumns, qb.entityMeta.TableName+"."+colMeta.ColumnName)
		}
		// Log (opcional): fmt.Printf("%s SELECT: Todas as colunas de %s (%d)\n", logPrefixDebug, qb.entityMeta.TableName, len(selectColumns))
	} else {
		// Caso explícito: .Select() foi chamado.
		if len(qb.selectCols) == 0 {
			// Chamou .Select() mas não passou colunas? Ou algo deu errado antes.
			err = errors.New("buildSelectSQL: Select() foi chamado, mas nenhuma coluna foi definida para seleção")
			fmt.Printf("%s %v [%s]\n", logPrefixError, err, time.Now().Format(time.RFC3339))
			return "", nil, err
		}
		// Usa as colunas que foram adicionadas via .Select() (já são nomes do DB)
		for _, dbColName := range qb.selectCols {
			// Também qualifica com o nome da tabela
			selectColumns = append(selectColumns, qb.entityMeta.TableName+"."+dbColName)
		}
		// Log (opcional): fmt.Printf("%s SELECT: Colunas explícitas: %v\n", logPrefixDebug, selectColumns)
	}
	// Junta as colunas selecionadas com vírgula
	sb.WriteString(strings.Join(selectColumns, ", "))

	// 3. Cláusula FROM
	sb.WriteString(" FROM ")
	sb.WriteString(qb.entityMeta.TableName) // Nome da tabela principal vem dos metadados

	// --- Ponto onde JOINs para Preload seriam inseridos futuramente ---

	// 4. Cláusula WHERE
	if len(qb.conditions) > 0 {
		sb.WriteString(" WHERE ")
		for i, cond := range qb.conditions {
			if i > 0 { // Para a segunda condição em diante...
				sb.WriteString(" ")
				sb.WriteString(cond.connector) // Adiciona "AND" ou "OR"
				sb.WriteString(" ")
			}
			// Envolve cada condição em parênteses para garantir a precedência correta
			// Ex: WHERE (cond1) AND (cond2) OR (cond3)
			sb.WriteString("(")
			sb.WriteString(cond.query) // O fragmento SQL (ex: "idade > ?")
			sb.WriteString(")")
			// Adiciona os argumentos desta condição à lista geral de argumentos
			args = append(args, cond.args...)
		}
	}

	// 5. Cláusula ORDER BY
	if len(qb.orderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		orderClauses := make([]string, 0, len(qb.orderBy))
		for _, clause := range qb.orderBy {
			// Qualifica com nome da tabela. Ex: "usuarios.data_criacao DESC"
			orderClauses = append(orderClauses, qb.entityMeta.TableName+"."+clause.column+" "+clause.direction)
		}
		// Junta as cláusulas de ordenação com vírgula
		sb.WriteString(strings.Join(orderClauses, ", "))
	}

	// 6. Cláusula LIMIT
	if qb.limit >= 0 { // -1 significa que não foi definido
		sb.WriteString(" LIMIT ?")
		args = append(args, qb.limit) // Adiciona o valor do limit aos args
	}

	// 7. Cláusula OFFSET
	if qb.offset >= 0 { // -1 significa que não foi definido
		// Aviso se OFFSET for usado sem LIMIT (pode ter comportamento inesperado)
		if qb.limit < 0 {
			fmt.Printf("%s OFFSET %d definido sem LIMIT. Comportamento pode variar entre bancos de dados. [%s]\n", logPrefixWarn, qb.offset, time.Now().Format(time.RFC3339))
		}
		sb.WriteString(" OFFSET ?")
		args = append(args, qb.offset) // Adiciona o valor do offset aos args
	}

	// --- Fim da Construção ---

	// 8. Finaliza a string SQL
	finalSQL := sb.String()

	// Log de Debug da query final e argumentos (CUIDADO COM DADOS SENSÍVEIS nos args em produção)
	fmt.Printf("%s SQL Gerado: %s [%s]\n", logPrefixDebug, finalSQL, time.Now().Format(time.RFC3339))
	if len(args) > 0 {
		// Poderíamos ter uma flag para logar ou não os argumentos
		fmt.Printf("%s Args: %v [%s]\n", logPrefixDebug, args, time.Now().Format(time.RFC3339))
	}

	// Retorna o SQL montado, os argumentos coletados, e nil (sem erro nesta etapa)
	return finalSQL, args, nil
}
