-- name: CreateBooking :exec
-- Step 3 in the sequence diagram (design doc Section 4.1): persist a PENDING
-- record before attempting the Redis lock, so even a failed lock attempt
-- leaves an auditable row.
INSERT INTO bookings (
        booking_id,
        user_id,
        event_id,
        seat_number,
        booking_status,
        created_at
    )
VALUES (
        sqlc.arg(booking_id)::uuid,
        sqlc.arg(user_id)::uuid,
        sqlc.arg(event_id)::uuid,
        sqlc.arg(seat_number)::varchar,
        sqlc.arg(booking_status)::varchar,
        sqlc.arg(created_at)::timestamptz
    );
-- name: UpdateBookingStatus :execrows
-- Guarded transition: only applies if the row is still in expected_status.
-- This is the database-level optimistic-concurrency check (design doc
-- Section 3.3.1, "Layer 2") — returns 0 affected rows (not an error) if
-- another process already moved this booking out of the expected state.
UPDATE bookings
SET booking_status = sqlc.arg(new_status)::varchar,
    locked_until = sqlc.narg(locked_until)::timestamptz,
    confirmed_at = CASE
        WHEN sqlc.arg(new_status)::varchar = 'CONFIRMED' THEN now()
        ELSE confirmed_at
    END
WHERE booking_id = sqlc.arg(booking_id)::uuid
    AND booking_status = sqlc.arg(expected_status)::varchar;
-- name: GetBookingByID :one
SELECT booking_id,
    user_id,
    event_id,
    seat_number,
    booking_status,
    locked_until,
    created_at,
    confirmed_at,
    cancelled_at
FROM bookings
WHERE booking_id = sqlc.arg(booking_id)::uuid;
-- name: FindExpiredLocks :many
-- Input to the background TTL watcher (design doc Section 3.3.1, "automatic
-- reconciliation") — bookings stuck in LOCKED whose TTL has already passed.
SELECT booking_id,
    user_id,
    event_id,
    seat_number,
    booking_status,
    locked_until,
    created_at,
    confirmed_at,
    cancelled_at
FROM bookings
WHERE booking_status = 'LOCKED'
    AND locked_until < sqlc.arg(as_of)::timestamptz;
-- name: ListBookingsByUser :many
SELECT booking_id,
    user_id,
    event_id,
    seat_number,
    booking_status,
    locked_until,
    created_at,
    confirmed_at,
    cancelled_at
FROM bookings
WHERE user_id = sqlc.arg(user_id)::uuid
ORDER BY created_at DESC;