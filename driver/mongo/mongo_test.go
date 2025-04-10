// driver/mongo/mongo_test.go
package mongo_test

import (
	"context"
	"errors" // Para errors.Is

	// Para logs
	"os"
	"reflect" // Para DeepEqual na verificação
	"strings" // Necessário para sql.NullString em outros testes, se houver
	"testing"
	"time"

	"github.com/chmenegatti/typegorm"

	mongo_driver "github.com/chmenegatti/typegorm/driver/mongo"

	// Imports específicos do MongoDB para o teste Insert/Find
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// getTestDocumentStore (permanece igual)
func getTestDocumentStore(t *testing.T) typegorm.DocumentStore {
	t.Helper()

	mongoURI := os.Getenv("TEST_MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	dbName := os.Getenv("TEST_MONGO_DBNAME")
	if dbName == "" {
		dbName = "typegorm_testdb"
	}
	// if os.Getenv("RUN_MONGO_TESTS") != "true" {
	// 	t.Skip("Pulando testes de integração MongoDB: RUN_MONGO_TESTS não definida como 'true'")
	// }

	config := mongo_driver.Config{
		URI:          mongoURI,
		DatabaseName: dbName, // Importante definir o DB padrão aqui
	}
	t.Logf("getTestDocumentStore (Mongo): Conectando a %s (DB Padrão: %s)", "[URI OMITIDO]", config.DatabaseName)

	docStore, err := typegorm.ConnectDocumentStore(config)
	if err != nil {
		t.Fatalf("getTestDocumentStore: typegorm.ConnectDocumentStore() falhou: %v...", err)
	}
	if docStore == nil {
		t.Fatal("...")
	}

	t.Cleanup(func() {
		t.Log("getTestDocumentStore Cleanup (Mongo): Desconectando...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := docStore.Disconnect(ctx); err != nil {
			t.Errorf("...Disconnect() falhou: %v", err)
		} else {
			t.Log("...Desconexão bem-sucedida.")
		}
	})

	// Limpeza inicial do banco de dados de teste ANTES de retornar o DocStore
	// Isso garante um estado limpo para cada teste que chama getTestDocumentStore.
	// CUIDADO: mongoDB.Drop() apaga o banco inteiro! Usar db.Collection().Drop() é mais seguro se compartilhar o DB.
	// Ou usar db.Collection().DeleteMany() para limpar coleções específicas.
	dbInterface := docStore.Database()
	if dbInterface != nil {
		if mongoDB, ok := dbInterface.(*mongo.Database); ok {
			t.Logf("getTestDocumentStore: Limpando banco de dados de teste '%s' antes do teste...", mongoDB.Name())
			// Limpa coleções específicas usadas nos testes em vez de dropar o DB
			errCollDrop := mongoDB.Collection("test_docs").Drop(context.Background())
			if errCollDrop != nil && !strings.Contains(errCollDrop.Error(), "ns not found") { // Ignora erro se coleção não existe
				t.Logf("Aviso: Falha ao dropar coleção 'test_docs' (pode não existir): %v", errCollDrop)
			}
			// Adicionar Drop para outras coleções de teste aqui...
		}
	}

	return docStore
}

// TestMongoConnectionAndPing (permanece igual)
func TestMongoConnectionAndPing(t *testing.T) {
	ds := getTestDocumentStore(t)
	ctx := context.Background()
	t.Log("TestMongoConnectionAndPing: DocumentStore obtido.")

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ds.Ping(pingCtx); err != nil {
		t.Fatalf("docStore.Ping() falhou: %v", err)
	}
	t.Log("TestMongoConnectionAndPing: Ping bem-sucedido.")

	if ds.Client() == nil {
		t.Error("docStore.Client() retornou nil")
	} else {
		t.Log("docStore.Client() ok.")
	}
	if ds.Database() == nil {
		t.Error("docStore.Database() retornou nil")
	} else {
		t.Log("docStore.Database() ok.")
	}
}

// --- Teste Completo: Inserir e Buscar ---

// Struct de exemplo para o teste
type TestDoc struct {
	ID   primitive.ObjectID `bson:"_id,omitempty"` // Padrão Mongo _id, omitempty para inserção
	Name string             `bson:"name"`
	Age  int                `bson:"age"`
	Tags []string           `bson:"tags,omitempty"`
}

func TestMongo_InsertAndFind(t *testing.T) {
	ds := getTestDocumentStore(t) // Obtém DocumentStore limpo
	ctx := context.Background()

	// 1. Obter a instância *mongo.Database a partir da interface
	dbInterface := ds.Database()
	if dbInterface == nil {
		t.Fatal("Instância do Database está nil (DatabaseName foi configurado?). Não pode continuar.")
	}
	db, ok := dbInterface.(*mongo.Database)
	if !ok {
		t.Fatalf("Falha ao converter ds.Database() para *mongo.Database. Tipo recebido: %T", dbInterface)
	}

	// 2. Obter a Coleção
	collectionName := "test_docs"
	collection := db.Collection(collectionName)
	t.Logf("Usando coleção: %s", collectionName)
	// Limpeza prévia já feita (idealmente) no getTestDataSource

	// 3. Inserir um Documento
	docParaInserir := TestDoc{
		Name: "Teste Mongo",
		Age:  10,
		Tags: []string{"go", "typegorm", "teste"},
	}
	t.Logf("Inserindo documento: %+v", docParaInserir)

	insertResult, err := collection.InsertOne(ctx, docParaInserir)
	if err != nil {
		t.Fatalf("collection.InsertOne falhou: %v", err)
	}
	if insertResult.InsertedID == nil {
		t.Fatal("collection.InsertOne não retornou InsertedID")
	}

	// Verifica se o ID inserido é um ObjectID válido
	insertedID, idOk := insertResult.InsertedID.(primitive.ObjectID)
	if !idOk {
		t.Fatalf("InsertedID retornado não é um primitive.ObjectID, tipo: %T", insertResult.InsertedID)
	}
	t.Logf("Documento inserido com ID: %s", insertedID.Hex())

	// 4. Buscar o Documento Inserido
	var docEncontrado TestDoc
	filter := bson.M{"_id": insertedID} // Filtro pelo ID retornado
	t.Logf("Buscando documento com filtro: %v", filter)

	err = collection.FindOne(ctx, filter).Decode(&docEncontrado)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			t.Fatalf("collection.FindOne não encontrou o documento recém-inserido (ID: %s)", insertedID.Hex())
		} else {
			t.Fatalf("collection.FindOne falhou com erro inesperado: %v", err)
		}
	}
	t.Logf("Documento encontrado: %+v", docEncontrado)

	// 5. Verificar os Dados Encontrados
	if docEncontrado.ID != insertedID {
		t.Errorf("Verificação falhou: ID esperado %s, obteve %s", insertedID.Hex(), docEncontrado.ID.Hex())
	}
	if docEncontrado.Name != docParaInserir.Name {
		t.Errorf("Verificação falhou: Name esperado '%s', obteve '%s'", docParaInserir.Name, docEncontrado.Name)
	}
	if docEncontrado.Age != docParaInserir.Age {
		t.Errorf("Verificação falhou: Age esperado %d, obteve %d", docParaInserir.Age, docEncontrado.Age)
	}
	// Usar DeepEqual para comparar slices (Tags)
	if !reflect.DeepEqual(docEncontrado.Tags, docParaInserir.Tags) {
		t.Errorf("Verificação falhou: Tags esperado %v, obteve %v", docParaInserir.Tags, docEncontrado.Tags)
	}

	t.Log("Verificação dos dados do documento encontrado bem-sucedida.")
}

