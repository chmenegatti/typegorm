// pkg/dialects/registry_test.go
package dialects

import (
	"context"
	"fmt" // Added fmt for mock SQL strings
	"testing"

	"github.com/chmenegatti/typegorm/pkg/config"
	"github.com/chmenegatti/typegorm/pkg/dialects/common"
	"github.com/chmenegatti/typegorm/pkg/schema" // Import schema for mock dialect signature
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mocks/Stubs for testing ---
type mockDataSource struct {
	dialect common.Dialect
}

func (m *mockDataSource) Connect(cfg config.DatabaseConfig) error                  { return nil }
func (m *mockDataSource) Ping(ctx context.Context) error                           { return nil }
func (m *mockDataSource) BeginTx(ctx context.Context, opts any) (common.Tx, error) { return nil, nil }
func (m *mockDataSource) Exec(ctx context.Context, query string, args ...any) (common.Result, error) {
	return nil, nil
}
func (m *mockDataSource) QueryRow(ctx context.Context, query string, args ...any) common.RowScanner {
	return nil
}
func (m *mockDataSource) Query(ctx context.Context, query string, args ...any) (common.Rows, error) {
	return nil, nil
}
func (m *mockDataSource) Close() error            { return nil }
func (m *mockDataSource) Dialect() common.Dialect { return m.dialect }

type mockDialect struct{ name string }

func (m *mockDialect) Name() string                                { return m.name }
func (m *mockDialect) Quote(id string) string                      { return `"` + id + `"` }
func (m *mockDialect) BindVar(i int) string                        { return fmt.Sprintf("$%d", i) }
func (m *mockDialect) GetDataType(f *schema.Field) (string, error) { return "MOCK_TYPE", nil }
func (m *mockDialect) CreateSchemaMigrationsTableSQL(tableName string) string {
	return fmt.Sprintf("CREATE TABLE %s (id TEXT, applied_at TEXT);", m.Quote(tableName))
}
func (m *mockDialect) GetAppliedMigrationsSQL(tableName string) string {
	return fmt.Sprintf("SELECT id, applied_at FROM %s;", m.Quote(tableName))
}
func (m *mockDialect) InsertMigrationSQL(tableName string) string {
	return fmt.Sprintf("INSERT INTO %s (id, applied_at) VALUES (%s, %s);", m.Quote(tableName), m.BindVar(1), m.BindVar(2))
}
func (m *mockDialect) DeleteMigrationSQL(tableName string) string {
	return fmt.Sprintf("DELETE FROM %s WHERE id = %s;", m.Quote(tableName), m.BindVar(1))
}

var _ common.Dialect = (*mockDialect)(nil)

func newMockDataSourceFactory(dialectName string) DataSourceFactory {
	return func() common.DataSource {
		return &mockDataSource{
			dialect: &mockDialect{name: dialectName},
		}
	}
}
func cleanupRegistry(t *testing.T) {
	t.Helper()
	driversMu.Lock()
	drivers = make(map[string]DataSourceFactory)
	driversMu.Unlock()
}

// --- Test Functions ---

func TestRegisterAndGet(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })

	// Arrange
	factory := newMockDataSourceFactory("mock1")

	// Act
	Register("mock1", factory)
	// *** CORRECTED: Use single return value assignment ***
	retrievedFactory := Get("mock1")

	// Assert
	// *** CORRECTED: Check for nil directly ***
	require.NotNil(t, retrievedFactory, "Factory 'mock1' should be found (not nil)")
	assert.IsType(t, (DataSourceFactory)(nil), retrievedFactory, "Retrieved item should be a factory function")

	// Check if the factory produces the correct type
	ds := retrievedFactory()
	require.NotNil(t, ds, "Factory should produce a DataSource")
	require.NotNil(t, ds.Dialect(), "DataSource should have a Dialect")
	assert.Equal(t, "mock1", ds.Dialect().Name(), "Dialect name should match")
}

func TestGet_NotFound(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })

	// Act
	// *** CORRECTED: Use single return value assignment ***
	retrievedFactory := Get("nonexistent")

	// Assert
	// *** CORRECTED: Check for nil directly ***
	assert.Nil(t, retrievedFactory, "Getting a non-registered driver should return nil factory")
}

// (TestRegister_DuplicatePanic, TestRegister_NilFactoryPanic, TestRegisteredDrivers remain the same as they were correct)
func TestRegister_DuplicatePanic(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })
	factory1 := newMockDataSourceFactory("mock-dup")
	Register("mock-dup", factory1)
	factory2 := newMockDataSourceFactory("mock-dup-different-impl")
	assert.PanicsWithValue(t, "dialects: Register called twice for driver mock-dup", func() {
		Register("mock-dup", factory2)
	}, "Registering the same driver name twice should panic")
}

func TestRegister_NilFactoryPanic(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })
	assert.PanicsWithValue(t, "dialects: Register factory is nil", func() {
		Register("mock-nil", nil)
	}, "Registering a nil factory should panic")
}

func TestRegisteredDrivers(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })
	assert.Empty(t, RegisteredDrivers(), "Initially, no drivers should be registered")
	factoryA := newMockDataSourceFactory("mockA")
	factoryB := newMockDataSourceFactory("mockB")
	Register("mockA", factoryA)
	Register("mockB", factoryB)
	driverList := RegisteredDrivers()
	require.Len(t, driverList, 2, "Should list 2 registered drivers")
	assert.ElementsMatch(t, []string{"mockA", "mockB"}, driverList, "List should contain the registered driver names")
}
