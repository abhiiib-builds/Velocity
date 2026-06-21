package eventclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Client calls Event Service's internal seat-adjustment endpoints over HTTP.
// This is the real cross-service boundary from the design doc's
// database-per-service rule (Section 2.3): Booking Service never touches
// Event Service's database directly, only this client. If Event Service's
// schema changes tomorrow, nothing outside this file needs to know.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			// A short timeout here matters: this call sits on Booking
			// Service's critical path (design doc Section 2.1 — "synchronous
			// for the critical path"), so a slow or hung Event Service must
			// not be allowed to stall a booking confirmation indefinitely.
			Timeout: 3 * time.Second,
		},
	}
}

type seatAdjustmentRequest struct {
	Count int `json:"count"`
}

type seatAdjustmentResponse struct {
	Applied bool `json:"applied"`
}

// DecrementSeats calls Event Service to perform the compare-and-swap seat
// decrement (design doc Section 4.3, "Layer 2" defense). Returns
// applied=false (not an error) when there wasn't enough inventory — that is
// a legitimate business outcome the caller must branch on, not a failure.
func (c *Client) DecrementSeats(ctx context.Context, eventID uuid.UUID, count int) (applied bool, err error) {
	return c.callSeatEndpoint(ctx, "decrement-seats", eventID, count)
}

// IncrementSeats calls Event Service's compensating endpoint, used when a
// confirmed booking is later cancelled/refunded.
func (c *Client) IncrementSeats(ctx context.Context, eventID uuid.UUID, count int) (applied bool, err error) {
	return c.callSeatEndpoint(ctx, "increment-seats", eventID, count)
}

func (c *Client) callSeatEndpoint(ctx context.Context, action string, eventID uuid.UUID, count int) (bool, error) {
	body, err := json.Marshal(seatAdjustmentRequest{Count: count})
	if err != nil {
		return false, fmt.Errorf("eventclient: failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/internal/events/%s/%s", c.baseURL, eventID, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("eventclient: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network-level failure (timeout, connection refused, DNS, etc.) —
		// the caller (BookingService) must decide what "Event Service is
		// unreachable" means for the booking in progress. We do not swallow
		// this or guess; we surface it as an error.
		return false, fmt.Errorf("eventclient: request to event service failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("eventclient: event service returned status %d", resp.StatusCode)
	}

	var result seatAdjustmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("eventclient: failed to decode response: %w", err)
	}
	return result.Applied, nil
}