// --- Teste para UpdateOne ---
func TestMongo_UpdateOne(t *testing.T) {
	ds := getTestDocumentStore(t)
	ctx := context.Background()
	dbInterface := ds.Database()
	if dbInterface == nil {
		t.Fatal("Database() retornou nil")
	}
	db, ok := dbInterface.(*mongo.Database)
	if !ok {
		t.Fatalf("Falha na conversão para *mongo.Database: %T", dbInterface)
	}
	collection := db.Collection("test_docs") // Usa a mesma coleção dos outros testes

	// Setup: Insere um documento para atualizar
	docInicial := TestDoc{Name: "Para Atualizar", Age: 20, Tags: []string{"original"}}
	insertResult, err := collection.InsertOne(ctx, docInicial)
	if err != nil {
		t.Fatalf("Setup falhou (InsertOne): %v", err)
	}
	insertedID := insertResult.InsertedID.(primitive.ObjectID)
	t.Logf("Documento para atualizar inserido com ID: %s", insertedID.Hex())

	// Update: Atualiza o documento inserido
	filter := bson.M{"_id": insertedID}
	update := bson.M{
		"$set": bson.M{
			"name": "Atualizado com Sucesso",
			"age":  21,
		},
		"$push": bson.M{ // Adiciona um item ao array
			"tags": "atualizado",
		},
	}
	t.Logf("Atualizando documento com filtro %v e update %v", filter, update)

	updateResult, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		t.Fatalf("collection.UpdateOne falhou: %v", err)
	}

	// Verifica resultado do Update
	if updateResult.MatchedCount != 1 {
		t.Errorf("Esperado MatchedCount=1, obteve %d", updateResult.MatchedCount)
	}
	if updateResult.ModifiedCount != 1 {
		t.Errorf("Esperado ModifiedCount=1, obteve %d", updateResult.ModifiedCount)
	}
	t.Logf("Update bem-sucedido: Matched=%d, Modified=%d", updateResult.MatchedCount, updateResult.ModifiedCount)

	// Verifica: Busca o documento e confere os novos valores
	var docAtualizado TestDoc
	err = collection.FindOne(ctx, filter).Decode(&docAtualizado)
	if err != nil {
		t.Fatalf("FindOne falhou ao buscar documento atualizado: %v", err)
	}

	if docAtualizado.Name != "Atualizado com Sucesso" {
		t.Errorf("Verificação falhou: Name esperado 'Atualizado com Sucesso', obteve '%s'", docAtualizado.Name)
	}
	if docAtualizado.Age != 21 {
		t.Errorf("Verificação falhou: Age esperado 21, obteve %d", docAtualizado.Age)
	}
	expectedTags := []string{"original", "atualizado"}
	if !reflect.DeepEqual(docAtualizado.Tags, expectedTags) {
		t.Errorf("Verificação falhou: Tags esperado %v, obteve %v", expectedTags, docAtualizado.Tags)
	}
	t.Logf("Verificação dos dados atualizados bem-sucedida: %+v", docAtualizado)
}

