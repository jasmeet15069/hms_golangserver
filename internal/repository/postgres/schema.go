package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EnsureAppSchema creates compatibility tables that older local databases may
// be missing. It is intentionally additive and safe to run on every boot.
func (d *DB) EnsureAppSchema(ctx context.Context) error {
	if err := d.runSQLMigrations(ctx); err != nil {
		return err
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS order_items (
			id UUID PRIMARY KEY,
			order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
			quantity INTEGER NOT NULL DEFAULT 1,
			unit_price NUMERIC(12,2) NOT NULL DEFAULT 0,
			notes TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id UUID PRIMARY KEY,
			user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			action TEXT NOT NULL,
			table_name TEXT NOT NULL,
			record_id UUID,
			old_data JSONB,
			new_data JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items(order_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC)`,
		`INSERT INTO payment_settings (id, hotel_id, gateway_name, webhook_url, is_active, created_at, updated_at)
		 VALUES
		   (uuid_generate_v4(), '00000000-0000-0000-0000-000000000001', 'cash', NULL, true, now(), now()),
		   (uuid_generate_v4(), '00000000-0000-0000-0000-000000000001', 'card', NULL, true, now(), now()),
		   (uuid_generate_v4(), '00000000-0000-0000-0000-000000000001', 'stripe', NULL, true, now(), now())
		 ON CONFLICT (gateway_name) DO NOTHING`,
	}
	for _, statement := range statements {
		if _, err := d.Pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func (d *DB) runSQLMigrations(ctx context.Context) error {
	dir, err := findMigrationsDir()
	if err != nil {
		d.logger.Warn("schema: migrations directory not found")
		return nil
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("schema: list migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("schema: read %s: %w", file, err)
		}
		for _, statement := range splitSQLStatements(string(body)) {
			if _, err := d.Pool.Exec(ctx, statement); err != nil {
				return fmt.Errorf("schema: migration %s failed: %w\nstatement: %s", filepath.Base(file), err, statement)
			}
		}
	}
	return nil
}

func findMigrationsDir() (string, error) {
	candidates := []string{
		"migrations",
		filepath.Join("..", "migrations"),
		filepath.Join("..", "..", "migrations"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}

func splitSQLStatements(sql string) []string {
	parts := strings.Split(sql, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		lines := strings.Split(part, "\n")
		cleanLines := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			cleanLines = append(cleanLines, line)
		}
		statement := strings.TrimSpace(strings.Join(cleanLines, "\n"))
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
