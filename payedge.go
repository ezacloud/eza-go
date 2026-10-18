package eza

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// TransactionStatus denotes the lifecycle state of a PayEdge payment.
type TransactionStatus string

const (
	StatusPending   TransactionStatus = "pending"
	StatusCompleted TransactionStatus = "completed"
	StatusFailed    TransactionStatus = "failed"
	StatusCancelled TransactionStatus = "cancelled"
	StatusTimedOut  TransactionStatus = "timed_out"
)

// Transaction models an M-Pesa STK push payment.
type Transaction struct {
	ID                string            `json:"id"`
	ConfigID          string            `json:"config_id"`
	CheckoutRequestID *string           `json:"checkout_request_id"`
	Status            TransactionStatus `json:"status"`
	AmountKES         int               `json:"amount_kes"`
	Phone             string            `json:"phone"`
	AccountReference  string            `json:"account_reference"`
	MpesaReceipt      *string           `json:"mpesa_receipt"`
	ResultDescription *string           `json:"result_description"`
	Livemode          bool              `json:"livemode"`
	CreatedAt         time.Time         `json:"created_at"`
	SettledAt         *time.Time        `json:"settled_at"`
}

// STKPushParams contains arguments for initiating an M-Pesa STK push.
type STKPushParams struct {
	// Phone in formats: 2547XXXXXXXX, 2541XXXXXXXX, 07..., +254...
	Phone string `json:"phone"`
	// AmountKES in whole shillings, minimum 1.
	AmountKES int `json:"amount_kes"`
	// AccountReference (letters, numbers, dashes, up to 12 chars).
	AccountReference string `json:"account_reference"`
	Description      string `json:"description,omitempty"`
	ConfigID         string `json:"config_id,omitempty"`
	IdempotencyKey   string `json:"-"`
}

type stkPushPayload struct {
	Phone            string `json:"phone"`
	AmountKES        int    `json:"amount_kes"`
	AccountReference string `json:"account_reference"`
	Description      string `json:"description,omitempty"`
	ConfigID         string `json:"config_id,omitempty"`
	IdempotencyKey   string `json:"idempotency_key"`
}

// PayEdgeService handles M-Pesa mobile money interactions.
type PayEdgeService struct {
	client *Client
}

// STKPush initiates an M-Pesa prompt on the user's mobile phone.
func (s *PayEdgeService) STKPush(ctx context.Context, params STKPushParams) (*Transaction, error) {
	if params.AmountKES < 1 {
		return nil, errors.New("eza: amount_kes must be at least 1 KES")
	}
	if params.Phone == "" {
		return nil, errors.New("eza: phone is required")
	}
	if params.AccountReference == "" {
		return nil, errors.New("eza: account_reference is required")
	}

	idempotencyKey := params.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = newUUID()
	}

	payload := stkPushPayload{
		Phone:            params.Phone,
		AmountKES:        params.AmountKES,
		AccountReference: params.AccountReference,
		Description:      params.Description,
		ConfigID:         params.ConfigID,
		IdempotencyKey:   idempotencyKey,
	}

	headers := map[string]string{
		"Idempotency-Key": idempotencyKey,
	}

	respBytes, err := s.client.request(ctx, "payedge", "POST", "/v1/stk-push", payload, headers)
	if err != nil {
		return nil, err
	}

	var tx Transaction
	if err := json.Unmarshal(respBytes, &tx); err != nil {
		return nil, err
	}

	return &tx, nil
}

// GetTransaction retrieves a transaction by ID or CheckoutRequestID.
func (s *PayEdgeService) GetTransaction(ctx context.Context, ref string) (*Transaction, error) {
	if ref == "" {
		return nil, errors.New("eza: ref (id or checkout_request_id) is required")
	}

	respBytes, err := s.client.request(ctx, "payedge", "GET", "/v1/transactions/"+escapePath(ref), nil, nil)
	if err != nil {
		return nil, err
	}

	var tx Transaction
	if err := json.Unmarshal(respBytes, &tx); err != nil {
		return nil, err
	}

	return &tx, nil
}