// --- Teste para DeleteOne ---
func TestMongo_DeleteOne(t *testing.T) {
	ds := getTestDocumentStore(t)
	ctx := context.Background()
	dbInterface := ds.Database()
	if dbInterface == nil {
		t.Fatal("Database() nil")
	}
	db, ok := dbInterface.(*mongo.Database)
	if !ok {
		t.Fatalf("Falha conversão *mongo.Database: %T", dbInterface)
	}
	collection := db.Collection("test_docs")

	// Setup: Insere um documento para deletar
	docParaDeletar := TestDoc{Name: "Para Deletar", Age: 99}
	insertResult, err := collection.InsertOne(ctx, docParaDeletar)
	if err != nil {
		t.Fatalf("Setup falhou (InsertOne): %v", err)
	}
	insertedID := insertResult.InsertedID.(primitive.ObjectID)
	t.Logf("Documento para deletar inserido com ID: %s", insertedID.Hex())

	// Delete: Deleta o documento
	filter := bson.M{"_id": insertedID}
	t.Logf("Deletando documento com filtro: %v", filter)

	deleteResult, err := collection.DeleteOne(ctx, filter)
	if err != nil {
		t.Fatalf("collection.DeleteOne falhou: %v", err)
	}

	// Verifica resultado do Delete
	if deleteResult.DeletedCount != 1 {
		t.Errorf("Esperado DeletedCount=1, obteve %d", deleteResult.DeletedCount)
	} else {
		t.Logf("DeleteOne bem-sucedido: DeletedCount=%d", deleteResult.DeletedCount)
	}

	// Verifica Ausência: Tenta buscar o documento deletado
	var docFantasma TestDoc
	err = collection.FindOne(ctx, filter).Decode(&docFantasma)
	if err == nil {
		t.Errorf("Esperado erro ao buscar documento deletado, mas busca funcionou e retornou: %+v", docFantasma)
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		// Garante que o erro foi especificamente 'documento não encontrado'
		t.Errorf("Erro inesperado ao buscar documento deletado. Esperado mongo.ErrNoDocuments, obteve: %v", err)
	} else {
		t.Logf("Verificação de ausência bem-sucedida (recebeu mongo.ErrNoDocuments).")
	}
}

