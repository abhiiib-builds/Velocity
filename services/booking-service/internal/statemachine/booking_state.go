package statemachine

import (
	"fmt"

	"github.com/abhiiib-builds/Velocity/services/booking-service/internal/model"
)

// transitions defines every legal (from -> to) move in the booking lifecycle

// PENDING -> LOCKED (all seats locks acquired in Redis)
// PENDING -> EXPIRED (lock acquisition failed / explicit cancel before lock)
// LOCKED -> CONFIRMED (PaymentConfirmed event consumed from Kafka)
// LOCKED -> EXPIRED (TTL Watcher: lock expired with no payment)

var transitions = map[model.BookingStatus][]model.BookingStatus{
	model.StatusPending: {model.StatusLocked, model.StatusExpired},
	model.StatusLocked:  {model.StatusConfirmed, model.StatusExpired},
}

// CanTransition reports whether moving from "from" to "to" is legal.
func CanTransition(from, to model.BookingStatus) bool {
	allowed, ok := transitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// Apply validates and returns the new status, or an error if illegal.
// Callers should always go through Apply rather than setting status
// directly, so an invalid transition is caught at the point of intent.
func Apply(current, target model.BookingStatus) (model.BookingStatus, error) {
	if !CanTransition(current, target) {
		return current, fmt.Errorf("statemachine: illegal transition from %s to %s", current, target)
	}
	return target, nil
}

// IsTerminal reports whether a status has no further legal transitions.
func IsTerminal(status model.BookingStatus) bool {
	_, hasOutgoing := transitions[status]
	return !hasOutgoing
}
