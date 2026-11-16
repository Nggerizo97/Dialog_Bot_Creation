package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/httpserver"
	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/privacy"
)

// Mailer delivers one rendered email.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Recipient is one address a campaign may be sent to, with the proof that its owner
// asked for such emails.
type Recipient struct {
	Email string
	// ConsentAt is when the person opted in. A recipient without it is never emailed.
	ConsentAt time.Time
	// ConsentSource says where they opted in, for example "webchat" or "signup-form".
	ConsentSource string
}

// Result says what happened to one recipient. It names the person only by hash.
type Result struct {
	EmailHash string
	Status    string
}

// Service sends campaigns and takes unsubscribes.
type Service struct {
	store   Store
	mailer  Mailer
	signer  *Signer
	sender  Sender
	baseURL string
}

// NewService checks everything an email needs before any is sent: who it is from and
// a public address for the unsubscribe link.
func NewService(store Store, mailer Mailer, signer *Signer, sender Sender, publicBaseURL string) (*Service, error) {
	if store == nil || mailer == nil || signer == nil {
		return nil, errors.New("campaigns: store, mailer and signer are required")
	}
	if err := sender.validate(); err != nil {
		return nil, err
	}
	u, err := url.Parse(publicBaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("campaigns: PUBLIC_BASE_URL %q is not an http(s) address", publicBaseURL)
	}
	return &Service{store: store, mailer: mailer, signer: signer, sender: sender, baseURL: publicBaseURL}, nil
}

// Handler serves the health checks and the public unsubscribe endpoint.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/unsubscribe", s.unsubscribe)
	mux.Handle("/", httpserver.New("campaigns"))
	return mux
}

// Send delivers a campaign to its recipients, one at a time. For each recipient it
// checks, in this order and right before sending: that the address is an address, that
// its owner opted in, and that it is not on the do-not-contact list. If the list cannot
// be read, the recipient is not emailed.
func (s *Service) Send(ctx context.Context, campaign Campaign, recipients []Recipient) ([]Result, error) {
	if err := campaign.validate(); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(recipients))
	for _, recipient := range recipients {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		email, ok := privacy.NormalizeEmail(recipient.Email)
		if !ok {
			// There is no address to hash, so there is nothing to record about a person.
			results = append(results, Result{Status: StatusFailed})
			continue
		}
		hash := privacy.EmailHash(campaign.WorkspaceID, email)
		status := s.sendOne(ctx, campaign, recipient, email, hash)
		if err := s.store.RecordSend(ctx, campaign.WorkspaceID, campaign.ID, hash, status); err != nil {
			log.Printf("campaign %s: could not record %s for recipient %s: %v", campaign.ID, status, privacy.Short(hash), err)
		}
		results = append(results, Result{EmailHash: hash, Status: status})
	}
	return results, nil
}

func (s *Service) sendOne(ctx context.Context, campaign Campaign, recipient Recipient, email, hash string) string {
	if recipient.ConsentAt.IsZero() {
		return StatusSkippedNoConsent
	}
	// Checked for every recipient at the moment of sending, never once per batch:
	// someone who unsubscribes while a campaign is going out is still skipped.
	suppressed, err := s.store.IsSuppressed(ctx, campaign.WorkspaceID, hash)
	if err != nil {
		log.Printf("campaign %s: do-not-contact list unavailable, not sending to %s: %v", campaign.ID, privacy.Short(hash), err)
		return StatusFailed
	}
	if suppressed {
		return StatusSkippedSuppressed
	}
	link := unsubscribeURL(s.baseURL, s.signer.Token(campaign.WorkspaceID, hash, campaign.ID))
	if err := s.mailer.Send(ctx, render(campaign, s.sender, email, link)); err != nil {
		log.Printf("campaign %s: delivery to %s failed: %v", campaign.ID, privacy.Short(hash), err)
		return StatusFailed
	}
	return StatusSent
}
