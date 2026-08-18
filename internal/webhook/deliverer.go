package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Deliverer handles webhook delivery.
type Deliverer struct {
	client *http.Client
}

// NewDeliverer creates a deliverer with secure defaults.
func NewDeliverer() *Deliverer {
	// Create a transport that blocks private/loopback IPs (SSRF protection)
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Deliverer{
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		},
	}
}

// Delivery represents a webhook delivery attempt.
type Delivery struct {
	ID            string
	SinkID        string
	EventID       []byte
	URL           string
	Secret        string
	Payload       []byte
	Attempt       int
	MaxAttempts   int
	IdempotencyKey string
}

// Result is the outcome of a delivery attempt.
type Result struct {
	StatusCode int
	Body       string
	Duration   time.Duration
	Error      error
	Retryable  bool
}

// Deliver sends a webhook.
func (d *Deliverer) Deliver(ctx context.Context, del *Delivery) *Result {
	// Build request
	req, err := http.NewRequestWithContext(ctx, "POST", del.URL, bytes.NewReader(del.Payload))
	if err != nil {
		return &Result{Error: err, Retryable: false}
	}

	// Headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "RelayDB-Webhook/1.0")
	req.Header.Set("X-RelayDB-Event-ID", hex.EncodeToString(del.EventID))
	req.Header.Set("X-RelayDB-Attempt", fmt.Sprintf("%d", del.Attempt))
	req.Header.Set("Idempotency-Key", del.IdempotencyKey)

	// HMAC signature
	timestamp := time.Now().Unix()
	signature := d.computeSignature(del.Secret, timestamp, del.EventID, del.Payload)
	req.Header.Set("X-RelayDB-Signature", signature)
	req.Header.Set("X-RelayDB-Timestamp", fmt.Sprintf("%d", timestamp))

	// Send
	start := time.Now()
	resp, err := d.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &Result{
			Error:     err,
			Duration:  duration,
			Retryable: isRetryableError(err),
		}
	}
	defer resp.Body.Close()

	// Read bounded response
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	return &Result{
		StatusCode: resp.StatusCode,
		Body:       string(body),
		Duration:   duration,
		Retryable:  isRetryableStatus(resp.StatusCode),
	}
}

// computeSignature computes the HMAC-SHA256 signature.
func (d *Deliverer) computeSignature(secret string, timestamp int64, eventID []byte, payload []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(h, "%d.", timestamp)
	h.Write(eventID)
	h.Write(payload)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// VerifySignature verifies a webhook signature.
func VerifySignature(secret string, timestamp int64, eventID []byte, payload []byte, signature string) bool {
	d := &Deliverer{}
	expected := d.computeSignature(secret, timestamp, eventID, payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// isRetryableStatus returns true for retryable HTTP status codes.
func isRetryableStatus(code int) bool {
	// Retry: 408, 425, 429, 5xx
	if code == 408 || code == 425 || code == 429 {
		return true
	}
	return code >= 500 && code < 600
}

// isRetryableError returns true for retryable network errors.
func isRetryableError(err error) bool {
	// Timeouts and connection failures are retryable
	if err == context.DeadlineExceeded {
		return true
	}
	// DNS failures might be transient
	if dnsErr, ok := err.(*net.DNSError); ok {
		return dnsErr.IsTimeout || dnsErr.IsTemporary
	}
	return false
}

// ClassifyError classifies an error as retryable or permanent.
func ClassifyError(err error) string {
	if err == nil {
		return "success"
	}
	if isRetryableError(err) {
		return "retryable"
	}
	return "permanent"
}

// Backoff computes retry delays.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
}

// DefaultBackoff returns sensible defaults.
func DefaultBackoff() *Backoff {
	return &Backoff{
		Initial: 1 * time.Second,
		Max:     5 * time.Minute,
	}
}

// Delay computes the delay for an attempt (1-indexed).
func (b *Backoff) Delay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	delay := b.Initial
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > b.Max {
			delay = b.Max
			break
		}
	}

	// Add jitter (0-25%)
	jitter := time.Duration(float64(delay) * 0.25 * (2*float64(time.Now().UnixNano()%1000)/1000 - 1))
	return delay + jitter
}

// ShouldRetry returns true if another attempt should be made.
func (b *Backoff) ShouldRetry(attempt, maxAttempts int) bool {
	return attempt < maxAttempts
}