package model

import (
	"time"

	"github.com/google/uuid"
)

// BookingStatus represents the finite set of states a booking can be in.
// See internal/statemachine for the transition rules — this type only
// defines the values, not the rules.
type BookingStatus string

const (
	StatusPending   BookingStatus = "PENDING"
	StatusLocked    BookingStatus = "LOCKED"
	StatusConfirmed BookingStatus = "CONFIRMED" // terminal
	StatusExpired   BookingStatus = "EXPIRED"   // terminal
)

// Booking is the system-of-record representation of a single seat reservation.
// One row per seat, per the schema design in the Solution Design Document,
// Section 5.2.3.
type Booking struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	EventID     uuid.UUID
	SeatNumber  string
	Status      BookingStatus
	LockedUntil *time.Time
	CreatedAt   time.Time
	ConfirmedAt *time.Time
	CancelledAt *time.Time
}

// CreateBookingRequest is the inbound payload for POST /bookings.
type CreateBookingRequest struct {
	EventID uuid.UUID `json:"event_id"`
	Seats   []string  `json:"seats"`
}

// CreateBookingResponse is returned on successful seat lock acquisition.
type CreateBookingResponse struct {
	BookingID      uuid.UUID     `json:"booking_id"`
	Status         BookingStatus `json:"status"`
	LockTTLSeconds int           `json:"lock_ttl_seconds"`
}
