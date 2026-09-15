// Package push delivers breaking notifications.
//
// Two senders exist: an FCM HTTP v1 sender for production and a dry-run sender used in
// local/staging environments where no credentials exist. Notification payloads carry an
// article ID, never the whole article (§39).
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Message is one notification request.
type Message struct {
	Topic     string  `json:"topic"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	ArticleID string  `json:"articleId"`
	Score     float64 `json:"score"`
}

// Result describes a delivery attempt.
type Result struct {
	Sent  int      `json:"sent"`
	Dry   bool     `json:"dryRun"`
	Notes []string `json:"notes,omitempty"`
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, msg Message) (Result, error)
}

// DryRunSender records notifications without contacting FCM. It is the default in local
// and staging environments so the full pipeline stays testable without credentials.
type DryRunSender struct {
	Log *slog.Logger

	mu   sync.Mutex
	Sent []Message
}

// Send records the message and reports success.
func (d *DryRunSender) Send(_ context.Context, msg Message) (Result, error) {
	d.mu.Lock()
	d.Sent = append(d.Sent, msg)
	d.mu.Unlock()
	if d.Log != nil {
		d.Log.Info("push dry-run", "topic", msg.Topic, "title", msg.Title, "article", msg.ArticleID)
	}
	return Result{Sent: 0, Dry: true, Notes: []string{"push dry-run: no FCM credentials configured"}}, nil
}

// History returns the dry-run outbox (used by tests and the admin preview).
func (d *DryRunSender) History() []Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Message, len(d.Sent))
	copy(out, d.Sent)
	return out
}

// FCMSender implements FCM HTTP v1 with a service-account credential file (§39).
type FCMSender struct {
	ProjectID    string
	Credentials  []byte
	Log          *slog.Logger
	client       *http.Client
	httpClientMu sync.Mutex
}

type fcmMessage struct {
	Message fcmInner `json:"message"`
}

type fcmInner struct {
	Topic        string            `json:"topic"`
	Data         map[string]string `json:"data"`
	Android      map[string]any    `json:"android,omitempty"`
	Notification map[string]string `json:"notification,omitempty"`
}

// Send publishes to an FCM topic. The article ID travels as data so the client can
// deep-link without receiving article content (§39, §40).
func (f *FCMSender) Send(ctx context.Context, msg Message) (Result, error) {
	if len(f.Credentials) == 0 || f.ProjectID == "" {
		return Result{}, fmt.Errorf("push: FCM credentials are not configured")
	}
	if f.client == nil {
		f.httpClientMu.Lock()
		if f.client == nil {
			creds, err := google.CredentialsFromJSON(ctx, f.Credentials,
				"https://www.googleapis.com/auth/firebase.messaging")
			if err != nil {
				f.httpClientMu.Unlock()
				return Result{}, fmt.Errorf("push: parse credentials: %w", err)
			}
			f.client = &http.Client{
				Timeout:   15 * time.Second,
				Transport: &authTransport{base: http.DefaultTransport, src: creds.TokenSource},
			}
		}
		f.httpClientMu.Unlock()
	}

	payload := fcmMessage{Message: fcmInner{
		Topic: msg.Topic,
		Data: map[string]string{
			"articleId": msg.ArticleID,
			"type":      "breaking",
		},
		Android: map[string]any{
			"priority": "high",
			"notification": map[string]any{
				"channel_id": "breaking",
				"tag":        msg.ArticleID,
			},
		},
		Notification: map[string]string{"title": msg.Title, "body": msg.Body},
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", f.ProjectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("push: FCM returned %d: %s", resp.StatusCode, string(respBody))
	}
	return Result{Sent: 1, Notes: []string{"fcm-v1"}}, nil
}

type authTransport struct {
	base http.RoundTripper
	src  oauth2.TokenSource
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if tok, err := a.src.Token(); err == nil && tok != nil && tok.AccessToken != "" {
		clone.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	}
	return a.base.RoundTrip(clone)
}
