// pkg/schema/field.go
package schema

import "reflect"

// Field representa metadados sobre um campo de struct mapeado para o banco de dados.
// Esta é uma definição preliminar e será expandida posteriormente.
type Field struct {
	GoName       string       // Nome do campo na struct Go
	GoType       reflect.Type // Tipo do campo em Go
	DBName       string       // Nome da coluna/atributo no banco
	IsPrimary    bool         // É chave primária?
	IsRequired   bool         // NOT NULL / obrigatório?
	Size         int          // Tamanho (ex: VARCHAR(size))
	Precision    int          // Para tipos decimais
	Scale        int          // Para tipos decimais
	DefaultValue *string      // Valor padrão (como string SQL/literal)
	// Outros atributos podem ser adicionados: Unique, Index, relacionamentos, etc.
}