// --- Teste para Find com Múltiplos Resultados ---
func TestMongo_FindMultiple(t *testing.T) {
	ds := getTestDocumentStore(t)
	ctx := context.Background()
	dbInterface := ds.Database()
	if dbInterface == nil {
		t.Fatal("Database() nil")
	}
	db, ok := dbInterface.(*mongo.Database)
	if !ok {
		t.Fatalf("Falha conversão *mongo.Database: %T", dbInterface)
	}
	collection := db.Collection("test_docs")

	// Setup: Insere múltiplos documentos
	docsParaInserir := []any{ // Usa interface{} para InsertMany
		TestDoc{Name: "Multi A", Age: 5, Tags: []string{"find", "A"}},
		TestDoc{Name: "Multi B", Age: 15, Tags: []string{"find", "B"}},
		TestDoc{Name: "Multi C", Age: 25, Tags: []string{"find", "C"}},
		TestDoc{Name: "Outro", Age: 15}, // Sem a tag "find"
	}
	t.Logf("Inserindo %d documentos para FindMultiple...", len(docsParaInserir))
	_, err := collection.InsertMany(ctx, docsParaInserir)
	if err != nil {
		t.Fatalf("Setup falhou (InsertMany): %v", err)
	}

	// Find: Busca documentos com a tag "find" e ordena por idade descendente
	filter := bson.M{"tags": "find"}                                       // Filtra por documentos que contêm "find" no array "tags"
	findOptions := options.Find().SetSort(bson.D{{Key: "age", Value: -1}}) // Ordena por age DESC (-1)
	t.Logf("Buscando documentos com filtro %v e opções %v", filter, findOptions)

	cursor, err := collection.Find(ctx, filter, findOptions)
	if err != nil {
		t.Fatalf("collection.Find falhou: %v", err)
	}
	// Garante que o cursor seja fechado
	defer cursor.Close(ctx)

	// Itera e Decodifica Resultados
	var docsEncontrados []TestDoc
	t.Log("Iterando sobre o cursor...")
	for cursor.Next(ctx) {
		var doc TestDoc
		if err := cursor.Decode(&doc); err != nil {
			t.Errorf("cursor.Decode falhou: %v", err)
			// Decide se quer continuar ou falhar o teste aqui
			// continue
			t.FailNow() // Falha imediatamente se um decode falhar
		}
		docsEncontrados = append(docsEncontrados, doc)
		t.Logf("  - Decodificado: %+v", doc)
	}
	// Verifica erros do cursor após o loop
	if err := cursor.Err(); err != nil {
		t.Errorf("cursor.Err() reportou erro: %v", err)
	}
	t.Logf("Cursor finalizado. %d documentos decodificados.", len(docsEncontrados))

	// Verifica Resultados
	if len(docsEncontrados) != 3 { // Esperamos 3 documentos com a tag "find"
		t.Fatalf("Esperado encontrar 3 documentos, mas encontrou %d", len(docsEncontrados))
	}

	// Como ordenamos por 'age' descendente no Find, a ordem deve ser C, B, A.
	expectedOrderNames := []string{"Multi C", "Multi B", "Multi A"}
	for i, doc := range docsEncontrados {
		if doc.Name != expectedOrderNames[i] {
			t.Errorf("Ordem incorreta. Na posição %d, esperado Name '%s', obteve '%s'", i, expectedOrderNames[i], doc.Name)
		}
	}

	// Verificação mais robusta (opcional): comparar slices completos após ordenação se a ordem não fosse garantida
	// expectedDocs := ... (recriar a lista esperada na ordem correta)
	// if !reflect.DeepEqual(docsEncontrados, expectedDocs) { ... }

	t.Log("Verificação da quantidade e ordem dos documentos bem-sucedida.")
}
