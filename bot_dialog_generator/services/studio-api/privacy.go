package main

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/privacy"
)

// This file holds the data-subject side of studio-api: proof of consent, the right to
// erasure, and the audience settings that switch on the age gate (ADR 0006).

const (
	AudienceGeneral = "general"
	AudienceMinors  = "minors"
	maxMinAge       = 21
)

// BotSettings says who a bot is for.
type BotSettings struct {
	Audience string `json:"audience"`
	MinAge   int    `json:"min_age"`
}

func (s BotSettings) validate() error {
	if s.Audience != AudienceGeneral && s.Audience != AudienceMinors {
		return fmt.Errorf("%w: audience must be general or minors", ErrInvalid)
	}
	if s.MinAge < 0 || s.MinAge > maxMinAge {
		return fmt.Errorf("%w: min_age must be between 0 and %d", ErrInvalid, maxMinAge)
	}
	return nil
}

// ConsentRecord is the proof that a chat user accepted the notice. It deliberately has
// no IP address, browser details or name: the conversation ID is enough to find it.
type ConsentRecord struct {
	Channel   string `json:"channel"`
	SubjectID string `json:"subject_id"`
	// Purposes lists what the person opted into, for example {"analytics": true}.
	// Strictly necessary processing is not a purpose: it needs no opt-in.
	Purposes          map[string]bool `json:"purposes"`
	PolicyVersion     string          `json:"policy_version"`
	AgeConfirmed      bool            `json:"age_confirmed"`
	GuardianConsent   bool            `json:"guardian_consent"`
	GuardianReference string          `json:"guardian_reference,omitempty"`
	At                time.Time       `json:"at"`
}

// knownPurposes are the optional purposes a person can opt into. Anything else in a
// consent record is dropped rather than stored.
var knownPurposes = []string{"processing", "analytics", "campaigns"}

func (c *ConsentRecord) normalize() error {
	var err error
	if c.Channel == "" {
		c.Channel = "webchat"
	}
	if c.Channel, err = cleanID("channel", c.Channel); err != nil {
		return err
	}
	if c.SubjectID, err = cleanID("subject_id", c.SubjectID); err != nil {
		return err
	}
	purposes := make(map[string]bool, len(knownPurposes))
	for _, name := range knownPurposes {
		if granted, ok := c.Purposes[name]; ok {
			purposes[name] = granted
		}
	}
	c.Purposes = purposes
	c.PolicyVersion = strings.TrimSpace(c.PolicyVersion)
	c.GuardianReference = strings.TrimSpace(c.GuardianReference)
	if len(c.PolicyVersion) > 64 || len(c.GuardianReference) > maxIDLen {
		return fmt.Errorf("%w: policy_version or guardian_reference is too long", ErrInvalid)
	}
	if c.At.IsZero() {
		c.At = time.Now().UTC()
	}
	return nil
}

// SubjectKind says how the person behind an erasure request is identified.
type SubjectKind string

const (
	// SubjectChatUser is identified by the conversation ID the chat widget created.
	SubjectChatUser SubjectKind = "chat_user"
	// SubjectEmail is identified by the address campaigns were sent to.
	SubjectEmail SubjectKind = "email"
)

// ErasureInput is a verified request to delete what the platform holds about one person
// inside one workspace.
type ErasureInput struct {
	Kind      SubjectKind
	SubjectID string // conversation ID or email address
	Channel   string // chat_user only; "webchat" when empty
	Source    string // "webchat" (the person, from the widget) or "studio" (an owner, on their behalf)
	// Verification says how the person's identity was checked. It is required: nobody's
	// data is deleted because someone else asked.
	Verification string
	RequestedBy  string
}

