package main

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
)

// Sender identifies who a campaign comes from. The legal name and postal address are
// printed in every email, so they are required before anything is sent.
type Sender struct {
	LegalName     string
	PostalAddress string
	// From is the mailbox campaigns are sent from, e.g. "news@example.com".
	From string
	// UnsubscribeMailbox optionally adds a mailto: target to List-Unsubscribe.
	UnsubscribeMailbox string
}

func (s Sender) validate() error {
	if strings.TrimSpace(s.LegalName) == "" || strings.TrimSpace(s.PostalAddress) == "" {
		return errors.New("campaigns: the sender's legal name and postal address are required (SENDER_LEGAL_NAME, SENDER_POSTAL_ADDRESS)")
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		return fmt.Errorf("campaigns: SENDER_FROM is not a mailbox: %w", err)
	}
	return nil
}

func (s Sender) footerLine() string {
	if s.LegalName == "" {
		return ""
	}
	return s.LegalName + " · " + s.PostalAddress
}

// Campaign is one email sent to many recipients of a workspace.
type Campaign struct {
	ID          string
	WorkspaceID string
	Subject     string
	// TextBody is required. HTMLBody is optional and written by staff, so it is used as is.
	TextBody string
	HTMLBody string
}

func (c Campaign) validate() error {
	if c.ID == "" || c.WorkspaceID == "" {
		return errors.New("campaigns: a campaign needs an ID and a workspace")
	}
	if strings.TrimSpace(c.Subject) == "" || strings.TrimSpace(c.TextBody) == "" {
		return errors.New("campaigns: a campaign needs a subject and a plain-text body")
	}
	if strings.ContainsAny(c.Subject, "\r\n") {
		return errors.New("campaigns: the subject must be a single line")
	}
	return nil
}

// Message is a rendered email, ready for a Mailer.
type Message struct {
	From, To string
	Subject  string
	// Header holds the unsubscribe headers. Mailers must send them unchanged.
	Header   textproto.MIMEHeader
	TextBody string
	HTMLBody string
}

// unsubscribeURL is the link for one recipient. It must be HTTPS for mail apps to
// offer their own one-click unsubscribe button (RFC 8058).
func unsubscribeURL(baseURL, token string) string {
	return strings.TrimRight(baseURL, "/") + "/unsubscribe?token=" + url.QueryEscape(token)
}

// render builds the email for one recipient. The unsubscribe link and the sender's
// identity are added here, after the campaign's own text, so no campaign can be
// written without them.
func render(c Campaign, sender Sender, to, unsubscribe string) Message {
	header := textproto.MIMEHeader{}
	targets := "<" + unsubscribe + ">"
	if sender.UnsubscribeMailbox != "" {
		targets += ", <mailto:" + sender.UnsubscribeMailbox + "?subject=unsubscribe>"
	}
	header.Set("List-Unsubscribe", targets)
	header.Set("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")

	text := strings.TrimRight(c.TextBody, "\n") + "\n\n-- \n" +
		"You are receiving this email because you agreed to receive messages from " + sender.LegalName + ".\n" +
		"Unsubscribe with one click: " + unsubscribe + "\n" +
		sender.footerLine() + "\n"

	msg := Message{From: sender.From, To: to, Subject: c.Subject, Header: header, TextBody: text}
	if c.HTMLBody != "" {
		// The link text says what it does and the link is as easy to see as the body text.
		msg.HTMLBody = c.HTMLBody + "\n" +
			`<hr><p style="font-size:14px;color:#18201f">You are receiving this email because you agreed to receive messages from ` +
			html.EscapeString(sender.LegalName) + `.<br>` +
			`<a href="` + html.EscapeString(unsubscribe) + `" style="color:#0b5c53;text-decoration:underline">Unsubscribe with one click</a><br>` +
			html.EscapeString(sender.footerLine()) + `</p>`
	}
	return msg
}

// Bytes returns the message in Internet Message Format, as SMTP expects it.
func (m Message) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	writeHeader := func(name, value string) { fmt.Fprintf(&buf, "%s: %s\r\n", name, value) }
	writeHeader("From", m.From)
	writeHeader("To", m.To)
	writeHeader("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	writeHeader("MIME-Version", "1.0")
	for _, name := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if value := m.Header.Get(name); value != "" {
			writeHeader(name, value)
		}
	}

	writePart := func(w *quotedprintable.Writer, body string) error {
		if _, err := w.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
			return err
		}
		return w.Close()
	}

	if m.HTMLBody == "" {
		writeHeader("Content-Type", "text/plain; charset=utf-8")
		writeHeader("Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		if err := writePart(quotedprintable.NewWriter(&buf), m.TextBody); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}

	var body bytes.Buffer
	parts := multipart.NewWriter(&body)
	for _, part := range []struct{ contentType, content string }{
		{"text/plain; charset=utf-8", m.TextBody},
		{"text/html; charset=utf-8", m.HTMLBody},
	} {
		w, err := parts.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		if err := writePart(quotedprintable.NewWriter(w), part.content); err != nil {
			return nil, err
		}
	}
	if err := parts.Close(); err != nil {
		return nil, err
	}
	writeHeader("Content-Type", `multipart/alternative; boundary="`+parts.Boundary()+`"`)
	buf.WriteString("\r\n")
	buf.Write(body.Bytes())
	return buf.Bytes(), nil
}
