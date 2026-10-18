// Package eza provides the official Go SDK for Eza Cloud:
// transactional email, PayEdge M-Pesa STK pushes, and signed webhook verification.
//
// It has zero external dependencies and uses standard net/http, crypto/hmac,
// and encoding/json.
package eza

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	Version          = "0.2.0"
	DefaultEmailURL  = "https://email.eza.co.ke"
	DefaultPayEdgeURL = "https://payedge.eza.co.ke"
)

// FieldError represents a field-level validation issue.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is returned by API calls on non-2xx responses.
type Error struct {
	Status    int          `json:"status"`
	Code      string       `json:"code"`
	RequestID string       `json:"request_id"`
	Fields    []FieldError `json:"fields,omitempty"`
	Message   string       `json:"message"`
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("eza [%s]: %s (status %d)", e.Code, e.Message, e.Status)
	}
	return fmt.Sprintf("eza: %s (status %d)", e.Message, e.Status)
}

// SignatureError is returned when a webhook signature fails verification.
type SignatureError struct {
	Message string
}

func (e *SignatureError) Error() string {
	return "eza webhook signature: " + e.Message
}

// Option configures the Eza Client.
type Option func(*Client)

// WithAPIKey sets the primary API key (used for PayEdge and fallback for Email).
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithEmailKey sets an email-specific API key.
func WithEmailKey(key string) Option {
	return func(c *Client) { c.emailKey = key }
}

// WithEmailURL overrides the email base URL.
func WithEmailURL(rawURL string) Option {
	return func(c *Client) { c.emailURL = strings.TrimRight(rawURL, "/") }
}

// WithPayEdgeURL overrides the PayEdge base URL.
func WithPayEdgeURL(rawURL string) Option {
	return func(c *Client) { c.payedgeURL = strings.TrimRight(rawURL, "/") }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.httpClient = httpClient }
}

// WithMaxRetries sets the maximum retry attempts for 429/503/network drops.
func WithMaxRetries(retries int) Option {
	return func(c *Client) { c.maxRetries = retries }
}

// Client interacts with the Eza Cloud services.
type Client struct {
	apiKey     string
	emailKey   string
	emailURL   string
	payedgeURL string
	httpClient *http.Client
	maxRetries int

	Emails  *EmailsService
	PayEdge *PayEdgeService
}

// New creates an Eza Client with automatic environment detection.
func New(opts ...Option) *Client {
	c := &Client{
		apiKey:     os.Getenv("EZA_API_KEY"),
		emailKey:   os.Getenv("EZA_EMAIL_KEY"),
		emailURL:   strings.TrimRight(os.Getenv("EZA_EMAIL_URL"), "/"),
		payedgeURL: strings.TrimRight(os.Getenv("EZA_PAYEDGE_URL"), "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: 2,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.emailURL == "" {
		c.emailURL = DefaultEmailURL
	}
	if c.payedgeURL == "" {
		c.payedgeURL = DefaultPayEdgeURL
	}
	if c.emailKey == "" {
		c.emailKey = c.apiKey
	}

	c.Emails = &EmailsService{client: c}
	c.PayEdge = &PayEdgeService{client: c}

	return c
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC 4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type apiErrorResponse struct {
	Title      string       `json:"title"`
	Detail     string       `json:"detail"`
	Message    string       `json:"message"`
	Name       string       `json:"name"`
	Code       string       `json:"code"`
	RequestID  string       `json:"request_id"`
	StatusCode int          `json:"statusCode"`
	Errors     []FieldError `json:"errors"`
}

func (c *Client) request(ctx context.Context, product, method, path string, body any, headers map[string]string) ([]byte, error) {
	key := c.apiKey
	baseURL := c.payedgeURL
	if product == "email" {
		key = c.emailKey
		baseURL = c.emailURL
	}

	if key == "" {
		if product == "email" {
			return nil, errors.New("eza: no API key for email: pass WithAPIKey, WithEmailKey, or set EZA_EMAIL_KEY (apps on Eza have it already)")
		}
		return nil, errors.New("eza: no API key for PayEdge: pass WithAPIKey (with payedge scope) or set EZA_API_KEY")
	}

	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("eza: marshal request: %w", err)
		}
	}

	targetURL := baseURL + path

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("eza: create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "eza-go/"+Version)
		if reqBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < c.maxRetries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(250*(1<<attempt)) * time.Millisecond):
					continue
				}
			}
			return nil, &Error{Status: 0, Code: "network_error", Message: err.Error()}
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("eza: read response: %w", readErr)
		}

		if (resp.StatusCode == 429 || resp.StatusCode == 503 || resp.StatusCode == 504) && attempt < c.maxRetries {
			retryDelay := time.Duration(250*(1<<attempt)) * time.Millisecond
			if afterStr := resp.Header.Get("Retry-After"); afterStr != "" {
				if seconds, parseErr := strconv.Atoi(afterStr); parseErr == nil && seconds > 0 {
					if seconds > 10 {
						seconds = 10
					}
					retryDelay = time.Duration(seconds) * time.Second
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
				continue
			}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}

		reqID := resp.Header.Get("Eza-Request-Id")
		var apiErr apiErrorResponse
		_ = json.Unmarshal(respBody, &apiErr)

		if apiErr.RequestID != "" {
			reqID = apiErr.RequestID
		}

		code := apiErr.Code
		if code == "" {
			code = apiErr.Name
		}
		if code == "" {
			code = "http_error"
		}

		msg := apiErr.Detail
		if msg == "" {
			msg = apiErr.Message
		}
		if msg == "" {
			msg = apiErr.Title
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}

		if len(apiErr.Errors) > 0 {
			var errMsgs []string
			for _, e := range apiErr.Errors {
				errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", e.Field, e.Message))
			}
			msg = strings.Join(errMsgs, "; ")
		}

		return nil, &Error{
			Status:    resp.StatusCode,
			Code:      code,
			RequestID: reqID,
			Fields:    apiErr.Errors,
			Message:   msg,
		}
	}
}

func escapePath(s string) string {
	return url.PathEscape(s)
}
