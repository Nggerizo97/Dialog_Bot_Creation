package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	service, description, err := newServiceFromEnv(context.Background(), os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	// Campaign authoring and scheduling arrive with milestone 8. Until then this
	// service takes unsubscribes; Service.Send is ready for the scheduler to call.
	log.Printf("bot_dialog_generator campaigns listening on :8083 (%s)", description)
	log.Fatal(http.ListenAndServe(":8083", service.Handler()))
}

// newServiceFromEnv reads configuration from the environment:
//
//	APP_ENV                  local (default), dev, staging, production, ...
//	DATABASE_URL             PostgreSQL as an application role (member of bdg_app); required outside local and dev
//	UNSUBSCRIBE_SIGNING_KEY  secret for unsubscribe links, at least 32 characters; required outside local and dev
//	PUBLIC_BASE_URL          public address of this service, used in unsubscribe links; HTTPS outside local and dev
//	SENDER_LEGAL_NAME        legal name printed in every email
//	SENDER_POSTAL_ADDRESS    postal address printed in every email
//	SENDER_FROM              mailbox campaigns are sent from
//	SENDER_UNSUBSCRIBE_MAILBOX  optional mailto: target for List-Unsubscribe
//	SMTP_ADDR, SMTP_USERNAME, SMTP_PASSWORD  relay; unset logs instead of sending (local and dev only)
func newServiceFromEnv(ctx context.Context, getenv func(string) string) (*Service, string, error) {
	appEnv := strings.ToLower(getenv("APP_ENV"))
	local := appEnv == "" || appEnv == "local" || appEnv == "dev"

	var store Store
	storeName := "memory store"
	if databaseURL := getenv("DATABASE_URL"); databaseURL != "" {
		pg, err := NewPostgresStore(ctx, databaseURL)
		if err != nil {
			return nil, "", err
		}
		store, storeName = pg, "postgres"
	} else if local {
		store = NewMemoryStore()
		log.Println("WARNING: no DATABASE_URL. Unsubscribes are kept in memory and lost on restart. Local development only.")
	} else {
		// An unsubscribe that is forgotten on restart means emailing people who opted out.
		return nil, "", fmt.Errorf("DATABASE_URL is required when APP_ENV=%s: the do-not-contact list must survive restarts", appEnv)
	}

	key := getenv("UNSUBSCRIBE_SIGNING_KEY")
	if key == "" {
		if !local {
			return nil, "", fmt.Errorf("UNSUBSCRIBE_SIGNING_KEY is required when APP_ENV=%s", appEnv)
		}
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			return nil, "", err
		}
		key = hex.EncodeToString(random)
		log.Println("WARNING: no UNSUBSCRIBE_SIGNING_KEY. Using a random key: unsubscribe links stop working when this process restarts.")
	}
	signer, err := NewSigner(key)
	if err != nil {
		return nil, "", err
	}

	baseURL := getenv("PUBLIC_BASE_URL")
	if baseURL == "" && local {
		baseURL = "http://localhost:8083"
	}
	if !local && !strings.HasPrefix(baseURL, "https://") {
		return nil, "", fmt.Errorf("PUBLIC_BASE_URL must be an https:// address when APP_ENV=%s", appEnv)
	}

	sender := Sender{
		LegalName:          getenv("SENDER_LEGAL_NAME"),
		PostalAddress:      getenv("SENDER_POSTAL_ADDRESS"),
		From:               getenv("SENDER_FROM"),
		UnsubscribeMailbox: getenv("SENDER_UNSUBSCRIBE_MAILBOX"),
	}
	if local && sender.LegalName == "" && sender.PostalAddress == "" && sender.From == "" {
		sender = Sender{LegalName: "Demo sender (not configured)", PostalAddress: "No postal address configured", From: "campaigns@localhost.invalid"}
	}

	var mailer Mailer = LogMailer{}
	mailerName := "mail is logged, not sent"
	if addr := getenv("SMTP_ADDR"); addr != "" {
		smtpMailer, err := NewSMTPMailer(addr, getenv("SMTP_USERNAME"), getenv("SMTP_PASSWORD"))
		if err != nil {
			return nil, "", err
		}
		mailer, mailerName = smtpMailer, "smtp "+addr
	} else if !local {
		return nil, "", fmt.Errorf("SMTP_ADDR is required when APP_ENV=%s", appEnv)
	}

	service, err := NewService(store, mailer, signer, sender, baseURL)
	if err != nil {
		return nil, "", err
	}
	return service, storeName + ", " + mailerName, nil
}
