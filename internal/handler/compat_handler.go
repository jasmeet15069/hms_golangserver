package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hotelharmony/api/pkg/response"
)

type CompatHandler struct {
	pool *pgxpool.Pool
}

func NewCompatHandler(pool *pgxpool.Pool) *CompatHandler {
	return &CompatHandler{pool: pool}
}

func (h *CompatHandler) Register(r fiber.Router) {
	r.Get("/tables/:table", h.Select)
	r.Post("/tables/:table", h.Insert)
	r.Patch("/tables/:table", h.Update)
	r.Delete("/tables/:table", h.Delete)
}

type compatFilter struct {
	Column   string      `json:"column"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

type compatMutation struct {
	Values  interface{}    `json:"values"`
	Filters []compatFilter `json:"filters"`
	Single  string         `json:"single"`
}

func (h *CompatHandler) Select(c *fiber.Ctx) error {
	table := c.Params("table")
	filters := parseCompatFilters(c.Query("filters"))

	switch table {
	case "profiles":
		return h.selectProfiles(c, filters)
	case "user_roles":
		return h.selectUserRoles(c, filters)
	case "rooms":
		return h.selectRooms(c, filters)
	case "guest_stays":
		return h.selectGuestStays(c, filters)
	default:
		return response.Error(c, fiber.StatusNotFound, fmt.Sprintf("unsupported compatibility table: %s", table))
	}
}

func (h *CompatHandler) Insert(c *fiber.Ctx) error {
	table := c.Params("table")
	payload, err := parseMutation(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	values, ok := firstValueMap(payload.Values)
	if !ok {
		return response.Error(c, fiber.StatusBadRequest, "insert values are required")
	}

	switch table {
	case "rooms":
		return h.insertRoom(c, values)
	case "guest_stays":
		return h.insertGuestStay(c, values)
	default:
		return response.Error(c, fiber.StatusNotFound, fmt.Sprintf("unsupported compatibility insert table: %s", table))
	}
}

func (h *CompatHandler) Update(c *fiber.Ctx) error {
	table := c.Params("table")
	payload, err := parseMutation(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	values, ok := firstValueMap(payload.Values)
	if !ok {
		return response.Error(c, fiber.StatusBadRequest, "update values are required")
	}
	id, ok := stringFilter(payload.Filters, "id")
	if !ok || id == "" {
		return response.Error(c, fiber.StatusBadRequest, "id filter is required")
	}

	switch table {
	case "rooms":
		return h.updateRoom(c, id, values)
	case "guest_stays":
		return h.updateGuestStay(c, id, values)
	default:
		return response.Error(c, fiber.StatusNotFound, fmt.Sprintf("unsupported compatibility update table: %s", table))
	}
}

func (h *CompatHandler) Delete(c *fiber.Ctx) error {
	table := c.Params("table")
	payload, err := parseMutation(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	id, ok := stringFilter(payload.Filters, "id")
	if !ok || id == "" {
		return response.Error(c, fiber.StatusBadRequest, "id filter is required")
	}

	switch table {
	case "rooms":
		if _, err := h.pool.Exec(c.Context(), `DELETE FROM rooms WHERE id = $1`, id); err != nil {
			return response.Error(c, fiber.StatusBadRequest, err.Error())
		}
		return response.OK(c, []map[string]interface{}{})
	case "guest_stays":
		if _, err := h.pool.Exec(c.Context(), `DELETE FROM guest_stays WHERE id = $1`, id); err != nil {
			return response.Error(c, fiber.StatusBadRequest, err.Error())
		}
		return response.OK(c, []map[string]interface{}{})
	default:
		return response.Error(c, fiber.StatusNotFound, fmt.Sprintf("unsupported compatibility delete table: %s", table))
	}
}

func parseCompatFilters(raw string) []compatFilter {
	if raw == "" {
		return nil
	}
	var filters []compatFilter
	_ = json.Unmarshal([]byte(raw), &filters)
	return filters
}

func parseMutation(c *fiber.Ctx) (*compatMutation, error) {
	var payload compatMutation
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return nil, err
	}
	if payload.Filters == nil {
		payload.Filters = parseCompatFilters(c.Query("filters"))
	}
	return &payload, nil
}

func filterValue(filters []compatFilter, column string) (interface{}, bool) {
	for _, f := range filters {
		if f.Column == column && f.Operator == "eq" {
			return f.Value, true
		}
	}
	return nil, false
}

func stringFilter(filters []compatFilter, column string) (string, bool) {
	v, ok := filterValue(filters, column)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func firstValueMap(values interface{}) (map[string]interface{}, bool) {
	switch v := values.(type) {
	case map[string]interface{}:
		return v, true
	case []interface{}:
		if len(v) == 0 {
			return nil, false
		}
		m, ok := v[0].(map[string]interface{})
		return m, ok
	default:
		return nil, false
	}
}

func singleMode(c *fiber.Ctx) string {
	return c.Query("single")
}

func (h *CompatHandler) selectProfiles(c *fiber.Ctx, filters []compatFilter) error {
	q := `SELECT id, user_id, full_name, phone, avatar_url, created_at, updated_at FROM profiles`
	args := []interface{}{}
	if v, ok := filterValue(filters, "user_id"); ok {
		q += " WHERE user_id = $1"
		args = append(args, v)
	}
	q += " ORDER BY created_at DESC"

	rows, err := h.pool.Query(c.Context(), q, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, userID, fullName string
		var phone, avatarURL *string
		var createdAt, updatedAt interface{}
		if err := rows.Scan(&id, &userID, &fullName, &phone, &avatarURL, &createdAt, &updatedAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, err.Error())
		}
		items = append(items, map[string]interface{}{
			"id":         id,
			"user_id":    userID,
			"full_name":  fullName,
			"phone":      phone,
			"avatar_url": avatarURL,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	if singleMode(c) != "" {
		if len(items) == 0 {
			return response.OK(c, nil)
		}
		return response.OK(c, items[0])
	}
	return response.OK(c, items)
}

func (h *CompatHandler) selectUserRoles(c *fiber.Ctx, filters []compatFilter) error {
	q := `SELECT id, user_id, role, created_at FROM user_roles`
	args := []interface{}{}
	if v, ok := filterValue(filters, "user_id"); ok {
		q += " WHERE user_id = $1"
		args = append(args, v)
	}
	q += " ORDER BY created_at"

	rows, err := h.pool.Query(c.Context(), q, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, userID, role string
		var createdAt interface{}
		if err := rows.Scan(&id, &userID, &role, &createdAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, err.Error())
		}
		items = append(items, map[string]interface{}{
			"id":         id,
			"user_id":    userID,
			"role":       role,
			"created_at": createdAt,
		})
	}
	return response.OK(c, items)
}

func (h *CompatHandler) selectRooms(c *fiber.Ctx, filters []compatFilter) error {
	q := `SELECT id, room_number, room_type, floor, capacity, price_per_night, status, amenities, created_at, updated_at FROM rooms`
	args := []interface{}{}
	if v, ok := filterValue(filters, "status"); ok {
		q += " WHERE status = $1"
		args = append(args, v)
	}
	q += " ORDER BY floor, room_number"

	rows, err := h.pool.Query(c.Context(), q, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, roomNumber, roomType, status string
		var floor, capacity int
		var price float64
		var amenities []string
		var createdAt, updatedAt interface{}
		if err := rows.Scan(&id, &roomNumber, &roomType, &floor, &capacity, &price, &status, &amenities, &createdAt, &updatedAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, err.Error())
		}
		items = append(items, map[string]interface{}{
			"id":              id,
			"room_number":     roomNumber,
			"room_type":       roomType,
			"floor":           floor,
			"capacity":        capacity,
			"price_per_night": price,
			"status":          status,
			"amenities":       amenities,
			"created_at":      createdAt,
			"updated_at":      updatedAt,
		})
	}
	return response.OK(c, items)
}

func (h *CompatHandler) selectGuestStays(c *fiber.Ctx, filters []compatFilter) error {
	q := `SELECT gs.id, gs.guest_id, gs.room_id, gs.guest_name, gs.guest_email, gs.guest_phone,
		         gs.check_in_date, gs.check_out_date, gs.actual_check_in, gs.actual_check_out,
		         gs.total_amount, gs.notes, gs.created_by, gs.created_at, gs.updated_at,
		         r.room_number, r.room_type
		  FROM guest_stays gs
		  LEFT JOIN rooms r ON r.id = gs.room_id`
	args := []interface{}{}
	where := []string{}
	if v, ok := filterValue(filters, "guest_id"); ok {
		args = append(args, v)
		where = append(where, fmt.Sprintf("gs.guest_id = $%d", len(args)))
	}
	if v, ok := filterValue(filters, "room_id"); ok {
		args = append(args, v)
		where = append(where, fmt.Sprintf("gs.room_id = $%d", len(args)))
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY gs.check_in_date DESC"

	rows, err := h.pool.Query(c.Context(), q, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, roomID, guestName string
		var guestID, guestEmail, guestPhone, notes, createdBy, roomNumber, roomType *string
		var checkIn, checkOut, actualCheckIn, actualCheckOut, createdAt, updatedAt interface{}
		var totalAmount *float64
		if err := rows.Scan(
			&id, &guestID, &roomID, &guestName, &guestEmail, &guestPhone,
			&checkIn, &checkOut, &actualCheckIn, &actualCheckOut,
			&totalAmount, &notes, &createdBy, &createdAt, &updatedAt,
			&roomNumber, &roomType,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, err.Error())
		}
		item := map[string]interface{}{
			"id":               id,
			"guest_id":         guestID,
			"room_id":          roomID,
			"guest_name":       guestName,
			"guest_email":      guestEmail,
			"guest_phone":      guestPhone,
			"check_in_date":    checkIn,
			"check_out_date":   checkOut,
			"actual_check_in":  actualCheckIn,
			"actual_check_out": actualCheckOut,
			"total_amount":     totalAmount,
			"notes":            notes,
			"created_by":       createdBy,
			"created_at":       createdAt,
			"updated_at":       updatedAt,
			"rooms":            nil,
		}
		if roomNumber != nil {
			item["rooms"] = map[string]interface{}{
				"room_number": *roomNumber,
				"room_type":   roomType,
			}
		}
		items = append(items, item)
	}
	return response.OK(c, items)
}

func (h *CompatHandler) insertRoom(c *fiber.Ctx, v map[string]interface{}) error {
	id := uuid.New().String()
	roomNumber := asString(v["room_number"])
	roomType := asStringDefault(v["room_type"], "Standard")
	floor := asIntDefault(v["floor"], 1)
	capacity := asIntDefault(v["capacity"], 2)
	price := asFloatDefault(v["price_per_night"], 0)
	status := asStringDefault(v["status"], "available")
	amenities := asStringSlice(v["amenities"])

	const q = `INSERT INTO rooms (id, room_number, room_type, floor, capacity, price_per_night, status, amenities, created_at, updated_at)
	           VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now(),now())
	           RETURNING id, room_number, room_type, floor, capacity, price_per_night, status, amenities, created_at, updated_at`
	rows, err := h.pool.Query(c.Context(), q, id, roomNumber, roomType, floor, capacity, price, status, amenities)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	defer rows.Close()
	items, err := scanRoomMaps(rows)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Created(c, items)
}

func (h *CompatHandler) updateRoom(c *fiber.Ctx, id string, v map[string]interface{}) error {
	allowed := map[string]bool{
		"room_number": true, "room_type": true, "floor": true, "capacity": true,
		"price_per_night": true, "status": true, "amenities": true,
	}
	return h.updateAllowed(c, "rooms", id, allowed, v)
}

func (h *CompatHandler) insertGuestStay(c *fiber.Ctx, v map[string]interface{}) error {
	id := uuid.New().String()
	guestName := asStringDefault(v["guest_name"], "Guest")
	const q = `INSERT INTO guest_stays (
	             id, guest_id, room_id, guest_name, guest_email, guest_phone,
	             check_in_date, check_out_date, actual_check_in, actual_check_out,
	             total_amount, notes, created_by, created_at, updated_at
	           ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),now())
	           RETURNING id`
	if _, err := h.pool.Exec(c.Context(), q,
		id, nullableString(v["guest_id"]), asString(v["room_id"]), guestName,
		nullableString(v["guest_email"]), nullableString(v["guest_phone"]),
		v["check_in_date"], v["check_out_date"], v["actual_check_in"], v["actual_check_out"],
		nullableFloat(v["total_amount"]), nullableString(v["notes"]), nullableString(v["created_by"]),
	); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Created(c, []map[string]interface{}{{"id": id}})
}

func (h *CompatHandler) updateGuestStay(c *fiber.Ctx, id string, v map[string]interface{}) error {
	allowed := map[string]bool{
		"guest_id": true, "room_id": true, "guest_name": true, "guest_email": true,
		"guest_phone": true, "check_in_date": true, "check_out_date": true,
		"actual_check_in": true, "actual_check_out": true, "total_amount": true,
		"notes": true, "created_by": true,
	}
	return h.updateAllowed(c, "guest_stays", id, allowed, v)
}

func (h *CompatHandler) updateAllowed(c *fiber.Ctx, table, id string, allowed map[string]bool, v map[string]interface{}) error {
	set := []string{}
	args := []interface{}{}
	for key, value := range v {
		if !allowed[key] {
			continue
		}
		args = append(args, value)
		set = append(set, fmt.Sprintf("%s = $%d", key, len(args)))
	}
	if len(set) == 0 {
		return response.OK(c, []map[string]interface{}{})
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE %s SET %s, updated_at = now() WHERE id = $%d", table, strings.Join(set, ", "), len(args))
	if _, err := h.pool.Exec(c.Context(), q, args...); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.OK(c, []map[string]interface{}{})
}

func scanRoomMaps(rows interface {
	Next() bool
	Scan(...interface{}) error
	Err() error
}) ([]map[string]interface{}, error) {
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, roomNumber, roomType, status string
		var floor, capacity int
		var price float64
		var amenities []string
		var createdAt, updatedAt interface{}
		if err := rows.Scan(&id, &roomNumber, &roomType, &floor, &capacity, &price, &status, &amenities, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]interface{}{
			"id":              id,
			"room_number":     roomNumber,
			"room_type":       roomType,
			"floor":           floor,
			"capacity":        capacity,
			"price_per_night": price,
			"status":          status,
			"amenities":       amenities,
			"created_at":      createdAt,
			"updated_at":      updatedAt,
		})
	}
	return items, rows.Err()
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func asStringDefault(v interface{}, fallback string) string {
	if s := asString(v); s != "" {
		return s
	}
	return fallback
}

func nullableString(v interface{}) interface{} {
	if s := asString(v); s != "" {
		return s
	}
	return nil
}

func asIntDefault(v interface{}, fallback int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if parsed, err := strconv.Atoi(n); err == nil {
			return parsed
		}
	}
	return fallback
}

func asFloatDefault(v interface{}, fallback float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		if parsed, err := strconv.ParseFloat(n, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func nullableFloat(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	return asFloatDefault(v, 0)
}

func asStringSlice(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		if existing, ok := v.([]string); ok {
			return existing
		}
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
