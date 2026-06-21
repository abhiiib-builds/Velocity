package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// SeatLocker acquires and releases per seat distributed locks in Redis
type SeatLocker struct {
	client *redis.Client
	ttl    time.Duration
}

func NewSeatLocker(client *redis.Client, ttlSeconds int) *SeatLocker {
	return &SeatLocker{
		client: client,
		ttl:    time.Duration(ttlSeconds) * time.Second,
	}
}

func seatKey(eventID, seatNumber string) string {
	return fmt.Sprintf("seat-lock:%s:%s", eventID, seatNumber)
}

// AcquireAll attempts to lock every seat in the request atomically as a
// group. If any single seat is already locked, it releases whatever it
// already acquired in this call and returns the seat number that failed.
func (l *SeatLocker) AcquireAll(ctx context.Context, bookingID, eventID string, seats []string) (failedSeat string, err error) {
	acquired := make([]string, 0, len(seats))

	for _, seat := range seats {
		key := seatKey(eventID, seat)
		ok, err := l.client.SetNX(ctx, key, bookingID, l.ttl).Result()
		if err != nil {
			l.releaseAll(ctx, eventID, acquired)
			return seat, fmt.Errorf("lock: redis error acquiring seat %s: %w", seat, err)
		}
		if !ok {
			l.releaseAll(ctx, eventID, acquired)
			return seat, nil
		}
		acquired = append(acquired, seat)
	}

	return "", nil
}

// releaseAll best-effort releases a set of seat locks. Errors are swallowed
// intentionally: a failed release just means the key falls back on its TTL
// and self-expires (design doc Section 4.3) — not a correctness problem.
func (l *SeatLocker) releaseAll(ctx context.Context, eventID string, seats []string) {
	for _, seat := range seats {
		l.client.Del(ctx, seatKey(eventID, seat))
	}
}

// Release removes the locks for a confirmed or explicitly-cancelled booking.
func (l *SeatLocker) Release(ctx context.Context, eventID string, seats []string) error {
	keys := make([]string, len(seats))
	for i, s := range seats {
		keys[i] = seatKey(eventID, s)
	}
	if err := l.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("lock: failed to release seats: %w", err)
	}
	return nil
}

// IsLocked checks current lock state for a single seat.
func (l *SeatLocker) IsLocked(ctx context.Context, eventID, seat string) (bool, error) {
	exists, err := l.client.Exists(ctx, seatKey(eventID, seat)).Result()
	if err != nil {
		return false, fmt.Errorf("lock: failed to check seat status: %w", err)
	}
	return exists > 0, nil
}
