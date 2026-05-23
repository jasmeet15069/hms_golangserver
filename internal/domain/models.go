// Package domain contains all domain entities, value objects, and business
// logic types for Hotel Harmony. Every entity maps 1-to-1 with a PostgreSQL
// table; JSON fields are represented as typed slices/maps rather than raw
// strings so the application layer never has to deserialise them manually.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type RoomStatus string

const (
	RoomStatusAvailable   RoomStatus = "available"
	RoomStatusOccupied    RoomStatus = "occupied"
	RoomStatusCleaning    RoomStatus = "cleaning"
	RoomStatusMaintenance RoomStatus = "maintenance"
)

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusPreparing OrderStatus = "preparing"
	OrderStatusReady     OrderStatus = "ready"
	OrderStatusDelivered OrderStatus = "delivered"
	OrderStatusCancelled OrderStatus = "cancelled"
)

type ComplaintStatus string

const (
	ComplaintStatusOpen       ComplaintStatus = "open"
	ComplaintStatusInProgress ComplaintStatus = "in_progress"
	ComplaintStatusResolved   ComplaintStatus = "resolved"
)

type ComplaintPriority string

const (
	ComplaintPriorityLow      ComplaintPriority = "low"
	ComplaintPriorityMedium   ComplaintPriority = "medium"
	ComplaintPriorityHigh     ComplaintPriority = "high"
	ComplaintPriorityCritical ComplaintPriority = "critical"
)

type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusCompleted PaymentStatus = "completed"
	PaymentStatusFailed    PaymentStatus = "failed"
	PaymentStatusRefunded  PaymentStatus = "refunded"
)

type UserRole string

const (
	RoleAdmin          UserRole = "admin"
	RoleSuperAdmin     UserRole = "super_admin"
	RoleFoodManager    UserRole = "food_manager"
	RoleKitchenManager UserRole = "kitchen_manager"
	RoleWaiter         UserRole = "waiter"
	RoleGuest          UserRole = "guest"
)

