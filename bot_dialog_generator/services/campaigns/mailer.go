package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/smtp"
)

// LogMailer is for local development: it sends nothing and logs that it would have,
// without the address or the body.
type LogMailer struct{}

func (LogMailer) Send(_ context.Context, msg Message) error {
	log.Printf("mail (not sent, local development): subject %q, unsubscribe header %t", msg.Subject, msg.Header.Get("List-Unsubscribe") != "")
	return nil
}

// SMTPMailer delivers through an SMTP relay. Mail apps only show their own one-click
// unsubscribe button for DKIM-signed mail, so the relay must sign for the From domain.
type SMTPMailer struct {
	addr string
	auth smtp.Auth
}

// NewSMTPMailer takes the relay's host:port and optional credentials.
func NewSMTPMailer(addr, username, password string) (*SMTPMailer, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, errors.New("SMTP_ADDR must be host:port")
	}
	m := &SMTPMailer{addr: addr}
	if username != "" {
		m.auth = smtp.PlainAuth("", username, password, host)
	}
	return m, nil
}

func (m *SMTPMailer) Send(_ context.Context, msg Message) error {
	raw, err := msg.Bytes()
	if err != nil {
		return err
	}
	return smtp.SendMail(m.addr, m.auth, msg.From, []string{msg.To}, raw)
}
