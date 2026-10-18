package eza

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const defaultTolerance = 300 * time.Second

// WebhookEvent represents an incoming Eza webhook payload.
type WebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Livemode  bool            `json:"livemode"`
	Data      json.RawMessage `json:"data"`
}

// PaymentEventData holds payment-specific webhook fields.
type PaymentEventData struct {
	ID                string            `json:"id"`
	ConfigID          string            `json:"config_id"`
	CheckoutRequestID *string           `json:"checkout_request_id"`
	MerchantRequestID *string           `json:"merchant_request_id"`
	AmountKES         int               `json:"amount_kes"`
	Phone             string            `json:"phone"`
	AccountReference  string            `json:"account_reference"`
	Status            TransactionStatus `json:"status"`
	ResultCode        *int              `json:"result_code"`
	ResultDescription *string           `json:"result_description"`
	MpesaReceipt      *string           `json:"mpesa_receipt"`
	CreatedAt         time.Time         `json:"created_at"`
	SettledAt         *time.Time        `json:"settled_at"`
}

// VerifyOption configures webhook verification.
type VerifyOption func(*verifyConfig)

type verifyConfig struct {
	tolerance time.Duration
	now       time.Time
}

// WithTolerance sets the acceptable timestamp drift.
func WithTolerance(t time.Duration) VerifyOption {
	return func(c *verifyConfig) { c.tolerance = t }
}

// WithNow overrides the current time (used for reproducible testing).
func WithNow(t time.Time) VerifyOption {
	return func(c *verifyConfig) { c.now = t }
}

// VerifyWebhook validates the Eza-Signature header against the raw body bytes and returns the event.
func VerifyWebhook(body []byte, header string, secret string, opts ...VerifyOption) (*WebhookEvent, error) {
	if secret == "" {
		return nil, errors.New("eza: webhook secret is required")
	}
	if header == "" {
		return nil, &SignatureError{Message: "missing Eza-Signature header"}
	}

	cfg := verifyConfig{
		tolerance: defaultTolerance,
		now:       time.Now(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	var timestampStr string
	var signatures [][]byte

	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		k, v := kv[0], kv[1]
		if k == "t" {
			timestampStr = v
		} else if k == "v1" {
			if decoded, err := hex.DecodeString(v); err == nil {
				signatures = append(signatures, decoded)
			}
		}
	}

	if timestampStr == "" || len(signatures) == 0 {
		return nil, &SignatureError{Message: "malformed Eza-Signature header"}
	}

	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return nil, &SignatureError{Message: "invalid timestamp format"}
	}

	eventTime := time.Unix(ts, 0)
	diff := cfg.now.Sub(eventTime)
	if math.Abs(diff.Seconds()) > cfg.tolerance.Seconds() {
		return nil, &SignatureError{Message: "webhook timestamp is outside tolerance; possible replay"}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestampStr))
	mac.Write([]byte("."))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)

	matched := false
	for _, sig := range signatures {
		if hmac.Equal(sig, expectedMAC) {
			matched = true
			break
		}
	}

	if !matched {
		return nil, &SignatureError{Message: "signature does not match"}
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("eza: parse webhook event: %w", err)
	}

	return &event, nil
}