// User is the authentication principal.
type User struct {
	ID           uuid.UUID `db:"id" json:"id"`
	Email        string    `db:"email" json:"email"`
	PasswordHash string    `db:"password_hash" json:"-"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// Profile holds display-level information about a user.
type Profile struct {
	ID        uuid.UUID `db:"id" json:"id"`
	UserID    uuid.UUID `db:"user_id" json:"user_id"`
	FullName  string    `db:"full_name" json:"full_name"`
	Phone     *string   `db:"phone" json:"phone,omitempty"`
	AvatarURL *string   `db:"avatar_url" json:"avatar_url,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// UserRoleEntry assigns a role to a user.
type UserRoleEntry struct {
	ID        uuid.UUID `db:"id" json:"id"`
	UserID    uuid.UUID `db:"user_id" json:"user_id"`
	Role      UserRole  `db:"role" json:"role"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Room represents a physical hotel room.
type Room struct {
	ID            uuid.UUID  `db:"id" json:"id"`
	RoomNumber    string     `db:"room_number" json:"room_number"`
	RoomType      string     `db:"room_type" json:"room_type"`
	Floor         int        `db:"floor" json:"floor"`
	Capacity      int        `db:"capacity" json:"capacity"`
	PricePerNight float64    `db:"price_per_night" json:"price_per_night"`
	Status        RoomStatus `db:"status" json:"status"`
	Amenities     []string   `db:"amenities" json:"amenities"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

// GuestStay is a single booking / check-in record.
type GuestStay struct {
	ID             uuid.UUID    `db:"id" json:"id"`
	GuestID        *uuid.UUID   `db:"guest_id" json:"guest_id,omitempty"`
	RoomID         uuid.UUID    `db:"room_id" json:"room_id"`
	GuestName      string       `db:"guest_name" json:"guest_name"`
	GuestEmail     *string      `db:"guest_email" json:"guest_email,omitempty"`
	GuestPhone     *string      `db:"guest_phone" json:"guest_phone,omitempty"`
	CheckInDate    time.Time    `db:"check_in_date" json:"check_in_date"`
	CheckOutDate   time.Time    `db:"check_out_date" json:"check_out_date"`
	ActualCheckIn  *time.Time   `db:"actual_check_in" json:"actual_check_in,omitempty"`
	ActualCheckOut *time.Time   `db:"actual_check_out" json:"actual_check_out,omitempty"`
	TotalAmount    *float64     `db:"total_amount" json:"total_amount,omitempty"`
	Notes          *string      `db:"notes" json:"notes,omitempty"`
	CreatedBy      *uuid.UUID   `db:"created_by" json:"created_by,omitempty"`
	CreatedAt      time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time    `db:"updated_at" json:"updated_at"`
	Room           *RoomSummary `db:"-" json:"rooms,omitempty"`
}

// RoomSummary is a lightweight projection used for enrichment.
type RoomSummary struct {
	RoomNumber string `json:"room_number"`
	RoomType   string `json:"room_type"`
}

// MenuCategory groups menu items.
type MenuCategory struct {
	ID           uuid.UUID `db:"id" json:"id"`
	Name         string    `db:"name" json:"name"`
	Description  *string   `db:"description" json:"description,omitempty"`
	DisplayOrder int       `db:"display_order" json:"display_order"`
	IsActive     bool      `db:"is_active" json:"is_active"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// MenuItem is a single dish or drink on the menu.
type MenuItem struct {
	ID              uuid.UUID               `db:"id" json:"id"`
	CategoryID      *uuid.UUID              `db:"category_id" json:"category_id,omitempty"`
	Name            string                  `db:"name" json:"name"`
	Description     *string                 `db:"description" json:"description,omitempty"`
	Price           float64                 `db:"price" json:"price"`
	ImageURL        *string                 `db:"image_url" json:"image_url,omitempty"`
	IsAvailable     bool                    `db:"is_available" json:"is_available"`
	PreparationTime int                     `db:"preparation_time" json:"preparation_time"`
	CreatedAt       time.Time               `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time               `db:"updated_at" json:"updated_at"`
	Category        *MenuCategorySummary    `db:"-" json:"menu_categories,omitempty"`
	Customizations  []MenuItemCustomization `db:"-" json:"menu_item_customizations,omitempty"`
}

type MenuCategorySummary struct {
	Name string `json:"name"`
}

// MenuItemCustomization is a modifier/add-on for a menu item.
type MenuItemCustomization struct {
	ID          uuid.UUID `db:"id" json:"id"`
	MenuItemID  uuid.UUID `db:"menu_item_id" json:"menu_item_id"`
	Name        string    `db:"name" json:"name"`
	Price       float64   `db:"price" json:"price"`
	IsAvailable bool      `db:"is_available" json:"is_available"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// InventoryItem tracks kitchen / housekeeping stock.
type InventoryItem struct {
	ID           uuid.UUID  `db:"id" json:"id"`
	Name         string     `db:"name" json:"name"`
	Unit         string     `db:"unit" json:"unit"`
	CurrentStock float64    `db:"current_stock" json:"current_stock"`
	MinStock     float64    `db:"min_stock" json:"min_stock"`
	CostPerUnit  *float64   `db:"cost_per_unit" json:"cost_per_unit,omitempty"`
	IsPerishable bool       `db:"is_perishable" json:"is_perishable"`
	ExpiryDate   *time.Time `db:"expiry_date" json:"expiry_date,omitempty"`
	Supplier     *string    `db:"supplier" json:"supplier,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

// Recipe links a MenuItem to one or more InventoryItems.
type Recipe struct {
	ID               uuid.UUID `db:"id" json:"id"`
	MenuItemID       uuid.UUID `db:"menu_item_id" json:"menu_item_id"`
	InventoryItemID  uuid.UUID `db:"inventory_item_id" json:"inventory_item_id"`
	QuantityRequired float64   `db:"quantity_required" json:"quantity_required"`
	CreatedAt        time.Time `db:"created_at" json:"created_at"`
}

// Order is a food/beverage order from a guest.
type Order struct {
	ID                  uuid.UUID   `db:"id" json:"id"`
	OrderNumber         string      `db:"order_number" json:"order_number"`
	GuestStayID         *uuid.UUID  `db:"guest_stay_id" json:"guest_stay_id,omitempty"`
	RoomID              *uuid.UUID  `db:"room_id" json:"room_id,omitempty"`
	GuestID             *uuid.UUID  `db:"guest_id" json:"guest_id,omitempty"`
	Status              OrderStatus `db:"status" json:"status"`
	SpecialInstructions *string     `db:"special_instructions" json:"special_instructions,omitempty"`
	TotalAmount         float64     `db:"total_amount" json:"total_amount"`
	AssignedWaiterID    *uuid.UUID  `db:"assigned_waiter_id" json:"assigned_waiter_id,omitempty"`
	CreatedBy           *uuid.UUID  `db:"created_by" json:"created_by,omitempty"`
	KitchenNotes        *string     `db:"kitchen_notes" json:"kitchen_notes,omitempty"`
	PickupTime          *time.Time  `db:"pickup_time" json:"pickup_time,omitempty"`
	DeliveryTime        *time.Time  `db:"delivery_time" json:"delivery_time,omitempty"`
	Rating              *int        `db:"rating" json:"rating,omitempty"`
	Feedback            *string     `db:"feedback" json:"feedback,omitempty"`
	CreatedAt           time.Time   `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time   `db:"updated_at" json:"updated_at"`
	Room                *struct {
		RoomNumber string `json:"room_number"`
	} `db:"-" json:"rooms,omitempty"`
	GuestStay *struct {
		GuestName string `json:"guest_name"`
	} `db:"-" json:"guest_stays,omitempty"`
	Items []OrderItem `db:"-" json:"order_items,omitempty"`
}

// OrderItem is one line of an order.
type OrderItem struct {
	ID         uuid.UUID `db:"id" json:"id"`
	OrderID    uuid.UUID `db:"order_id" json:"order_id"`
	MenuItemID uuid.UUID `db:"menu_item_id" json:"menu_item_id"`
	Quantity   int       `db:"quantity" json:"quantity"`
	UnitPrice  float64   `db:"unit_price" json:"unit_price"`
	Notes      *string   `db:"notes" json:"notes,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	MenuItem   *struct {
		Name string `json:"name"`
	} `db:"-" json:"menu_items,omitempty"`
}

// StaffShift tracks clock-in / clock-out for staff.
type StaffShift struct {
	ID        uuid.UUID  `db:"id" json:"id"`
	UserID    uuid.UUID  `db:"user_id" json:"user_id"`
	ClockIn   time.Time  `db:"clock_in" json:"clock_in"`
	ClockOut  *time.Time `db:"clock_out" json:"clock_out,omitempty"`
	Notes     *string    `db:"notes" json:"notes,omitempty"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
}

// Complaint is a guest complaint record.
type Complaint struct {
	ID              uuid.UUID         `db:"id" json:"id"`
	ComplaintNumber string            `db:"complaint_number" json:"complaint_number"`
	GuestStayID     *uuid.UUID        `db:"guest_stay_id" json:"guest_stay_id,omitempty"`
	GuestID         *uuid.UUID        `db:"guest_id" json:"guest_id,omitempty"`
	Category        string            `db:"category" json:"category"`
	Priority        ComplaintPriority `db:"priority" json:"priority"`
	Status          ComplaintStatus   `db:"status" json:"status"`
	Description     string            `db:"description" json:"description"`
	Resolution      *string           `db:"resolution" json:"resolution,omitempty"`
	ResolvedBy      *uuid.UUID        `db:"resolved_by" json:"resolved_by,omitempty"`
	ResolvedAt      *time.Time        `db:"resolved_at" json:"resolved_at,omitempty"`
	GuestFeedback   *string           `db:"guest_feedback" json:"guest_feedback,omitempty"`
	CreatedBy       *uuid.UUID        `db:"created_by" json:"created_by,omitempty"`
	CreatedAt       time.Time         `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time         `db:"updated_at" json:"updated_at"`
	GuestStay       *GuestStaySummary `db:"-" json:"guest_stays,omitempty"`
}

type GuestStaySummary struct {
	GuestName string       `json:"guest_name"`
	RoomID    *uuid.UUID   `json:"room_id,omitempty"`
	Room      *RoomSummary `json:"rooms,omitempty"`
}

// Payment is a financial transaction record.
type Payment struct {
	ID            uuid.UUID     `db:"id" json:"id"`
	PaymentNumber string        `db:"payment_number" json:"payment_number"`
	GuestStayID   *uuid.UUID    `db:"guest_stay_id" json:"guest_stay_id,omitempty"`
	OrderID       *uuid.UUID    `db:"order_id" json:"order_id,omitempty"`
	Amount        float64       `db:"amount" json:"amount"`
	PaymentMethod string        `db:"payment_method" json:"payment_method"`
	Status        PaymentStatus `db:"status" json:"status"`
	ProcessedBy   *uuid.UUID    `db:"processed_by" json:"processed_by,omitempty"`
	Notes         *string       `db:"notes" json:"notes,omitempty"`
	CreatedAt     time.Time     `db:"created_at" json:"created_at"`
	Order         *struct {
		OrderNumber string `json:"order_number"`
	} `db:"-" json:"orders,omitempty"`
	GuestStay *PaymentGuestStaySummary `db:"-" json:"guest_stays,omitempty"`
}

type PaymentGuestStaySummary struct {
	GuestName string       `json:"guest_name"`
	GuestID   *uuid.UUID   `json:"guest_id,omitempty"`
	RoomID    *uuid.UUID   `json:"room_id,omitempty"`
	Room      *RoomSummary `json:"rooms,omitempty"`
}

// WasteLog records discarded inventory.
type WasteLog struct {
	ID              uuid.UUID  `db:"id" json:"id"`
	InventoryItemID uuid.UUID  `db:"inventory_item_id" json:"inventory_item_id"`
	Quantity        float64    `db:"quantity" json:"quantity"`
	Reason          string     `db:"reason" json:"reason"`
	LoggedBy        *uuid.UUID `db:"logged_by" json:"logged_by,omitempty"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
}

// AuditLog is an immutable record of every data mutation.
type AuditLog struct {
	ID        uuid.UUID              `db:"id" json:"id"`
	UserID    *uuid.UUID             `db:"user_id" json:"user_id,omitempty"`
	Action    string                 `db:"action" json:"action"`
	TableName string                 `db:"table_name" json:"table_name"`
	RecordID  *uuid.UUID             `db:"record_id" json:"record_id,omitempty"`
	OldData   map[string]interface{} `db:"old_data" json:"old_data,omitempty"`
	NewData   map[string]interface{} `db:"new_data" json:"new_data,omitempty"`
	CreatedAt time.Time              `db:"created_at" json:"created_at"`
}

// GuestPreferences stores dietary and preference data for a guest.
type GuestPreferences struct {
	ID                  uuid.UUID `db:"id" json:"id"`
	UserID              uuid.UUID `db:"user_id" json:"user_id"`
	DietaryRestrictions []string  `db:"dietary_restrictions" json:"dietary_restrictions"`
	Allergies           []string  `db:"allergies" json:"allergies"`
	FavoriteCategories  []string  `db:"favorite_categories" json:"favorite_categories"`
	Notes               *string   `db:"notes" json:"notes,omitempty"`
	CreatedAt           time.Time `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time `db:"updated_at" json:"updated_at"`
}

// PaymentSetting is a gateway configuration record.
type PaymentSetting struct {
	ID          uuid.UUID  `db:"id" json:"id"`
	GatewayName string     `db:"gateway_name" json:"gateway_name"`
	WebhookURL  *string    `db:"webhook_url" json:"webhook_url,omitempty"`
	IsActive    bool       `db:"is_active" json:"is_active"`
	CreatedBy   *uuid.UUID `db:"created_by" json:"created_by,omitempty"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`
}

// DashboardStats is the aggregated read model for the dashboard.
type DashboardStats struct {
	OccupancyRate          float64 `json:"occupancy_rate"`
	RoomsAvailable         int     `json:"rooms_available"`
	RoomsOccupied          int     `json:"rooms_occupied"`
	ActiveOrders           int     `json:"active_orders"`
	PendingComplaints      int     `json:"pending_complaints"`
	RevenueToday           float64 `json:"revenue_today"`
	LowStockItems          int     `json:"low_stock_items"`
	StaffClockedIn         int     `json:"staff_clocked_in"`
	GuestsCheckingInToday  int     `json:"guests_checking_in_today"`
	GuestsCheckingOutToday int     `json:"guests_checking_out_today"`
}

type ChatMessage struct {
	Role    string `json:"role" validate:"required,oneof=user assistant system"`
	Content string `json:"content" validate:"required"`
}

type InventoryAlert struct {
	ItemID           uuid.UUID `json:"item_id"`
	Name             string    `json:"name"`
	CurrentStock     float64   `json:"current_stock"`
	MinStock         float64   `json:"min_stock"`
	Severity         string    `json:"severity"`
	AIRecommendation string    `json:"ai_recommendation"`
	IsPerishable     bool      `json:"is_perishable"`
}
