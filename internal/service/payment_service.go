package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/hotelharmony/api/internal/cache"
	"github.com/hotelharmony/api/internal/config"
	"github.com/hotelharmony/api/internal/domain"
	"github.com/hotelharmony/api/internal/repository/postgres"
)

// zeroDecimalCurrencies are Stripe currencies that must NOT be multiplied by 100.
var zeroDecimalCurrencies = map[string]bool{
	"BIF": true, "CLP": true, "DJF": true, "GNF": true, "JPY": true,
	"KMF": true, "KRW": true, "MGA": true, "PYG": true, "RWF": true,
	"UGX": true, "VND": true, "VUV": true, "XAF": true, "XOF": true, "XPF": true,
}

// BookingCheckoutRequest contains all inputs for a new booking checkout.
type BookingCheckoutRequest struct {
	RoomID       uuid.UUID `json:"room_id" validate:"required"`
	UserID       uuid.UUID `json:"user_id" validate:"required"`
	Currency     string    `json:"currency"`
	CheckInDate  time.Time `json:"check_in_date" validate:"required"`
	CheckOutDate time.Time `json:"check_out_date" validate:"required"`
	GuestName    string    `json:"guest_name"`
	GuestEmail   string    `json:"guest_email"`
	GuestPhone   string    `json:"guest_phone"`
	Country      string    `json:"country"`
	OriginURL    string    `json:"-"`
}

// CheckoutResult is returned after a successful Stripe checkout session creation.
type CheckoutResult struct {
	CheckoutURL string    `json:"checkout_url"`
	SessionID   string    `json:"session_id"`
	StayID      uuid.UUID `json:"stay_id"`
	PaymentID   uuid.UUID `json:"payment_id,omitempty"`
}

// PaymentService orchestrates Stripe checkout and payment completion.
type PaymentService interface {
	BookingCheckout(ctx context.Context, req BookingCheckoutRequest) (*CheckoutResult, error)
	PaymentCheckout(ctx context.Context, paymentID uuid.UUID, currency, country, originURL string) (*CheckoutResult, error)
	CompletePayment(ctx context.Context, paymentID uuid.UUID, sessionID string) error
	GetConfig(ctx context.Context) map[string]interface{}
	GetExchangeRate(ctx context.Context, base, target string) (float64, error)
}

type paymentService struct {
	roomRepo    postgres.RoomRepository
	paymentRepo postgres.PaymentRepository
	cache       cache.Cache
	cfg         *config.Config
	log         *zap.Logger
	httpClient  *http.Client
	fxMu        sync.Mutex
}

