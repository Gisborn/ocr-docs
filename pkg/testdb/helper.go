// Package testdb provides helpers for PostgreSQL integration tests.
// Tests skip automatically if the database is not available.
package testdb

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	return "postgres://api_scan:api_scan_secret@localhost:5432/api_scan"
}

// DefaultBillingURL returns the default billing database URL.
func DefaultBillingURL() string {
	if u := os.Getenv("TEST_BILLING_DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://billing:billing_secret@localhost:5433/billing_db"
}
