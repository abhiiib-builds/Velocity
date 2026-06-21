-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS bookings (
    booking_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    event_id UUID NOT NULL,
    seat_number VARCHAR(50) NOT NULL,
    booking_status VARCHAR(20) NOT NULL CHECK (
        booking_status IN ('PENDING', 'LOCKED', 'CONFIRMED', 'EXPIRED')
    ),
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_bookings_user_id ON bookings (user_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_bookings_event_id ON bookings (event_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_bookings_locked_until ON bookings (locked_until)
WHERE booking_status = 'LOCKED';
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bookings;
-- +goose StatementEnd