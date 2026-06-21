package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/eventclient"
	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/lock"
	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/model"
	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/repository"
	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/statemachine"
)

// BookingService contains the business logic for the booking workflow. It is
// the only layer that knows about the repository, the Redis locker, AND the
// Event Service client — the orchestration order (create -> lock -> confirm)
// lives in exactly one place.
type BookingService struct {
	repo        *repository.BookingRepository
	locker      *lock.SeatLocker
	eventClient *eventclient.Client
	ttl         time.Duration
	logger      *zap.Logger
}

func NewBookingService(repo *repository.BookingRepository, locker *lock.SeatLocker, eventClient *eventclient.Client, ttlSeconds int, logger *zap.Logger) *BookingService {
	return &BookingService{
		repo:        repo,
		locker:      locker,
		eventClient: eventClient,
		ttl:         time.Duration(ttlSeconds) * time.Second,
		logger:      logger,
	}
}

// ErrSeatUnavailable is returned when a requested seat is already locked.
// Handlers map this to HTTP 409 Conflict.
type ErrSeatUnavailable struct {
	Seat string
}

func (e *ErrSeatUnavailable) Error() string {
	return fmt.Sprintf("seat %s is no longer available", e.Seat)
}

// ErrInventoryExhausted is returned when Event Service's compare-and-swap
// refused the decrement — there is genuinely no seat capacity left, even
// though the Redis lock succeeded. This should be rare (the Redis lock is
// supposed to prevent exactly this), and its existence is the literal
// embodiment of "defense in depth" from design doc Section 3.3.1: two
// independent layers, and the code respects both.
type ErrInventoryExhausted struct {
	EventID uuid.UUID
}

func (e *ErrInventoryExhausted) Error() string {
	return fmt.Sprintf("no available seat capacity remains for event %s", e.EventID)
}

// CreateBooking implements steps 3-7 of the sequence diagram (design doc
// Section 4.1): create a PENDING record, attempt to acquire the Redis lock,
// and on success advance to LOCKED.
func (s *BookingService) CreateBooking(ctx context.Context, userID, eventID uuid.UUID, seats []string) (*model.Booking, error) {
	booking := &model.Booking{
		ID:      uuid.New(),
		UserID:  userID,
		EventID: eventID,
		// NOTE: schema models one row per seat. For a multi-seat request,
		// a real implementation loops this per seat or batch-inserts.
		// Simplified here to the first seat for clarity.
		SeatNumber: seats[0],
		Status:     model.StatusPending,
		CreatedAt:  time.Now().UTC(),
	}

	if err := s.repo.Create(ctx, booking); err != nil {
		return nil, fmt.Errorf("service: failed to create booking record: %w", err)
	}

	failedSeat, err := s.locker.AcquireAll(ctx, booking.ID.String(), eventID.String(), seats)
	if err != nil {
		s.logger.Error("redis lock acquisition error", zap.Error(err), zap.String("booking_id", booking.ID.String()))
		return nil, fmt.Errorf("service: lock acquisition failed: %w", err)
	}
	if failedSeat != "" {
		s.logger.Info("seat unavailable, booking left PENDING for cleanup",
			zap.String("booking_id", booking.ID.String()), zap.String("seat", failedSeat))
		return nil, &ErrSeatUnavailable{Seat: failedSeat}
	}

	lockedUntil := time.Now().UTC().Add(s.ttl)
	newStatus, err := statemachine.Apply(model.StatusPending, model.StatusLocked)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	applied, err := s.repo.UpdateStatus(ctx, booking.ID, model.StatusPending, newStatus, &lockedUntil)
	if err != nil {
		return nil, fmt.Errorf("service: failed to persist LOCKED status: %w", err)
	}
	if !applied {
		return nil, fmt.Errorf("service: booking %s was modified concurrently", booking.ID)
	}

	booking.Status = newStatus
	booking.LockedUntil = &lockedUntil
	return booking, nil
}

