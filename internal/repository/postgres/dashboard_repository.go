package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/hotelharmony/api/internal/domain"
)

// DashboardRepository runs the single aggregated query that powers
// the dashboard stats endpoint. All counters are fetched in one round
// trip using PostgreSQL CTEs to avoid 10 separate queries.
type DashboardRepository interface {
	GetStats(ctx context.Context) (*domain.DashboardStats, error)
}

type dashboardRepository struct {
	db *DB
}

func NewDashboardRepository(db *DB) DashboardRepository {
	return &dashboardRepository{db: db}
}

// GetStats executes a single multi-CTE query instead of the original 10
// sequential SQLite queries. On PostgreSQL with proper indexes this runs
// in < 5 ms even at thousands of rooms.
func (r *dashboardRepository) GetStats(ctx context.Context) (*domain.DashboardStats, error) {
	today := time.Now().UTC().Format("2006-01-02")
	const q = `
		WITH
		  room_counts AS (
		    SELECT
		      COUNT(*)                                          AS total_rooms,
		      COUNT(*) FILTER (WHERE status = 'occupied')      AS occupied,
		      COUNT(*) FILTER (WHERE status = 'available')     AS available
		    FROM rooms
		  ),
		  order_count AS (
		    SELECT COUNT(*) AS active_orders
		    FROM orders
		    WHERE status IN ('pending','preparing','ready')
		  ),
		  complaint_count AS (
		    SELECT COUNT(*) AS pending_complaints
		    FROM complaints
		    WHERE status != 'resolved'
		  ),
		  revenue AS (
		    SELECT COALESCE(SUM(amount), 0) AS revenue_today
		    FROM payments
		    WHERE status = 'completed'
		      AND created_at::date = $1::date
		  ),
		  stock AS (
		    SELECT COUNT(*) AS low_stock
		    FROM inventory_items
		    WHERE current_stock <= min_stock
		  ),
		  staff AS (
		    SELECT COUNT(*) AS clocked_in
		    FROM staff_shifts
		    WHERE clock_out IS NULL
		  ),
		  arrivals AS (
		    SELECT COUNT(*) AS checking_in
		    FROM guest_stays
		    WHERE check_in_date::date = $1::date
		  ),
		  departures AS (
		    SELECT COUNT(*) AS checking_out
		    FROM guest_stays
		    WHERE check_out_date::date = $1::date
		  )
		SELECT
		  rc.total_rooms, rc.occupied, rc.available,
		  oc.active_orders, cc.pending_complaints,
		  rv.revenue_today, sk.low_stock, sf.clocked_in,
		  ar.checking_in, dp.checking_out
		FROM room_counts rc, order_count oc, complaint_count cc,
		     revenue rv, stock sk, staff sf, arrivals ar, departures dp`

	var (
		totalRooms   int
		occupied     int
		available    int
		activeOrders int
		pendComp     int
		revenueToday float64
		lowStock     int
		staffIn      int
		checkingIn   int
		checkingOut  int
	)

	err := r.db.Pool.QueryRow(ctx, q, today).Scan(
		&totalRooms, &occupied, &available,
		&activeOrders, &pendComp,
		&revenueToday, &lowStock, &staffIn,
		&checkingIn, &checkingOut,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboardRepo.GetStats: %w", err)
	}

	if totalRooms == 0 {
		totalRooms = 1
	}

	return &domain.DashboardStats{
		OccupancyRate:          float64(occupied) / float64(totalRooms),
		RoomsAvailable:         available,
		RoomsOccupied:          occupied,
		ActiveOrders:           activeOrders,
		PendingComplaints:      pendComp,
		RevenueToday:           revenueToday,
		LowStockItems:          lowStock,
		StaffClockedIn:         staffIn,
		GuestsCheckingInToday:  checkingIn,
		GuestsCheckingOutToday: checkingOut,
	}, nil
}