func (in *ErasureInput) normalize() error {
	in.Verification = strings.TrimSpace(in.Verification)
	if in.Verification == "" || len(in.Verification) > 500 {
		return fmt.Errorf("%w: verification is required (up to 500 characters): say how the person's identity was checked", ErrInvalid)
	}
	switch in.Kind {
	case SubjectChatUser:
		var err error
		if in.SubjectID, err = cleanID("subject_id", in.SubjectID); err != nil {
			return err
		}
		if in.Channel == "" {
			in.Channel = "webchat"
		}
		if in.Channel, err = cleanID("channel", in.Channel); err != nil {
			return err
		}
	case SubjectEmail:
		email, ok := privacy.NormalizeEmail(in.SubjectID)
		if !ok {
			return fmt.Errorf("%w: subject_id must be an email address", ErrInvalid)
		}
		in.SubjectID, in.Channel = email, ""
	default:
		return fmt.Errorf("%w: subject_kind must be chat_user or email", ErrInvalid)
	}
	return nil
}

// subjectHash is the only reference to the person that the ledger and the audit log keep.
func (in ErasureInput) subjectHash(workspaceID string) string {
	id := in.SubjectID
	if in.Kind == SubjectChatUser {
		id = in.Channel + ":" + id
	}
	return privacy.SubjectHash(workspaceID, string(in.Kind), id)
}

// ErasureCategory reports what happened to one kind of data.
type ErasureCategory struct {
	Category string `json:"category"`
	// Action is "deleted", "retained" (kept, with the reason in Detail), "queued" (an
	// erasure event was written for the service that owns the data) or "not_stored".
	Action string `json:"action"`
	Count  int    `json:"count"`
	Detail string `json:"detail,omitempty"`
}

