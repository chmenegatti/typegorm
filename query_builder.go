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
	query string // Fragmento SQL da condição (ex: "idade > ?", "status = ?")
	args  []any  // Argumentos para os placeholders no fragmento
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
