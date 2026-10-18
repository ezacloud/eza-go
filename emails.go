package eza

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// Attachment defines a file attached to an email.
type Attachment struct {
	Filename    string `json:"filename"`
	Content     []byte `json:"-"`
	ContentType string `json:"content_type,omitempty"`
}

type attachmentPayload struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

// Tag represents a key-value pair for categorization.
type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SendEmailParams contains fields to dispatch an email.
type SendEmailParams struct {
	From           string            `json:"from"`
	To             []string          `json:"to"`
	Cc             []string          `json:"cc,omitempty"`
	Bcc            []string          `json:"bcc,omitempty"`
	ReplyTo        []string          `json:"reply_to,omitempty"`
	Subject        string            `json:"subject"`
	HTML           string            `json:"html,omitempty"`
	Text           string            `json:"text,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Attachments    []Attachment      `json:"-"`
	Tags           []Tag             `json:"tags,omitempty"`
	IdempotencyKey string            `json:"-"`
}

type sendEmailPayload struct {
	From        string              `json:"from"`
	To          []string            `json:"to"`
	Cc          []string            `json:"cc,omitempty"`
	Bcc         []string            `json:"bcc,omitempty"`
	ReplyTo     []string            `json:"reply_to,omitempty"`
	Subject     string              `json:"subject"`
	HTML        string              `json:"html,omitempty"`
	Text        string              `json:"text,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	Attachments []attachmentPayload `json:"attachments,omitempty"`
	Tags        []Tag               `json:"tags,omitempty"`
}

// SentEmailResponse is returned on successful email creation.
type SentEmailResponse struct {
	ID string `json:"id"`
}

// SentEmail describes an email's current status and delivery.
type SentEmail struct {
	Object    string    `json:"object"`
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        []string  `json:"to"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
	LastEvent string    `json:"last_event"`
	Tags      []Tag     `json:"tags"`
}

// EmailsService handles transactional email delivery.
type EmailsService struct {
	client *Client
}

// Send dispatches one email with automatic idempotency protection.
func (s *EmailsService) Send(ctx context.Context, params SendEmailParams) (*SentEmailResponse, error) {
	if params.From == "" {
		return nil, errors.New("eza: emails.Send requires From")
	}
	if len(params.To) == 0 {
		return nil, errors.New("eza: emails.Send requires To")
	}
	if params.Subject == "" {
		return nil, errors.New("eza: emails.Send requires Subject")
	}
	if params.HTML == "" && params.Text == "" {
		return nil, errors.New("eza: emails.Send requires HTML, Text, or both")
	}

	payload := sendEmailPayload{
		From:    params.From,
		To:      params.To,
		Cc:      params.Cc,
		Bcc:     params.Bcc,
		ReplyTo: params.ReplyTo,
		Subject: params.Subject,
		HTML:    params.HTML,
		Text:    params.Text,
		Headers: params.Headers,
		Tags:    params.Tags,
	}

	if len(params.Attachments) > 0 {
		payload.Attachments = make([]attachmentPayload, len(params.Attachments))
		for i, a := range params.Attachments {
			if a.Filename == "" || a.Content == nil {
				return nil, errors.New("eza: each attachment requires Filename and Content")
			}
			payload.Attachments[i] = attachmentPayload{
				Filename:    a.Filename,
				Content:     base64.StdEncoding.EncodeToString(a.Content),
				ContentType: a.ContentType,
			}
		}
	}

	idempotencyKey := params.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = newUUID()
	}

	headers := map[string]string{
		"Idempotency-Key": idempotencyKey,
	}

	respBytes, err := s.client.request(ctx, "email", "POST", "/emails", payload, headers)
	if err != nil {
		return nil, err
	}

	var resp SentEmailResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Get retrieves metadata and the last delivery event for a sent email.
func (s *EmailsService) Get(ctx context.Context, id string) (*SentEmail, error) {
	if id == "" {
		return nil, errors.New("eza: emails.Get requires id")
	}

	respBytes, err := s.client.request(ctx, "email", "GET", "/emails/"+escapePath(id), nil, nil)
	if err != nil {
		return nil, err
	}

	var email SentEmail
	if err := json.Unmarshal(respBytes, &email); err != nil {
		return nil, err
	}

	return &email, nil
}
