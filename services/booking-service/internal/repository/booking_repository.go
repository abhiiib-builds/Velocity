package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/db"
	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/model"
)

// BookingRepository wraps the sqlc-generated Querier with business-meaningful
// method names, model-struct conversion, and consistent error wrapping.
// Nothing here writes raw SQL — that lives in db/queries/booking.sql and is
// compiled by sqlc into internal/db. This wrapper exists so callers have a
// stable interface that doesn't change shape just because a query's
// generated signature changes.
type BookingRepository struct {
	q db.Querier
}

func NewBookingRepository(pool *pgxpool.Pool) *BookingRepository {
	return &BookingRepository{q: db.New(pool)}
}

// rowToModel converts a generated row struct into our domain model.Booking.
// Centralizing this conversion in one function (rather than repeating field
// mapping in every method) means a schema change only requires updating
// this one function.
func rowToModel(row db.Booking) model.Booking {
	var lockedUntil, confirmedAt, cancelledAt *time.Time
	if row.LockedUntil != nil {
		lockedUntil = row.LockedUntil
	}
	if row.ConfirmedAt != nil {
		confirmedAt = row.ConfirmedAt
	}
	if row.CancelledAt != nil {
		cancelledAt = row.CancelledAt
	}
	return model.Booking{
		ID:          row.BookingID,
		UserID:      row.UserID,
		EventID:     row.EventID,
		SeatNumber:  row.SeatNumber,
		Status:      model.BookingStatus(row.BookingStatus),
		LockedUntil: lockedUntil,
		CreatedAt:   row.CreatedAt,
		ConfirmedAt: confirmedAt,
		CancelledAt: cancelledAt,
	}
}

// Create inserts a new booking row in PENDING status (sequence diagram
// step 3, design doc Section 4.1).
func (r *BookingRepository) Create(ctx context.Context, b *model.Booking) error {
	err := r.q.CreateBooking(ctx, db.CreateBookingParams{
		BookingID:     b.ID,
		UserID:        b.UserID,
		EventID:       b.EventID,
		SeatNumber:    b.SeatNumber,
		BookingStatus: string(b.Status),
		CreatedAt:     b.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("repository: failed to create booking: %w", err)
	}
	return nil
}

// UpdateStatus performs a guarded status transition: only applies if the row
// is still in expectedCurrent status (design doc Section 3.3.1, "Layer 2").
// Returns applied=false (not an error) for a benign lost race — the caller
// must check this boolean rather than assuming success.
func (r *BookingRepository) UpdateStatus(ctx context.Context, bookingID uuid.UUID, expectedCurrent, target model.BookingStatus, lockedUntil *time.Time) (applied bool, err error) {
	rows, err := r.q.UpdateBookingStatus(ctx, db.UpdateBookingStatusParams{
		BookingID:      bookingID,
		NewStatus:      string(target),
		LockedUntil:    lockedUntil,
		ExpectedStatus: string(expectedCurrent),
	})
	if err != nil {
		return false, fmt.Errorf("repository: failed to update booking status: %w", err)
	}
	return rows == 1, nil
}

// GetByID fetches a single booking.
func (r *BookingRepository) GetByID(ctx context.Context, bookingID uuid.UUID) (*model.Booking, error) {
	row, err := r.q.GetBookingByID(ctx, bookingID)
	if err != nil {
		return nil, fmt.Errorf("repository: failed to fetch booking %s: %w", bookingID, err)
	}
	b := rowToModel(row)
	return &b, nil
}

// FindExpiredLocks returns bookings whose lock passed TTL without reaching
// CONFIRMED — the input to the background TTL watcher.
func (r *BookingRepository) FindExpiredLocks(ctx context.Context, asOf time.Time) ([]model.Booking, error) {
	rows, err := r.q.FindExpiredLocks(ctx, asOf)
	if err != nil {
		return nil, fmt.Errorf("repository: failed to query expired locks: %w", err)
	}
	results := make([]model.Booking, 0, len(rows))
	for _, row := range rows {
		results = append(results, rowToModel(row))
	}
	return results, nil
}

// ListByUser returns a user's complete booking history.
func (r *BookingRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.Booking, error) {
	rows, err := r.q.ListBookingsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: failed to list bookings for user %s: %w", userID, err)
	}
	results := make([]model.Booking, 0, len(rows))
	for _, row := range rows {
		results = append(results, rowToModel(row))
	}
	return results, nil
}