// SubjectRequest is one entry in a workspace's erasure ledger.
type SubjectRequest struct {
	ID           string            `json:"id"`
	WorkspaceID  string            `json:"workspace_id"`
	SubjectKind  SubjectKind       `json:"subject_kind"`
	SubjectHash  string            `json:"subject_hash"`
	Source       string            `json:"source"`
	Verification string            `json:"verification"`
	RequestedBy  string            `json:"requested_by"`
	Status       string            `json:"status"`
	Result       []ErasureCategory `json:"result"`
	CreatedAt    time.Time         `json:"created_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
}

func newSubjectRequest(workspaceID string, in ErasureInput, now time.Time) *SubjectRequest {
	return &SubjectRequest{
		ID: newID(), WorkspaceID: workspaceID, SubjectKind: in.Kind, SubjectHash: in.subjectHash(workspaceID),
		Source: in.Source, Verification: in.Verification, RequestedBy: in.RequestedBy,
		Status: "completed", CreatedAt: now, CompletedAt: &now,
	}
}

const erasureEventType = "data_subject.erasure_requested"

const queuedDetail = "studio-api keeps no copy. An erasure event was queued for the service that owns this data."

// erasureResult lists every category of data about the person. A category the platform
// does not hold in PostgreSQL is "queued": the outbox event tells its owner to delete it.
func erasureResult(in ErasureInput, consents, sends int) []ErasureCategory {
	if in.Kind == SubjectEmail {
		return []ErasureCategory{
			{Category: "campaign_sends", Action: "deleted", Count: sends},
			{Category: "campaign_suppression", Action: "retained", Count: 1, Detail: "Kept as a one-way hash so this address is never contacted again."},
			{Category: "campaign_analytics", Action: "queued", Detail: queuedDetail},
		}
	}
	return []ErasureCategory{
		{Category: "consent_records", Action: "deleted", Count: consents},
		{Category: "conversation_sessions", Action: "queued", Detail: queuedDetail},
		{Category: "conversation_analytics", Action: "queued", Detail: queuedDetail},
		{Category: "knowledge_references", Action: "queued", Detail: queuedDetail},
	}
}

// erasureEvent is the outbox payload for the services that hold the person's data
// outside PostgreSQL (sessions, analytics, vectors). An email address never leaves as
// plain text: its owners key campaign data by the same hash.
func erasureEvent(req *SubjectRequest, in ErasureInput) map[string]any {
	event := map[string]any{
		"request_id":   req.ID,
		"workspace_id": req.WorkspaceID,
		"subject_kind": string(in.Kind),
		"timestamp":    req.CreatedAt,
	}
	if in.Kind == SubjectEmail {
		event["email_hash"] = privacy.EmailHash(req.WorkspaceID, in.SubjectID)
	} else {
		event["channel"] = in.Channel
		event["subject_id"] = in.SubjectID
	}
	return event
}

// campaignSend is a campaign delivery record, keyed by the recipient's email hash.
type campaignSend struct {
	CampaignID string
	EmailHash  string
	Status     string
}

// StaffRequest is a member of staff asking for their own data to be deleted.
type StaffRequest struct {
	ID         string            `json:"id"`
	Subject    string            `json:"subject"`
	Note       string            `json:"note,omitempty"`
	Status     string            `json:"status"` // pending, completed, partially_completed
	Result     []ErasureCategory `json:"result"`
	CreatedAt  time.Time         `json:"created_at"`
	ResolvedAt *time.Time        `json:"resolved_at,omitempty"`
	ResolvedBy string            `json:"resolved_by,omitempty"`
}

const maxNoteLen = 1000

func cleanNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if len(note) > maxNoteLen {
		return "", fmt.Errorf("%w: note must be at most %d characters", ErrInvalid, maxNoteLen)
	}
	return note, nil
}

// staffResult describes what executing a staff request did. keptAsLastOwner names the
// areas where the person is the only owner: removing them would orphan the area.
func staffResult(removed int, keptAsLastOwner []string) ([]ErasureCategory, string) {
	result := []ErasureCategory{{Category: "workspace_access", Action: "deleted", Count: removed}}
	status := "completed"
	if len(keptAsLastOwner) > 0 {
		status = "partially_completed"
		result = append(result, ErasureCategory{
			Category: "workspace_access", Action: "retained", Count: len(keptAsLastOwner),
			Detail: "Still the only owner of: " + strings.Join(keptAsLastOwner, ", ") + ". Make someone else an owner, then run this request again.",
		})
	}
	return append(result,
		ErasureCategory{Category: "audit_log", Action: "retained", Detail: "Kept for security and accountability for the period in the privacy policy."},
		ErasureCategory{Category: "authored_content", Action: "retained", Detail: "Bots and flows belong to the organization, not to the person who built them."},
		ErasureCategory{Category: "identity_account", Action: "not_stored", Detail: "The sign-in account and its groups live in the identity provider. Ask IT to remove them there."},
	), status
}

// --- HTTP ---

func (h *studio) updateBotSettings(w http.ResponseWriter, r *http.Request, s scope) {
	var body BotSettings
	if !decodeBody(w, r, &body) {
		return
	}
	bot, err := h.store.UpdateBotSettings(r.Context(), s.workspaceID, r.PathValue("botID"), body)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.auditDone(s.principal, s.workspaceID, fmt.Sprintf("bot.audience_set %s min_age=%d", bot.Audience, bot.MinAge), r)
	writeJSON(w, http.StatusOK, bot)
}

func (h *studio) listSubjectRequests(w http.ResponseWriter, r *http.Request, s scope) {
	list, err := h.store.ListSubjectRequests(r.Context(), s.workspaceID)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type erasureBody struct {
	SubjectKind  SubjectKind `json:"subject_kind"`
	SubjectID    string      `json:"subject_id"`
	Channel      string      `json:"channel"`
	Verification string      `json:"verification"`
}

// eraseSubject deletes what the workspace holds about one person. An owner calls it
// after checking that the request really comes from that person.
func (h *studio) eraseSubject(w http.ResponseWriter, r *http.Request, s scope) {
	var body erasureBody
	if !decodeBody(w, r, &body) {
		return
	}
	req, err := h.store.EraseSubject(r.Context(), s.workspaceID, ErasureInput{
		Kind: body.SubjectKind, SubjectID: body.SubjectID, Channel: body.Channel,
		Source: "studio", Verification: body.Verification, RequestedBy: s.principal.Subject,
	})
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	// The audit entry names the person only by hash, like the ledger.
	h.auditDone(s.principal, s.workspaceID, "data_subject.erased "+string(req.SubjectKind)+" "+privacy.Short(req.SubjectHash), r)
	writeJSON(w, http.StatusCreated, req)
}

func (h *studio) createStaffRequest(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 && !decodeBody(w, r, &body) {
		return
	}
	req, err := h.store.CreateStaffRequest(r.Context(), p.Subject, body.Note)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.auditDone(p, "", "data_deletion.requested "+req.ID, r)
	writeJSON(w, http.StatusCreated, req)
}

func (h *studio) listMyStaffRequests(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	list, err := h.store.ListStaffRequests(r.Context(), p.Subject)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// acceptTerms records, in the audit log, that the caller accepted a version of the
// terms of use and privacy policy.
func (h *studio) acceptTerms(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	version, err := cleanID("version", body.Version)
	if err != nil || len(version) > 64 {
		writeProblem(w, http.StatusBadRequest, "Invalid Request", "version must be 1-64 characters without spaces or slashes", r.URL.Path)
		return
	}
	if err := h.audit(p, "", "terms.accepted "+version, r); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *studio) adminListStaffRequests(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListStaffRequests(r.Context(), "")
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *studio) adminExecuteStaffRequest(w http.ResponseWriter, r *http.Request, p *Principal) {
	req, err := h.store.ExecuteStaffRequest(r.Context(), r.PathValue("requestID"), p.Subject)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.auditDone(p, "", "data_deletion.executed "+req.ID+" "+req.Status, r)
	writeJSON(w, http.StatusOK, req)
}

// internalCaller is the audit subject for requests that arrive from the channel gateway.
const internalCaller = "service:channel-gateway"

// internal authorizes service-to-service routes with the shared INTERNAL_API_TOKEN. The
// workspace comes from the gateway's channel binding, never from the browser.
func (h *studio) internal(token string, handle func(w http.ResponseWriter, r *http.Request, workspaceID string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, presented, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="bot-dialog-generator-internal"`)
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "This route is for platform services.", r.URL.Path)
			return
		}
		workspaceID := r.PathValue("workspaceID")
		exists, err := h.store.WorkspaceExists(r.Context(), workspaceID)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if !exists {
			notFound(w, r)
			return
		}
		handle(w, r, workspaceID)
	})
}