// ConfirmBooking is invoked when a PaymentConfirmed event arrives (design
// doc Section 4.1, steps 14-18). It performs the LOCKED -> CONFIRMED
// transition, calls Event Service for the seat-count compare-and-swap, and
// releases the Redis lock once PostgreSQL is the durable source of truth.
func (s *BookingService) ConfirmBooking(ctx context.Context, bookingID uuid.UUID) error {
	booking, err := s.repo.GetByID(ctx, bookingID)
	if err != nil {
		return fmt.Errorf("service: failed to load booking %s: %w", bookingID, err)
	}

	newStatus, err := statemachine.Apply(booking.Status, model.StatusConfirmed)
	if err != nil {
		return fmt.Errorf("service: cannot confirm booking %s: %w", bookingID, err)
	}

	// Cross-service call to Event Service — this is "Layer 2" of the defense
	// against overbooking (design doc Section 4.3), now implemented as a real
	// network call rather than a local-table shortcut, per the
	// database-per-service boundary (Section 2.3).
	seatsApplied, err := s.eventClient.DecrementSeats(ctx, booking.EventID, 1)
	if err != nil {
		// Event Service being unreachable here is a real operational problem,
		// not a business outcome — we do not confirm the booking, and we
		// surface this distinctly from "inventory exhausted" so callers/alerts
		// can tell the difference between "no seats left" and "dependency down".
		return fmt.Errorf("service: failed to reach event service for seat decrement: %w", err)
	}
	if !seatsApplied {
		// The Redis lock said this seat was available, but Event Service's
		// compare-and-swap disagreed. We trust Event Service — defense in
		// depth means refusing to confirm rather than assuming the lock layer
		// alone was sufficient.
		return &ErrInventoryExhausted{EventID: booking.EventID}
	}

	applied, err := s.repo.UpdateStatus(ctx, bookingID, booking.Status, newStatus, nil)
	if err != nil {
		return fmt.Errorf("service: failed to persist CONFIRMED status: %w", err)
	}
	if !applied {
		return fmt.Errorf("service: booking %s was modified concurrently during confirmation", bookingID)
	}

	if err := s.locker.Release(ctx, booking.EventID.String(), []string{booking.SeatNumber}); err != nil {
		// Logged, not returned: the booking is already durably CONFIRMED;
		// the Redis key self-expires via TTL even if this delete fails
		// (design doc Section 4.3).
		s.logger.Warn("failed to release redis lock after confirmation", zap.Error(err), zap.String("booking_id", bookingID.String()))
	}

	return nil
}

// ExpireStaleLocks is invoked periodically by the TTL watcher to reconcile
// bookings whose Redis lock expired without a corresponding confirmation.
func (s *BookingService) ExpireStaleLocks(ctx context.Context) (expiredCount int, err error) {
	stale, err := s.repo.FindExpiredLocks(ctx, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("service: failed to query expired locks: %w", err)
	}

	for _, b := range stale {
		newStatus, err := statemachine.Apply(b.Status, model.StatusExpired)
		if err != nil {
			s.logger.Warn("skipping stale lock with illegal transition", zap.String("booking_id", b.ID.String()), zap.Error(err))
			continue
		}
		applied, err := s.repo.UpdateStatus(ctx, b.ID, b.Status, newStatus, nil)
		if err != nil {
			s.logger.Error("failed to expire stale booking", zap.String("booking_id", b.ID.String()), zap.Error(err))
			continue
		}
		if applied {
			expiredCount++
		}
	}
	return expiredCount, nil
}

// GetBooking fetches a single booking for the GET /bookings/{id} endpoint.
func (s *BookingService) GetBooking(ctx context.Context, bookingID uuid.UUID) (*model.Booking, error) {
	b, err := s.repo.GetByID(ctx, bookingID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to fetch booking %s: %w", bookingID, err)
	}
	return b, nil
}
