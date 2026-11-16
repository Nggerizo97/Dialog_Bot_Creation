package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/privacy"
)

const (
	testWorkspace = "ws-customer-service"
	testKey       = "test-signing-key-0123456789abcdef"
)

var (
	testSender   = Sender{LegalName: "Example Bank S.A.", PostalAddress: "Calle 1 # 2-3, Bogotá, Colombia", From: "news@example.com", UnsubscribeMailbox: "unsubscribe@example.com"}
	testCampaign = Campaign{ID: "spring-offer", WorkspaceID: testWorkspace, Subject: "Novedades de abril", TextBody: "Hello,\nHere is what is new.", HTMLBody: "<p>Hello,<br>Here is what is new.</p>"}
	optedIn      = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
)

// recordingMailer keeps what was sent. onSend, when set, runs before each delivery.
type recordingMailer struct {
	sent   []Message
	onSend func(msg Message)
	fail   map[string]bool
}

func (m *recordingMailer) Send(_ context.Context, msg Message) error {
	if m.onSend != nil {
		m.onSend(msg)
	}
	if m.fail[msg.To] {
		return errors.New("relay refused the message")
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *recordingMailer) recipients() []string {
	var to []string
	for _, msg := range m.sent {
		to = append(to, msg.To)
	}
	return to
}

func newTestService(t *testing.T, store Store, mailer Mailer) *Service {
	t.Helper()
	signer, err := NewSigner(testKey)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, mailer, signer, testSender, "https://campaigns.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func recipient(email string) Recipient {
	return Recipient{Email: email, ConsentAt: optedIn, ConsentSource: "signup-form"}
}

func statuses(results []Result) []string {
	var out []string
	for _, r := range results {
		out = append(out, r.Status)
	}
	return out
}

func tokenFrom(t *testing.T, msg Message) string {
	t.Helper()
	target := strings.TrimSuffix(strings.TrimPrefix(strings.Split(msg.Header.Get("List-Unsubscribe"), ", ")[0], "<"), ">")
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("token")
}

func TestSendSkipsSuppressedAndUnconsentedRecipients(t *testing.T) {
	store, mailer := NewMemoryStore(), &recordingMailer{}
	service := newTestService(t, store, mailer)
	ctx := context.Background()
	if err := store.Suppress(ctx, testWorkspace, privacy.EmailHash(testWorkspace, "opted.out@example.com"), ReasonUnsubscribe); err != nil {
		t.Fatal(err)
	}

	results, err := service.Send(ctx, testCampaign, []Recipient{
		recipient("ana@example.com"),
		recipient("  Opted.Out@Example.com "), // the list matches however the address is written
		{Email: "never.asked@example.com"},    // no opt-in on record
		recipient("not-an-address"),
		recipient("luis@example.com"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{StatusSent, StatusSkippedSuppressed, StatusSkippedNoConsent, StatusFailed, StatusSent}
	if got := statuses(results); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("statuses = %v, want %v", got, want)
	}
	if got := mailer.recipients(); strings.Join(got, ",") != "ana@example.com,luis@example.com" {
		t.Errorf("emails went to %v", got)
	}

	// The delivery log covers everyone who has an address, by hash only.
	log := store.Sends()
	if len(log) != 4 {
		t.Fatalf("delivery log has %d entries, want 4: %+v", len(log), log)
	}
	for _, entry := range log {
		if strings.Contains(entry.EmailHash, "@") || len(entry.EmailHash) != 64 || entry.CampaignID != "spring-offer" || entry.WorkspaceID != testWorkspace {
			t.Errorf("log entry = %+v", entry)
		}
	}
}

func TestSuppressionIsCheckedRightBeforeEachSend(t *testing.T) {
	store, mailer := NewMemoryStore(), &recordingMailer{}
	service := newTestService(t, store, mailer)
	ctx := context.Background()
	// luis unsubscribes while the campaign is going out, after the batch started.
	mailer.onSend = func(msg Message) {
		if msg.To == "ana@example.com" {
			_ = store.Suppress(ctx, testWorkspace, privacy.EmailHash(testWorkspace, "luis@example.com"), ReasonUnsubscribe)
		}
	}
	results, err := service.Send(ctx, testCampaign, []Recipient{recipient("ana@example.com"), recipient("luis@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(results); got[0] != StatusSent || got[1] != StatusSkippedSuppressed {
		t.Errorf("statuses = %v, want luis skipped", got)
	}
	if len(mailer.sent) != 1 {
		t.Errorf("%d emails sent, want 1", len(mailer.sent))
	}
}

type unreadableStore struct{ *MemoryStore }

func (unreadableStore) IsSuppressed(context.Context, string, string) (bool, error) {
	return false, errors.New("database unavailable")
}

func TestNothingIsSentWhenTheDoNotContactListIsUnavailable(t *testing.T) {
	mailer := &recordingMailer{}
	service := newTestService(t, unreadableStore{NewMemoryStore()}, mailer)
	results, err := service.Send(context.Background(), testCampaign, []Recipient{recipient("ana@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != StatusFailed || len(mailer.sent) != 0 {
		t.Errorf("status = %s, emails sent = %d; want failed and none", results[0].Status, len(mailer.sent))
	}
}

func TestEveryEmailCarriesAnUnsubscribeLinkAndHeaders(t *testing.T) {
	mailer := &recordingMailer{}
	service := newTestService(t, NewMemoryStore(), mailer)
	if _, err := service.Send(context.Background(), testCampaign, []Recipient{recipient("ana@example.com")}); err != nil {
		t.Fatal(err)
	}
	msg := mailer.sent[0]

	header := msg.Header.Get("List-Unsubscribe")
	if !strings.HasPrefix(header, "<https://campaigns.example.com/unsubscribe?token=") || !strings.HasSuffix(header, ", <mailto:unsubscribe@example.com?subject=unsubscribe>") {
		t.Errorf("List-Unsubscribe = %q", header)
	}
	if got := msg.Header.Get("List-Unsubscribe-Post"); got != "List-Unsubscribe=One-Click" {
		t.Errorf("List-Unsubscribe-Post = %q", got)
	}
	link := strings.TrimSuffix(strings.TrimPrefix(strings.Split(header, ", ")[0], "<"), ">")
	// Neither the link nor the token shows the address.
	if strings.Contains(link, "ana") || strings.Contains(link, "%40") {
		t.Errorf("the unsubscribe link exposes the address: %s", link)
	}

	// The link is visible in both versions of the body, with the sender's identity.
	if !strings.Contains(msg.TextBody, "Unsubscribe with one click: "+link) || !strings.Contains(msg.TextBody, "Example Bank S.A. · Calle 1 # 2-3, Bogotá, Colombia") {
		t.Errorf("text body has no unsubscribe link or sender identity:\n%s", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, `href="`+strings.ReplaceAll(link, "&", "&amp;")+`"`) || !strings.Contains(msg.HTMLBody, ">Unsubscribe with one click</a>") {
		t.Errorf("html body has no unsubscribe link:\n%s", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, "display:none") || strings.Contains(msg.HTMLBody, "font-size:1px") {
		t.Error("the unsubscribe link must not be hidden")
	}

	// What goes on the wire parses as a message with both headers and both bodies.
	raw, err := msg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the rendered message does not parse: %v", err)
	}
	if parsed.Header.Get("List-Unsubscribe") != header || parsed.Header.Get("List-Unsubscribe-Post") != "List-Unsubscribe=One-Click" {
		t.Errorf("wire headers = %v", parsed.Header)
	}
	if subject, _ := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject")); subject != "Novedades de abril" {
		t.Errorf("subject = %q", subject)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q (%v)", mediaType, err)
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	for _, wantType := range []string{"text/plain", "text/html"} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("%s part: %v", wantType, err)
		}
		// multipart.Reader decodes quoted-printable parts itself.
		body, _ := io.ReadAll(part)
		if !strings.HasPrefix(part.Header.Get("Content-Type"), wantType) || !strings.Contains(string(body), "nsubscribe") {
			t.Errorf("%s part: type %q, body %q", wantType, part.Header.Get("Content-Type"), body)
		}
	}
}

func TestPlainTextOnlyCampaignStillHasTheLink(t *testing.T) {
	mailer := &recordingMailer{}
	service := newTestService(t, NewMemoryStore(), mailer)
	campaign := testCampaign
	campaign.HTMLBody = ""
	if _, err := service.Send(context.Background(), campaign, []Recipient{recipient("ana@example.com")}); err != nil {
		t.Fatal(err)
	}
	raw, err := mailer.sent[0].Bytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	if !strings.Contains(string(body), "Unsubscribe with one click: https://campaigns.example.com/unsubscribe?token=") {
		t.Errorf("body = %q", body)
	}
}

func TestUnsubscribeLinkStopsFutureEmailsInOneClick(t *testing.T) {
	store, mailer := NewMemoryStore(), &recordingMailer{}
	service := newTestService(t, store, mailer)
	ctx := context.Background()
	if _, err := service.Send(ctx, testCampaign, []Recipient{recipient("ana@example.com"), recipient("luis@example.com")}); err != nil {
		t.Fatal(err)
	}
	handler := service.Handler()

	// ana clicks the link in the body: one GET, no sign-in, no form.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unsubscribe?token="+url.QueryEscape(tokenFrom(t, mailer.sent[0])), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "You are unsubscribed") {
		t.Fatalf("GET: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<form") || strings.Contains(rec.Body.String(), "<input") {
		t.Error("the confirmation page asks for more input")
	}
	if !strings.Contains(rec.Body.String(), `lang="en"`) || !strings.Contains(rec.Body.String(), "Example Bank S.A.") {
		t.Errorf("the page has no language or sender identity: %s", rec.Body.String())
	}

	// luis's mail app sends the RFC 8058 one-click POST.
	post := httptest.NewRequest(http.MethodPost, "/unsubscribe?token="+url.QueryEscape(tokenFrom(t, mailer.sent[1])), strings.NewReader("List-Unsubscribe=One-Click"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// The next campaign reaches neither of them, and still reaches carla.
	mailer.sent = nil
	next := testCampaign
	next.ID = "summer-offer"
	results, err := service.Send(ctx, next, []Recipient{recipient("ana@example.com"), recipient("luis@example.com"), recipient("carla@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{StatusSkippedSuppressed, StatusSkippedSuppressed, StatusSent}
	if got := statuses(results); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("statuses = %v, want %v", got, want)
	}
	if got := mailer.recipients(); len(got) != 1 || got[0] != "carla@example.com" {
		t.Errorf("emails went to %v", got)
	}

	// Unsubscribing twice is not an error.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unsubscribe?token="+url.QueryEscape(tokenFrom(t, service.lastMessageFor(t, "ana@example.com"))), nil))
	if rec.Code != http.StatusOK {
		t.Errorf("second unsubscribe: status = %d", rec.Code)
	}
}

// lastMessageFor renders the email a recipient would get, to obtain their token.
func (s *Service) lastMessageFor(t *testing.T, email string) Message {
	t.Helper()
	hash := privacy.EmailHash(testCampaign.WorkspaceID, email)
	return render(testCampaign, s.sender, email, unsubscribeURL(s.baseURL, s.signer.Token(testCampaign.WorkspaceID, hash, testCampaign.ID)))
}

func TestUnsubscribeIsLimitedToItsWorkspace(t *testing.T) {
	store := NewMemoryStore()
	service := newTestService(t, store, &recordingMailer{})
	hash := privacy.EmailHash(testWorkspace, "ana@example.com")
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unsubscribe?token="+url.QueryEscape(service.signer.Token(testWorkspace, hash, "c1")), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	ctx := context.Background()
	if ok, _ := store.IsSuppressed(ctx, testWorkspace, hash); !ok {
		t.Error("the opt-out was not recorded in its workspace")
	}
	if ok, _ := store.IsSuppressed(ctx, "ws-hr", hash); ok {
		t.Error("the opt-out leaked into another workspace")
	}
}

func TestForgedOrDamagedTokensAreRefused(t *testing.T) {
	store := NewMemoryStore()
	service := newTestService(t, store, &recordingMailer{})
	hash := privacy.EmailHash(testWorkspace, "ana@example.com")
	valid := service.signer.Token(testWorkspace, hash, "c1")
	otherSigner, _ := NewSigner("another-signing-key-0123456789abcdef")

	payload, _, _ := strings.Cut(valid, ".")
	for name, token := range map[string]string{
		"missing":               "",
		"no signature":          payload,
		"signed by another key": otherSigner.Token(testWorkspace, hash, "c1"),
		"payload swapped":       tokenEncoding.EncodeToString([]byte("ws-hr\x1f"+hash+"\x1fc1")) + "." + strings.SplitN(valid, ".", 2)[1],
		"not base64":            "%%%.%%%",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			service.Handler().ServeHTTP(rec, httptest.NewRequest(method, "/unsubscribe?token="+url.QueryEscape(token), nil))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s (%s): status = %d, want 400", name, method, rec.Code)
			}
		}
	}
	if ok, _ := store.IsSuppressed(context.Background(), "ws-hr", hash); ok {
		t.Error("a forged token changed the do-not-contact list")
	}
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/unsubscribe?token="+valid, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: status = %d, want 405", rec.Code)
	}
}

func TestServiceRefusesToStartWithoutASenderIdentity(t *testing.T) {
	signer, _ := NewSigner(testKey)
	for name, sender := range map[string]Sender{
		"no legal name":     {PostalAddress: "Calle 1", From: "news@example.com"},
		"no postal address": {LegalName: "Example Bank S.A.", From: "news@example.com"},
		"no from mailbox":   {LegalName: "Example Bank S.A.", PostalAddress: "Calle 1"},
	} {
		if _, err := NewService(NewMemoryStore(), &recordingMailer{}, signer, sender, "https://campaigns.example.com"); err == nil {
			t.Errorf("%s: NewService accepted the sender", name)
		}
	}
	if _, err := NewSigner("short"); err == nil {
		t.Error("a short signing key was accepted")
	}
}

func TestProductionNeedsDurableStorageAndRealConfiguration(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	// Local development works out of the box, without sending mail.
	if _, description, err := newServiceFromEnv(context.Background(), env(nil)); err != nil || !strings.Contains(description, "memory") {
		t.Errorf("local: description = %q, err = %v", description, err)
	}
	production := map[string]string{
		"APP_ENV": "production", "UNSUBSCRIBE_SIGNING_KEY": testKey, "PUBLIC_BASE_URL": "https://campaigns.example.com",
		"SENDER_LEGAL_NAME": "Example Bank S.A.", "SENDER_POSTAL_ADDRESS": "Calle 1", "SENDER_FROM": "news@example.com", "SMTP_ADDR": "smtp.example.com:587",
	}
	// Without a database the do-not-contact list would not survive a restart.
	if _, _, err := newServiceFromEnv(context.Background(), env(production)); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("production without DATABASE_URL: err = %v", err)
	}
}