func (h *studio) internalRecordConsent(w http.ResponseWriter, r *http.Request, workspaceID string) {
	var body ConsentRecord
	if !decodeBody(w, r, &body) {
		return
	}
	body.At = time.Time{} // the server's clock is the proof, not the browser's
	if err := h.store.RecordConsent(r.Context(), workspaceID, body); err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// internalEraseSubject serves the "Delete my data" option of the chat widget. Holding
// the conversation ID is the verification: the request can only delete what that ID
// points to.
func (h *studio) internalEraseSubject(w http.ResponseWriter, r *http.Request, workspaceID string) {
	var body struct {
		Channel   string `json:"channel"`
		SubjectID string `json:"subject_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	req, err := h.store.EraseSubject(r.Context(), workspaceID, ErasureInput{
		Kind: SubjectChatUser, SubjectID: body.SubjectID, Channel: body.Channel,
		Source: "webchat", Verification: "The request came from the chat session that holds this conversation ID.",
		RequestedBy: "data-subject",
	})
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	entry := AuditEntry{Subject: internalCaller, WorkspaceID: workspaceID, Action: "data_subject.erased chat_user " + privacy.Short(req.SubjectHash), Resource: r.URL.Path}
	if err := h.store.AppendAudit(r.Context(), entry); err != nil {
		// The data is already gone and the ledger says so; a missing audit line must not
		// tell the person their request failed.
		log.Printf("audit write failed for %q: %v", entry.Action, err)
	}
	writeJSON(w, http.StatusCreated, req)
}
