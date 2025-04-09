// driver/sqlite/sqlite.go
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	// Needed for context timeout
	"github.com/chmenegatti/typegorm"
	_ "github.com/mattn/go-sqlite3"
)

var _ typegorm.DataSource = (*SQLiteDataSource)(nil)

type Config struct {
	Database string            `json:"database" yaml:"database"`
	Options  map[string]string `json:"options" yaml:"options"`
}

type SQLiteDataSource struct {
	config Config
	db     *sql.DB
	connMu sync.Mutex
}

func init() {
	fmt.Println("driver/sqlite: Initialized (running init function).") // LOG Adicionado
}

func NewDataSource() *SQLiteDataSource {
	return &SQLiteDataSource{}
}

// Connect implements the typegorm.DataSource.Connect method.
func (s *SQLiteDataSource) Connect(cfg typegorm.Config) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] sqlite.Connect: Entered function, mutex acquired.") // LOG Adicionado

	if s.db != nil {
		fmt.Println("[LOG] sqlite.Connect: Connection already established.") // LOG Adicionado
		return errors.New("sqlite: connection already established")
	}

	sqliteConfig, ok := cfg.(Config)
	// ... (type assertion logic remains the same) ...
	if !ok {
		fmt.Println("[LOG] sqlite.Connect: Invalid configuration type.") // LOG Adicionado
		return errors.New("sqlite: invalid configuration provided, expected sqlite.Config or *sqlite.Config")
	}
	s.config = sqliteConfig

	if s.config.Database == "" {
		fmt.Println("[LOG] sqlite.Connect: Database path is empty.") // LOG Adicionado
		return errors.New("sqlite: database path (Database) cannot be empty in config")
	}

	// Assemble the Data Source Name (DSN)
	dsn := s.config.Database
	// ... (DSN assembly logic remains the same) ...
	fmt.Printf("[LOG] sqlite.Connect: Assembled DSN: %s\n", dsn) // LOG Adicionado

	// Open the database connection
	fmt.Println("[LOG] sqlite.Connect: Calling sql.Open()...") // LOG Adicionado
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		fmt.Printf("[LOG] sqlite.Connect: sql.Open() failed: %v\n", err) // LOG Adicionado
		return fmt.Errorf("sqlite: failed to prepare connection: %w", err)
	}
	fmt.Println("[LOG] sqlite.Connect: sql.Open() successful.") // LOG Adicionado

	// Configure connection pool settings
	db.SetMaxOpenConns(1)
	s.db = db
	fmt.Println("[LOG] sqlite.Connect: *sql.DB assigned to struct field.") // LOG Adicionado

	// --- PONTO CRÍTICO: Ping Interno ---
	// Vamos remover temporariamente o ping de dentro do Connect para isolar o problema.
	// Se o teste passar sem isso, o problema está no Ping (ou como ele interage logo após Open).
	/*
		fmt.Println("[LOG] sqlite.Connect: Calling internal Ping() for verification...") // LOG Adicionado
		pingTimeout := 10 * time.Second // Timeout mais longo para depuração
		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		// IMPORTANTE: O defer cancel() aqui só seria chamado no fim do Connect,
		// o que pode ser tarde demais se o Ping travar. Idealmente, o contexto
		// deveria ser gerenciado externamente ou o Ping não deveria ser chamado aqui.
		// Removendo a chamada por enquanto:
		// pingErr := s.Ping(ctx)
		// cancel() // Cancela imediatamente após o ping (se fosse chamado)

		// if pingErr != nil {
		//     fmt.Printf("[LOG] sqlite.Connect: Internal Ping() failed: %v\n", pingErr) // LOG Adicionado
		//     s.db.Close()
		//     s.db = nil
		//     return fmt.Errorf("sqlite: failed to verify connection after opening (%s): %w", dsn, pingErr)
		// }
		// fmt.Println("[LOG] sqlite.Connect: Internal Ping() successful.") // LOG Adicionado
	*/
	// --- Fim do Ping Interno Removido ---

	fmt.Printf("[LOG] sqlite.Connect: Connection configured successfully for %s\n", s.config.Database)
	fmt.Println("[LOG] sqlite.Connect: Exiting function, mutex released.") // LOG Adicionado
	return nil
}

// Close implements the typegorm.DataSource.Close method.
func (s *SQLiteDataSource) Close() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	fmt.Println("[LOG] sqlite.Close: Entered function, mutex acquired.") // LOG Adicionado

	if s.db == nil {
		fmt.Println("[LOG] sqlite.Close: Connection not established or already closed.") // LOG Adicionado
		return errors.New("sqlite: connection not established or already closed")
	}
	fmt.Println("[LOG] sqlite.Close: Calling s.db.Close()...") // LOG Adicionado
	err := s.db.Close()
	dbRef := s.db // Salva referência para log
	s.db = nil    // Clear the reference *after* trying to close
	if err != nil {
		fmt.Printf("[LOG] sqlite.Close: s.db.Close() failed: %v (db ref: %p)\n", err, dbRef) // LOG Adicionado
		return fmt.Errorf("sqlite: error closing connection: %w", err)
	}
	fmt.Printf("[LOG] sqlite.Close: s.db.Close() successful. (db ref: %p)\n", dbRef) // LOG Adicionado
	fmt.Println("[LOG] sqlite.Close: Exiting function, mutex released.")             // LOG Adicionado
	return nil
}

// Ping implements the typegorm.DataSource.Ping method.
func (s *SQLiteDataSource) Ping(ctx context.Context) error {
	fmt.Println("[LOG] sqlite.Ping: Entered function.") // LOG Adicionado
	s.connMu.Lock()
	db := s.db
	s.connMu.Unlock()
	fmt.Printf("[LOG] sqlite.Ping: Mutex acquired/released. db reference: %p\n", db) // LOG Adicionado

	if db == nil {
		fmt.Println("[LOG] sqlite.Ping: Connection not established (db is nil).") // LOG Adicionado
		return errors.New("sqlite: connection not established")
	}

	fmt.Println("[LOG] sqlite.Ping: Calling db.PingContext()...") // LOG Adicionado
	err := db.PingContext(ctx)
	if err != nil {
		// Check specifically for context deadline exceeded
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Println("[LOG] sqlite.Ping: db.PingContext() failed: context deadline exceeded.") // LOG Adicionado
		} else {
			fmt.Printf("[LOG] sqlite.Ping: db.PingContext() failed: %v\n", err) // LOG Adicionado
		}
		return err // Return original error
	}
	fmt.Println("[LOG] sqlite.Ping: db.PingContext() successful.") // LOG Adicionado
	return nil
}

// GetDriverType implements the typegorm.DataSource.GetDriverType method.
func (s *SQLiteDataSource) GetDriverType() typegorm.DriverType {
	return typegorm.SQLite
}

// GetDB implements the typegorm.DataSource.GetDB method.
func (s *SQLiteDataSource) GetDB() (*sql.DB, error) {
	// Read s.db safely
	s.connMu.Lock()
	db := s.db
	s.connMu.Unlock()

	if db == nil {
		return nil, errors.New("sqlite: connection not established")
	}
	return db, nil
}

// GetNativeConnection implements the typegorm.DataSource.GetNativeConnection method.
func (s *SQLiteDataSource) GetNativeConnection() (interface{}, error) {
	// For SQLite using database/sql, the most relevant "native" connection is the *sql.DB itself.
	return s.GetDB()
}
