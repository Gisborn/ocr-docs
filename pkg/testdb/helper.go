// Package testdb provides helpers for PostgreSQL integration tests.
// Tests skip automatically if the database is not available.
package testdb

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SetupTestDB creates an ephemeral schema, applies migrations, and returns a pool.
// Cleanup drops the schema automatically via t.Cleanup.
func SetupTestDB(t *testing.T, databaseURL, migrationsDir string) *pgxpool.Pool {
	t.Helper()

	schemaName := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), rand.Int31())

	// Connect to admin schema (public) to create the test schema
	adminPool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Skipf("Cannot create admin pool: %v", err)
	}
	defer adminPool.Close()

	ctx := context.Background()
	if err := adminPool.Ping(ctx); err != nil {
		t.Skipf("Database not available: %v", err)
	}

	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	if err != nil {
		t.Fatalf("create schema %s: %v", schemaName, err)
	}

	// Build a pool that sets search_path on every new connection
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s", schemaName))
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	// Apply migrations inside the ephemeral schema
	ApplyMigrations(t, pool, migrationsDir)

	// Register cleanup: close pool then drop schema
	t.Cleanup(func() {
		pool.Close()

		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		tempPool, err := pgxpool.New(cleanupCtx, databaseURL)
		if err != nil {
			return
		}
		defer tempPool.Close()

		_, _ = tempPool.Exec(cleanupCtx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
	})

	return pool
}

// MustPool returns a connection pool or skips the test.
func MustPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set, skipping SQL test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("Cannot create pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Database not available: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// ApplyMigrations runs all .sql files in dir against the pool.
// Only the "Up" section (between -- +goose Up and -- +goose Down) is executed.
func ApplyMigrations(t *testing.T, pool *pgxpool.Pool, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir %s: %v", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}

		sql := extractUpSection(string(content))
		if strings.TrimSpace(sql) == "" {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err = pool.Exec(ctx, sql)
		cancel()
		if err != nil {
			t.Fatalf("apply migration %s: %v", path, err)
		}
	}
}

// extractUpSection returns SQL between "-- +goose Up" and "-- +goose Down".
func extractUpSection(content string) string {
	var lines []string
	inUp := false

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "-- +goose Up") {
			inUp = true
			continue
		}
		if strings.HasPrefix(trimmed, "-- +goose Down") {
			break
		}
		if !inUp {
			continue
		}
		if strings.HasPrefix(trimmed, "-- +goose StatementBegin") || strings.HasPrefix(trimmed, "-- +goose StatementEnd") {
			continue
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// Cleanup truncates the given tables and resets their sequences.
func Cleanup(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			t.Logf("cleanup truncate %s: %v", table, err)
		}
	}
}

// DefaultMainURL returns the default main database URL.
func DefaultMainURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://api_scan:api_scan_secret@localhost:15432/api_scan"
}

// DefaultBillingURL returns the default billing database URL.
func DefaultBillingURL() string {
	if u := os.Getenv("TEST_BILLING_DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://billing:billing_secret@localhost:15433/billing_db"
}
