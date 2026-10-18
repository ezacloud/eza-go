package eza_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ezacloud/eza-go"
)

type ConformanceFixture struct {
	Webhook struct {
		Secret           string `json:"secret"`
		Timestamp        int64  `json:"timestamp"`
		Header           string `json:"header"`
		Body             string `json:"body"`
		ToleranceSeconds int    `json:"tolerance_seconds"`
		InvalidCases     []struct {
			Name   string `json:"name"`
			Body   string `json:"body"`
			Header string `json:"header"`
			Secret string `json:"secret"`
			Now    int64  `json:"now"`
		} `json:"invalid_cases"`
	} `json:"webhook"`
}

func loadFixtures(t *testing.T) ConformanceFixture {
	t.Helper()
	path := filepath.Join("..", "conformance", "fixtures.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading fixtures: %v", err)
	}
	var f ConformanceFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("failed unmarshaling fixtures: %v", err)
	}
	return f
}

func TestWebhookConformance(t *testing.T) {
	f := loadFixtures(t)
	now := time.Unix(f.Webhook.Timestamp+30, 0)

	event, err := eza.VerifyWebhook(
		[]byte(f.Webhook.Body),
		f.Webhook.Header,
		f.Webhook.Secret,
		eza.WithNow(now),
	)
	if err != nil {
		t.Fatalf("unexpected webhook verification error: %v", err)
	}
	if event.Type != "payment.completed" {
		t.Errorf("expected payment.completed, got %s", event.Type)
	}
	if event.ID != "evt_01" {
		t.Errorf("expected evt_01, got %s", event.ID)
	}

	for _, tc := range f.Webhook.InvalidCases {
		t.Run("invalid_"+tc.Name, func(t *testing.T) {
			_, err := eza.VerifyWebhook(
				[]byte(tc.Body),
				tc.Header,
				tc.Secret,
				eza.WithNow(time.Unix(tc.Now, 0)),
			)
			if err == nil {
				t.Fatalf("expected error for case %s, got nil", tc.Name)
			}
		})
	}
}

func TestPayEdgeSTKPush(t *testing.T) {
	var capturedAuth, capturedIdempotency, capturedMethod string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedAuth = r.Header.Get("Authorization")
		capturedIdempotency = r.Header.Get("Idempotency-Key")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id": "tx_123",
			"config_id": "cfg_1",
			"checkout_request_id": "ws_CO_123",
			"status": "pending",
			"amount_kes": 1500,
			"phone": "+254 712 ••• 678",
			"account_reference": "INV1024",
			"livemode": false,
			"created_at": "2026-10-18T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	client := eza.New(
		eza.WithAPIKey("eza_live_testkey"),
		eza.WithPayEdgeURL(srv.URL),
		eza.WithHTTPClient(srv.Client()),
	)

	tx, err := client.PayEdge.STKPush(context.Background(), eza.STKPushParams{
		Phone:            "254712345678",
		AmountKES:        1500,
		AccountReference: "INV1024",
	})
	if err != nil {
		t.Fatalf("STKPush error: %v", err)
	}

	if tx.ID != "tx_123" {
		t.Errorf("expected tx_123, got %s", tx.ID)
	}
	if capturedMethod != "POST" {
		t.Errorf("expected POST, got %s", capturedMethod)
	}
	if capturedAuth != "Bearer eza_live_testkey" {
		t.Errorf("expected Bearer eza_live_testkey, got %s", capturedAuth)
	}
	if capturedIdempotency == "" {
		t.Error("expected non-empty Idempotency-Key")
	}
	if capturedBody["amount_kes"] != float64(1500) {
		t.Errorf("expected amount_kes 1500, got %v", capturedBody["amount_kes"])
	}
}

func TestEmailSendAndRetry(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"statusCode":429,"name":"rate_limit_exceeded","message":"Slow down"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"mail_01928f3e"}`))
	}))
	defer srv.Close()

	client := eza.New(
		eza.WithEmailKey("eza_mail_key"),
		eza.WithEmailURL(srv.URL),
		eza.WithHTTPClient(srv.Client()),
		eza.WithMaxRetries(2),
	)

	resp, err := client.Emails.Send(context.Background(), eza.SendEmailParams{
		From:    "Shop <orders@mail.shop.co.ke>",
		To:      []string{"wanjiku@gmail.com"},
		Subject: "Your receipt",
		HTML:    "<p>Paid KES 1,500</p>",
		Attachments: []eza.Attachment{
			{Filename: "receipt.txt", Content: []byte("Receipt content")},
		},
	})
	if err != nil {
		t.Fatalf("Emails.Send error: %v", err)
	}

	if resp.ID != "mail_01928f3e" {
		t.Errorf("expected mail_01928f3e, got %s", resp.ID)
	}
	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts after 429 retry, got %d", attempts.Load())
	}
}
