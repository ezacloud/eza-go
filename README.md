# Eza Cloud for Go

Official Go SDK for [Eza Cloud](https://eza.co.ke): Transactional email, PayEdge M-Pesa STK push, and signed webhook verification. Zero external dependencies.

```bash
go get github.com/ezacloud/eza-go
```

## Email

On Eza Cloud pods, `EZA_EMAIL_KEY` and `EZA_EMAIL_URL` are automatically injected into every application:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/ezacloud/eza-go"
)

func main() {
    client := eza.New() // Reads EZA_EMAIL_KEY & EZA_EMAIL_URL from environment

    res, err := client.Emails.Send(context.Background(), eza.SendEmailParams{
        From:    "Shop <orders@mail.shop.co.ke>",
        To:      []string{"wanjiku@gmail.com"},
        Subject: "Your receipt",
        HTML:    "<p>Thank you for your order.</p>",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Email sent: %s\n", res.ID)
}
```

## PayEdge M-Pesa STK Push

```go
client := eza.New(eza.WithAPIKey(os.Getenv("EZA_API_KEY")))

tx, err := client.PayEdge.STKPush(context.Background(), eza.STKPushParams{
    Phone:            "254712345678",
    AmountKES:        1500,
    AccountReference: "INV1024",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Prompt sent, status: %s\n", tx.Status)
```

## Webhooks

Verify incoming webhooks against raw body bytes:

```go
package main

import (
    "io"
    "net/http"
    "os"

    "github.com/ezacloud/eza-go"
)

func handleWebhook(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "cannot read body", http.StatusBadRequest)
        return
    }

    event, err := eza.VerifyWebhook(body, r.Header.Get("Eza-Signature"), os.Getenv("EZA_WEBHOOK_SECRET"))
    if err != nil {
        http.Error(w, "invalid signature: "+err.Error(), http.StatusBadRequest)
        return
    }

    switch event.Type {
    case "payment.completed":
        // Process payment
    }

    w.WriteHeader(http.StatusOK)
}
```

## Features

- **Zero Transitive Dependencies:** Built exclusively on the Go standard library (`net/http`, `crypto/hmac`, `encoding/json`).
- **Automatic Idempotency:** Automatically generates UUIDv4 `Idempotency-Key` headers on mutating requests.
- **Resilient Retries:** Exponential backoff with rate-limit header parsing on HTTP 429 and 503.
- **Constant-Time Verification:** Webhook signatures checked with `crypto/hmac.Equal`.
