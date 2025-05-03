// pkg/dialects/registry_test.go
package dialects

import (
	"context"
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

func (m *mockDataSource) Connect(cfg config.DatabaseConfig) error { return nil }
func (m *mockDataSource) Ping(ctx context.Context) error          { return nil }
func (m *mockDataSource) BeginTx(ctx context.Context, opts any) (common.Tx, error) {
	return nil, nil
}
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

type mockDialect struct {
	name string
}

func (m *mockDialect) Name() string                                { return m.name }
func (m *mockDialect) Quote(id string) string                      { return `"` + id + `"` }
func (m *mockDialect) BindVar(i int) string                        { return "?" }
func (m *mockDialect) GetDataType(f *schema.Field) (string, error) { return "MOCK_TYPE", nil }

// Factory function for the mock
func newMockDataSourceFactory(dialectName string) DataSourceFactory {
	return func() common.DataSource {
		return &mockDataSource{
			dialect: &mockDialect{name: dialectName},
		}
	}
}

// Helper to clean the global registry before/after tests
func cleanupRegistry(t *testing.T) {
	t.Helper()
	driversMu.Lock()
	drivers = make(map[string]DataSourceFactory) // Reset the map
	driversMu.Unlock()
}

// --- Test Functions ---

func TestRegisterAndGet(t *testing.T) {
	cleanupRegistry(t)                       // Clean up before test
	t.Cleanup(func() { cleanupRegistry(t) }) // Clean up after test

	// Arrange
	factory := newMockDataSourceFactory("mock1")

	// Act
	Register("mock1", factory)
	retrievedFactory := Get("mock1")

	// Assert
	require.NotNil(t, retrievedFactory, "Factory should be retrieved")
	assert.IsType(t, (DataSourceFactory)(nil), retrievedFactory, "Retrieved item should be a factory function")

	// Check if the factory produces the correct type
	ds := retrievedFactory()
	require.NotNil(t, ds, "Factory should produce a DataSource")
	assert.NotNil(t, ds.Dialect(), "DataSource should have a Dialect")
	assert.Equal(t, "mock1", ds.Dialect().Name(), "Dialect name should match")
}

func TestGet_NotFound(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })

	// Act
	retrievedFactory := Get("nonexistent")

	// Assert
	assert.Nil(t, retrievedFactory, "Getting a non-registered driver should return nil")
}

func TestRegister_DuplicatePanic(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })

	// Arrange
	factory1 := newMockDataSourceFactory("mock-dup")
	Register("mock-dup", factory1) // Register first time

	factory2 := newMockDataSourceFactory("mock-dup-different-impl") // A different factory

	// Act & Assert
	assert.PanicsWithValue(t, "dialects: Register called twice for driver mock-dup", func() {
		Register("mock-dup", factory2) // Register second time with the same name
	}, "Registering the same driver name twice should panic")
}

func TestRegister_NilFactoryPanic(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })

	// Act & Assert
	assert.PanicsWithValue(t, "dialects: Register factory is nil", func() {
		Register("mock-nil", nil) // Register with nil factory
	}, "Registering a nil factory should panic")
}

func TestRegisteredDrivers(t *testing.T) {
	cleanupRegistry(t)
	t.Cleanup(func() { cleanupRegistry(t) })

	// Assert initial state (empty)
	assert.Empty(t, RegisteredDrivers(), "Initially, no drivers should be registered")

	// Arrange
	factoryA := newMockDataSourceFactory("mockA")
	factoryB := newMockDataSourceFactory("mockB")
	Register("mockA", factoryA)
	Register("mockB", factoryB)

	// Act
	driverList := RegisteredDrivers()

	// Assert
	require.Len(t, driverList, 2, "Should list 2 registered drivers")
	// Use ElementsMatch because the order is not guaranteed
	assert.ElementsMatch(t, []string{"mockA", "mockB"}, driverList, "List should contain the registered driver names")
}
