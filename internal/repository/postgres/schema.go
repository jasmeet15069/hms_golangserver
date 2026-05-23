package postgres

import (
	"context"
	"fmt"
)

// EnsureAppSchema creates compatibility tables that older local databases may
// be missing. It is intentionally additive and safe to run on every boot.
func (d *DB) EnsureAppSchema(ctx context.Context) error {
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
		`INSERT INTO payment_settings (id, gateway_name, webhook_url, is_active, created_at, updated_at)
		 VALUES
		   (uuid_generate_v4(), 'cash', NULL, true, now(), now()),
		   (uuid_generate_v4(), 'card', NULL, true, now(), now()),
		   (uuid_generate_v4(), 'stripe', NULL, true, now(), now())
		 ON CONFLICT (gateway_name) DO NOTHING`,
	}
	for _, statement := range statements {
		if _, err := d.Pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}