func NewPaymentService(
	roomRepo postgres.RoomRepository,
	paymentRepo postgres.PaymentRepository,
	c cache.Cache,
	cfg *config.Config,
	log *zap.Logger,
) PaymentService {
	return &paymentService{
		roomRepo:    roomRepo,
		paymentRepo: paymentRepo,
		cache:       c,
		cfg:         cfg,
		log:         log,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

// BookingCheckout creates a guest_stay, payment record, and Stripe session.
func (s *paymentService) BookingCheckout(ctx context.Context, req BookingCheckoutRequest) (*CheckoutResult, error) {
	if s.cfg.Stripe.SecretKey == "" {
		return nil, fmt.Errorf("Stripe secret key is not configured")
	}
	lockKey := fmt.Sprintf("lock:booking:%s:%s:%s:%s", req.UserID, req.RoomID, req.CheckInDate.Format("2006-01-02"), req.CheckOutDate.Format("2006-01-02"))
	locked, err := s.cache.SetNX(ctx, lockKey, "1", cache.TTLLock)
	if err != nil {
		s.log.Warn("booking lock unavailable", zap.Error(err))
	} else if !locked {
		return nil, fmt.Errorf("booking is already being processed")
	} else {
		defer func() { _ = s.cache.Delete(context.Background(), lockKey) }()
	}

	currency := strings.ToUpper(req.Currency)
	if currency == "" {
		currency = "USD"
	}

	nights := int(req.CheckOutDate.Sub(req.CheckInDate).Hours() / 24)
	if nights < 1 {
		nights = 1
	}

	room, err := s.roomRepo.FindRoomByID(ctx, req.RoomID)
	if err != nil || room.Status != domain.RoomStatusAvailable {
		return nil, fmt.Errorf("room is no longer available")
	}

	usdAmount := room.PricePerNight * float64(nights)

	rate, err := s.GetExchangeRate(ctx, "USD", currency)
	if err != nil {
		return nil, fmt.Errorf("unable to price booking in %s: %w", currency, err)
	}
	convertedAmount := roundTo2(usdAmount * rate)

	guestName := req.GuestName
	if guestName == "" {
		guestName = "Guest"
	}
	guestEmail := &req.GuestEmail
	guestPhone := &req.GuestPhone
	notes := fmt.Sprintf("Stripe checkout pending. Country: %s. Currency: %s. Rate: %.6f", req.Country, currency, rate)

	stay, err := s.roomRepo.CreateStay(ctx, &domain.GuestStay{
		GuestID:      &req.UserID,
		RoomID:       req.RoomID,
		GuestName:    guestName,
		GuestEmail:   guestEmail,
		GuestPhone:   guestPhone,
		CheckInDate:  req.CheckInDate,
		CheckOutDate: req.CheckOutDate,
		TotalAmount:  &usdAmount,
		Notes:        &notes,
		CreatedBy:    &req.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("create stay: %w", err)
	}

	payNum := fmt.Sprintf("PAY-%s-%s", time.Now().Format("150405"), strings.ToUpper(stay.ID.String()[:6]))
	payMethod := "stripe"
	payNotes := fmt.Sprintf("Room %s booking for %d night(s) in %s", room.RoomNumber, nights, currency)
	payment, err := s.paymentRepo.Create(ctx, &domain.Payment{
		PaymentNumber: payNum,
		GuestStayID:   &stay.ID,
		Amount:        convertedAmount,
		PaymentMethod: payMethod,
		Status:        domain.PaymentStatusPending,
		ProcessedBy:   &req.UserID,
		Notes:         &payNotes,
	})
	if err != nil {
		return nil, fmt.Errorf("create payment: %w", err)
	}

	_ = s.roomRepo.UpdateRoomStatus(ctx, req.RoomID, domain.RoomStatusOccupied)
	_ = s.cache.Delete(ctx, cache.KeyDashboardStats(), cache.KeyRoomList("all"), cache.KeyRoomList(string(domain.RoomStatusAvailable)), cache.KeyRoomList(string(domain.RoomStatusOccupied)))

	description := fmt.Sprintf("Room %s - %s - %d night(s)", room.RoomNumber, room.RoomType, nights)
	successURL := fmt.Sprintf("%s/guest?booking=success&stay_id=%s&payment_id=%s&session_id={CHECKOUT_SESSION_ID}",
		req.OriginURL, stay.ID, payment.ID)
	cancelURL := fmt.Sprintf("%s/guest?booking=cancelled&stay_id=%s", req.OriginURL, stay.ID)

	session, err := s.createStripeSession(ctx, stripeSessionParams{
		Currency:       currency,
		Amount:         convertedAmount,
		GuestEmail:     req.GuestEmail,
		StayID:         stay.ID,
		PaymentID:      payment.ID,
		RoomID:         req.RoomID,
		Country:        req.Country,
		ProductName:    fmt.Sprintf("Hotel Room %s Booking", room.RoomNumber),
		Description:    description,
		SuccessURL:     successURL,
		CancelURL:      cancelURL,
		IdempotencyKey: stay.ID.String(),
	})
	if err != nil {
		_ = s.paymentRepo.Delete(ctx, payment.ID)
		_ = s.roomRepo.DeleteStay(ctx, stay.ID)
		_ = s.roomRepo.UpdateRoomStatus(ctx, req.RoomID, domain.RoomStatusAvailable)
		return nil, fmt.Errorf("Stripe checkout failed: %w", err)
	}

	return &CheckoutResult{
		CheckoutURL: session.URL,
		SessionID:   session.ID,
		StayID:      stay.ID,
		PaymentID:   payment.ID,
	}, nil
}

// PaymentCheckout creates a Stripe session for an existing payment record.
func (s *paymentService) PaymentCheckout(ctx context.Context, paymentID uuid.UUID, currency, country, originURL string) (*CheckoutResult, error) {
	if s.cfg.Stripe.SecretKey == "" {
		return nil, fmt.Errorf("Stripe secret key is not configured")
	}
	lockKey := "lock:payment:" + paymentID.String()
	locked, err := s.cache.SetNX(ctx, lockKey, "1", cache.TTLLock)
	if err != nil {
		s.log.Warn("payment lock unavailable", zap.Error(err))
	} else if !locked {
		return nil, fmt.Errorf("payment checkout is already being processed")
	} else {
		defer func() { _ = s.cache.Delete(context.Background(), lockKey) }()
	}

	currency = strings.ToUpper(currency)
	if currency == "" {
		currency = "USD"
	}

	payment, err := s.paymentRepo.FindByID(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("payment not found")
	}
	if payment.Status == domain.PaymentStatusCompleted {
		return nil, fmt.Errorf("payment is already completed")
	}
	if payment.GuestStayID == nil {
		return nil, fmt.Errorf("booking not found for this payment")
	}

	stay, err := s.roomRepo.FindStayByID(ctx, *payment.GuestStayID)
	if err != nil {
		return nil, fmt.Errorf("booking not found")
	}

	usdAmount := 0.0
	if stay.TotalAmount != nil {
		usdAmount = *stay.TotalAmount
	} else {
		usdAmount = payment.Amount
	}

	rate, err := s.GetExchangeRate(ctx, "USD", currency)
	if err != nil {
		return nil, fmt.Errorf("unable to price payment in %s: %w", currency, err)
	}
	convertedAmount := roundTo2(usdAmount * rate)

	notes := fmt.Sprintf("Stripe checkout pending. Country: %s. Currency: %s. Rate: %.6f", country, currency, rate)
	_ = s.paymentRepo.UpdateAmountAndNotes(ctx, paymentID, convertedAmount, "stripe", notes)

	roomNumber := ""
	if stay.Room != nil {
		roomNumber = stay.Room.RoomNumber
	}
	successURL := fmt.Sprintf("%s/guest?booking=success&stay_id=%s&payment_id=%s&session_id={CHECKOUT_SESSION_ID}",
		originURL, stay.ID, paymentID)
	cancelURL := fmt.Sprintf("%s/guest?booking=cancelled&stay_id=%s&payment_id=%s", originURL, stay.ID, paymentID)

	guestEmail := ""
	if stay.GuestEmail != nil {
		guestEmail = *stay.GuestEmail
	}

	session, err := s.createStripeSession(ctx, stripeSessionParams{
		Currency:       currency,
		Amount:         convertedAmount,
		GuestEmail:     guestEmail,
		StayID:         stay.ID,
		PaymentID:      paymentID,
		Country:        country,
		ProductName:    fmt.Sprintf("Hotel Room %s Booking Payment", roomNumber),
		Description:    fmt.Sprintf("Room %s payment", roomNumber),
		SuccessURL:     successURL,
		CancelURL:      cancelURL,
		IdempotencyKey: "pay-" + paymentID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("Stripe checkout failed: %w", err)
	}

	return &CheckoutResult{
		CheckoutURL: session.URL,
		SessionID:   session.ID,
		StayID:      stay.ID,
		PaymentID:   paymentID,
	}, nil
}

// CompletePayment verifies a Stripe session and marks the payment completed.
func (s *paymentService) CompletePayment(ctx context.Context, paymentID uuid.UUID, sessionID string) error {
	if s.cfg.Stripe.SecretKey == "" {
		return fmt.Errorf("Stripe secret key is not configured")
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.stripe.com/v1/checkout/sessions/%s", sessionID), nil)
	req.Header.Set("Authorization", "Bearer "+s.cfg.Stripe.SecretKey)
	req.Header.Set("User-Agent", "HotelHarmony/2.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("unable to verify Stripe payment: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var session struct {
		PaymentStatus string `json:"payment_status"`
	}
	if json.Unmarshal(body, &session) != nil || session.PaymentStatus != "paid" {
		return fmt.Errorf("Stripe payment is not paid yet")
	}

	if err := s.paymentRepo.UpdateStatus(ctx, paymentID, domain.PaymentStatusCompleted, "stripe"); err != nil {
		return err
	}
	_ = s.cache.Delete(ctx, cache.KeyDashboardStats())
	return nil
}

// GetConfig returns payment gateway configuration for the frontend.
func (s *paymentService) GetConfig(ctx context.Context) map[string]interface{} {
	secret := s.cfg.Stripe.SecretKey
	pub := s.cfg.Stripe.PublishableKey
	configured := strings.HasPrefix(secret, "sk_")
	mode := ""
	if strings.HasPrefix(secret, "sk_live_") {
		mode = "live"
	} else if strings.HasPrefix(secret, "sk_test_") {
		mode = "test"
	}
	pubMode := ""
	if strings.HasPrefix(pub, "pk_live_") {
		pubMode = "live"
	} else if strings.HasPrefix(pub, "pk_test_") {
		pubMode = "test"
	}
	return map[string]interface{}{
		"stripe_configured": configured,
		"mode":              mode,
		"publishable_mode":  pubMode,
		"mode_matches":      mode != "" && pubMode != "" && mode == pubMode,
	}
}

// GetExchangeRate fetches a live exchange rate from Frankfurter, cached in Redis.
func (s *paymentService) GetExchangeRate(ctx context.Context, base, target string) (float64, error) {
	base = strings.ToUpper(base)
	target = strings.ToUpper(target)
	if base == target {
		return 1.0, nil
	}

	cacheKey := fmt.Sprintf("fx:%s:%s", base, target)
	if v, err := s.cache.Get(ctx, cacheKey); err == nil {
		var rate float64
		if n, err := fmt.Sscanf(v, "%f", &rate); err == nil && n == 1 {
			return rate, nil
		}
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.frankfurter.dev/v2/rate/%s/%s", base, target), nil)
	req.Header.Set("User-Agent", "HotelHarmony/2.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("exchange rate fetch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Rate float64 `json:"rate"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Rate == 0 {
		return 0, fmt.Errorf("exchange rate: invalid response")
	}

	_ = s.cache.Set(ctx, cacheKey, fmt.Sprintf("%.6f", result.Rate), 10*time.Minute)
	return result.Rate, nil
}

type stripeSessionParams struct {
	Currency       string
	Amount         float64
	GuestEmail     string
	StayID         uuid.UUID
	PaymentID      uuid.UUID
	RoomID         uuid.UUID
	Country        string
	ProductName    string
	Description    string
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

type stripeSession struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func (s *paymentService) createStripeSession(ctx context.Context, p stripeSessionParams) (*stripeSession, error) {
	minorAmount := stripeMinorAmount(p.Amount, p.Currency)

	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("payment_method_types[0]", "card")
	form.Set("success_url", p.SuccessURL)
	form.Set("cancel_url", p.CancelURL)
	form.Set("customer_email", p.GuestEmail)
	form.Set("client_reference_id", p.StayID.String())
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", strings.ToLower(p.Currency))
	form.Set("line_items[0][price_data][unit_amount]", fmt.Sprintf("%d", minorAmount))
	form.Set("line_items[0][price_data][product_data][name]", p.ProductName)
	form.Set("line_items[0][price_data][product_data][description]", p.Description)
	form.Set("metadata[stay_id]", p.StayID.String())
	form.Set("metadata[payment_id]", p.PaymentID.String())
	form.Set("metadata[room_id]", p.RoomID.String())
	form.Set("metadata[currency]", p.Currency)
	form.Set("metadata[country]", p.Country)
	form.Set("payment_intent_data[metadata][stay_id]", p.StayID.String())
	form.Set("payment_intent_data[metadata][payment_id]", p.PaymentID.String())

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost,
		"https://api.stripe.com/v1/checkout/sessions",
		strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+s.cfg.Stripe.SecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Idempotency-Key", p.IdempotencyKey)
	req.Header.Set("User-Agent", "HotelHarmony/2.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stripe error %d: %s", resp.StatusCode, string(body))
	}

	var session stripeSession
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("stripe: invalid session response")
	}
	return &session, nil
}

func stripeMinorAmount(amount float64, currency string) int64 {
	if zeroDecimalCurrencies[strings.ToUpper(currency)] {
		v := int64(amount)
		if v < 1 {
			return 1
		}
		return v
	}
	v := int64(amount * 100)
	if v < 1 {
		return 1
	}
	return v
}

func roundTo2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
