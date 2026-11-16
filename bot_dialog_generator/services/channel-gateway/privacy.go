package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ConsentRequest is what the chat widget sends when a person accepts the notice.
type ConsentRequest struct {
	UserID string `json:"user_id"`
	// Purposes lists what the person agreed to, for example {"processing": true,
	// "analytics": false}. Unchecked boxes arrive as false, never as true by default.
	Purposes          map[string]bool `json:"purposes"`
	PolicyVersion     string          `json:"policy_version"`
	AgeConfirmed      bool            `json:"age_confirmed"`
	GuardianConsent   bool            `json:"guardian_consent"`
	GuardianReference string          `json:"guardian_reference,omitempty"`
}

// ErasureReceipt tells the person what happened to their request.
type ErasureReceipt struct {
	// Reference identifies the request if the person follows up with the privacy contact.
	Reference string `json:"reference"`
	Status    string `json:"status"`
	// ServerCopy is false when the platform held nothing about the conversation beyond
	// what was just deleted in the browser.
	ServerCopy bool `json:"server_copy"`
}

// PrivacyClient hands consent proof and erasure requests to the service that owns the
// data. The gateway itself stores nothing.
type PrivacyClient interface {
	RecordConsent(ctx context.Context, workspaceID string, consent ConsentRequest) error
	RequestErasure(ctx context.Context, workspaceID, userID string) (ErasureReceipt, error)
}

// errPrivacyRejected means studio-api refused the request as invalid.
var errPrivacyRejected = errors.New("privacy request rejected")

type gatewayConfig struct {
	privacy PrivacyClient
}

// GatewayOption customizes NewGatewayHandler.
type GatewayOption func(*gatewayConfig)

// WithPrivacyClient sets where consent proof and erasure requests go.
func WithPrivacyClient(client PrivacyClient) GatewayOption {
	return func(cfg *gatewayConfig) { cfg.privacy = client }
}

// LocalPrivacyClient keeps consent proof in memory. It is the default for local
// development, where nothing about a conversation is stored on a server at all.
type LocalPrivacyClient struct {
	mu       sync.Mutex
	consents map[string][]ConsentRequest // workspaceID:userID -> records
	requests int
}

func NewLocalPrivacyClient() *LocalPrivacyClient {
	return &LocalPrivacyClient{consents: make(map[string][]ConsentRequest)}
}

func (c *LocalPrivacyClient) RecordConsent(_ context.Context, workspaceID string, consent ConsentRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := workspaceID + ":" + consent.UserID
	c.consents[key] = append(c.consents[key], consent)
	return nil
}

func (c *LocalPrivacyClient) RequestErasure(_ context.Context, workspaceID, userID string) (ErasureReceipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := workspaceID + ":" + userID
	held := len(c.consents[key]) > 0
	delete(c.consents, key)
	c.requests++
	return ErasureReceipt{Reference: fmt.Sprintf("local-%d", c.requests), Status: "completed", ServerCopy: held}, nil
}

// StudioPrivacyClient forwards to studio-api's /internal routes, which keep consent
// proof and run erasures in PostgreSQL under row-level security.
type StudioPrivacyClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewStudioPrivacyClient needs studio-api's address and the shared INTERNAL_API_TOKEN.
func NewStudioPrivacyClient(baseURL, token string) (*StudioPrivacyClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("STUDIO_API_URL %q is not an http(s) address", baseURL)
	}
	if token == "" {
		return nil, errors.New("INTERNAL_API_TOKEN is required when STUDIO_API_URL is set")
	}
	return &StudioPrivacyClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (c *StudioPrivacyClient) post(ctx context.Context, workspaceID, resource string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := c.baseURL + "/internal/workspaces/" + url.PathEscape(workspaceID) + "/" + resource
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("studio-api unreachable: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusBadRequest:
		return errPrivacyRejected
	case res.StatusCode >= 300:
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("studio-api answered %d for %s", res.StatusCode, resource)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

func (c *StudioPrivacyClient) RecordConsent(ctx context.Context, workspaceID string, consent ConsentRequest) error {
	return c.post(ctx, workspaceID, "consents", map[string]any{
		"channel": "webchat", "subject_id": consent.UserID, "purposes": consent.Purposes,
		"policy_version": consent.PolicyVersion, "age_confirmed": consent.AgeConfirmed,
		"guardian_consent": consent.GuardianConsent, "guardian_reference": consent.GuardianReference,
	}, nil)
}

func (c *StudioPrivacyClient) RequestErasure(ctx context.Context, workspaceID, userID string) (ErasureReceipt, error) {
	var ledger struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Result []struct {
			Action string `json:"action"`
			Count  int    `json:"count"`
		} `json:"result"`
	}
	if err := c.post(ctx, workspaceID, "data-deletion-requests", map[string]string{"channel": "webchat", "subject_id": userID}, &ledger); err != nil {
		return ErasureReceipt{}, err
	}
	receipt := ErasureReceipt{Reference: ledger.ID, Status: ledger.Status}
	for _, category := range ledger.Result {
		receipt.ServerCopy = receipt.ServerCopy || (category.Action == "deleted" && category.Count > 0)
	}
	return receipt, nil
}
