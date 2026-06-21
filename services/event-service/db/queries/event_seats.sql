-- name: DecrementAvailableSeats :execrows
-- Compare-and-swap decrement: only succeeds if available_seats is still high
-- enough to cover the requested count. This is the design doc's "Layer 2"
-- defense against overbooking (Section 3.3.1 / 4.3) — it does not trust the
-- caller's Redis lock to have been sufficient on its own.
UPDATE events
SET available_seats = available_seats - sqlc.arg(seat_count)::int
WHERE event_id = sqlc.arg(event_id)::uuid
    AND available_seats >= sqlc.arg(seat_count)::int;
-- name: GetEventSeatInfo :one
-- Used to validate an event exists and report current availability before
-- a booking attempt even reaches the locking stage.
SELECT event_id,
    total_seats,
    available_seats
FROM events
WHERE event_id = sqlc.arg(event_id)::uuid;
-- name: IncrementAvailableSeats :execrows
-- Used for the compensating action when a booking is cancelled or expires
-- after having already decremented inventory (e.g. an admin-issued refund
-- path that reached CONFIRMED before being reversed).
UPDATE events
SET available_seats = available_seats + sqlc.arg(seat_count)::int
WHERE event_id = sqlc.arg(event_id)::uuid
    AND available_seats + sqlc.arg(seat_count)::int <= total_seats;