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
		`CREATE TABLE IF NOT EXISTS lost_items (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			room_id UUID REFERENCES rooms(id) ON DELETE SET NULL,
			guest_name TEXT,
			item_name TEXT NOT NULL,
			description TEXT,
			found_by UUID REFERENCES users(id) ON DELETE SET NULL,
			found_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			status VARCHAR(20) NOT NULL DEFAULT 'lost',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS linen_inventory (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			item_name TEXT NOT NULL,
			total_count INT NOT NULL DEFAULT 0,
			in_use INT NOT NULL DEFAULT 0,
			in_laundry INT NOT NULL DEFAULT 0,
			damaged INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS linen_transactions (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			linen_id UUID NOT NULL REFERENCES linen_inventory(id) ON DELETE CASCADE,
			transaction_type VARCHAR(10) NOT NULL,
			quantity INT NOT NULL,
			damaged INT NOT NULL DEFAULT 0,
			issued_to TEXT,
			notes TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO payment_settings (id, hotel_id, gateway_name, webhook_url, is_active, created_at, updated_at)
		 VALUES
		   (uuid_generate_v4(), '00000000-0000-0000-0000-000000000001', 'cash', NULL, true, now(), now()),
		   (uuid_generate_v4(), '00000000-0000-0000-0000-000000000001', 'card', NULL, true, now(), now()),
		   (uuid_generate_v4(), '00000000-0000-0000-0000-000000000001', 'stripe', NULL, true, now(), now())
		 ON CONFLICT (gateway_name) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS guests (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			full_name TEXT NOT NULL,
			email TEXT,
			phone TEXT,
			address TEXT,
			city TEXT,
			country TEXT,
			id_type TEXT,
			id_number TEXT,
			vip_status TEXT DEFAULT 'standard',
			notes TEXT,
			preferences JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS loyalty_tiers (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			min_points INT NOT NULL DEFAULT 0,
			multiplier NUMERIC(5,2) NOT NULL DEFAULT 1.0,
			benefits JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS loyalty_members (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			guest_id UUID NOT NULL REFERENCES guests(id) ON DELETE CASCADE,
			tier_id UUID REFERENCES loyalty_tiers(id) ON DELETE SET NULL,
			points INT NOT NULL DEFAULT 0,
			lifetime_points INT NOT NULL DEFAULT 0,
			enrolled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(guest_id, hotel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS loyalty_transactions (
			id UUID PRIMARY KEY,
			member_id UUID NOT NULL REFERENCES loyalty_members(id) ON DELETE CASCADE,
			type TEXT NOT NULL,
			points INT NOT NULL,
			reference TEXT,
			description TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS pricing_rules (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			rule_type TEXT NOT NULL,
			conditions JSONB DEFAULT '{}',
			adjustment NUMERIC(5,2) NOT NULL DEFAULT 0,
			priority INT NOT NULL DEFAULT 0,
			active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS vendors (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			contact_person TEXT,
			email TEXT,
			phone TEXT,
			address TEXT,
			category TEXT,
			rating NUMERIC(3,1),
			active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS purchase_orders (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			vendor_id UUID REFERENCES vendors(id) ON DELETE SET NULL,
			po_number TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			items JSONB DEFAULT '[]',
			total NUMERIC(12,2) NOT NULL DEFAULT 0,
			notes TEXT,
			issued_at TIMESTAMPTZ,
			received_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS channel_connections (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			channel_name TEXT NOT NULL,
			channel_type TEXT NOT NULL,
			api_key TEXT,
			settings JSONB DEFAULT '{}',
			connected BOOLEAN NOT NULL DEFAULT false,
			last_sync_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS night_audit_reports (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			audit_date DATE NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			expected_revenue NUMERIC(12,2),
			actual_revenue NUMERIC(12,2),
			total_tax NUMERIC(12,2),
			occupancy_rate NUMERIC(5,2),
			notes TEXT,
			closed_by UUID REFERENCES users(id) ON DELETE SET NULL,
			closed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS promotions (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			code TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			discount_type TEXT NOT NULL,
			discount_value NUMERIC(12,2) NOT NULL DEFAULT 0,
			min_nights INT DEFAULT 0,
			min_amount NUMERIC(12,2) DEFAULT 0,
			max_discount NUMERIC(12,2),
			usage_limit INT DEFAULT 0,
			used_count INT NOT NULL DEFAULT 0,
			valid_from DATE NOT NULL,
			valid_to DATE NOT NULL,
			active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS assets (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			category TEXT,
			location TEXT,
			serial_number TEXT,
			purchase_date DATE,
			purchase_cost NUMERIC(12,2),
			warranty_until DATE,
			status TEXT NOT NULL DEFAULT 'active',
			notes TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS revenue_daily (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			date DATE NOT NULL,
			total NUMERIC(12,2) NOT NULL DEFAULT 0,
			revpar NUMERIC(12,2) NOT NULL DEFAULT 0,
			adr NUMERIC(12,2) NOT NULL DEFAULT 0,
			occupancy_pct NUMERIC(5,2) NOT NULL DEFAULT 0,
			goppar NUMERIC(12,2) NOT NULL DEFAULT 0,
			UNIQUE(hotel_id, date)
		)`,
		`CREATE TABLE IF NOT EXISTS revenue_forecast (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			date DATE NOT NULL,
			occupancy_pct NUMERIC(5,2) NOT NULL DEFAULT 0,
			revenue NUMERIC(12,2) NOT NULL DEFAULT 0,
			UNIQUE(hotel_id, date)
		)`,
		`CREATE TABLE IF NOT EXISTS bookings (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			guest_id UUID REFERENCES guests(id) ON DELETE SET NULL,
			room_id UUID REFERENCES rooms(id) ON DELETE SET NULL,
			status TEXT NOT NULL DEFAULT 'confirmed',
			check_in DATE,
			check_out DATE,
			total NUMERIC(12,2) NOT NULL DEFAULT 0,
			tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
			channel_name TEXT DEFAULT 'direct',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS competitor_rates (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			competitor_name TEXT NOT NULL,
			room_type TEXT NOT NULL,
			our_rate NUMERIC(12,2) NOT NULL DEFAULT 0,
			their_rate NUMERIC(12,2) NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS maintenance_schedule (
			id UUID PRIMARY KEY,
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			asset_id UUID REFERENCES assets(id) ON DELETE CASCADE,
			task_name TEXT NOT NULL,
			frequency TEXT NOT NULL,
			last_done DATE,
			next_due DATE NOT NULL,
			assigned_to TEXT,
			notes TEXT,
			completed BOOLEAN NOT NULL DEFAULT false,
			completed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
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

	// Ledger of applied migrations so we never re-run DDL on every boot.
	if _, err := d.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("schema: ensure ledger: %w", err)
	}

	applied := map[string]bool{}
	rows, err := d.Pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("schema: read ledger: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("schema: scan ledger: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("schema: ledger rows: %w", err)
	}

	// NOTE: We deliberately do NOT auto-baseline. A previous version recorded
	// every migration as applied whenever public.users existed, on the
	// assumption that the presence of one core table proved the whole schema
	// was provisioned. That assumption was false on partially-migrated
	// databases (e.g. only 001 applied): it silently skipped real migrations
	// such as the one that creates `hotels`, leaving the schema broken while
	// the ledger claimed it was complete. Every migration here is idempotent
	// (DDL uses IF [NOT] EXISTS, seeds use ON CONFLICT), so it is always safe
	// to run any not-yet-recorded migration. A migration is only marked
	// applied after its DDL has actually executed below.
	for _, file := range files {
		version := filepath.Base(file)
		if applied[version] {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("schema: read %s: %w", file, err)
		}
		tx, err := d.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("schema: begin %s: %w", version, err)
		}
		for _, statement := range splitSQLStatements(string(body)) {
			if _, err := tx.Exec(ctx, statement); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("schema: migration %s failed: %w\nstatement: %s", version, err, statement)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("schema: record %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("schema: commit %s: %w", version, err)
		}
		d.logger.Info("schema: applied migration " + version)
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
