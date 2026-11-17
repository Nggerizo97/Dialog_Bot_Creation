package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/httpserver"
)

type WebchatInbound struct {
	Tenant    string `json:"tenant"`
	Channel   string `json:"channel"`
	UserID    string `json:"user_id"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
	Choice    string `json:"choice,omitempty"`
	// Consent repeats the optional purposes the person opted into, so the gateway can
	// drop what they did not agree to share.
	Consent WebchatConsent `json:"consent"`
	// Context is extra information about the visit. Only the keys in
	// campaignContextKeys are kept, and only with the "campaigns" opt-in.
	Context map[string]string `json:"context,omitempty"`
}

// WebchatConsent lists the optional purposes a chat user opted into. Everything is
// off unless the widget says the person switched it on.
type WebchatConsent struct {
	Analytics bool `json:"analytics"`
	Campaigns bool `json:"campaigns"`
}

// campaignContextKeys is everything the gateway forwards about how a person arrived.
// Anything else a page sends in "context" is dropped here and never reaches the engine.
var campaignContextKeys = []string{"utm_source", "utm_medium", "utm_campaign"}

const (
	maxBodyBytes    = 64 << 10
	maxTextLen      = 4000
	maxContextValue = 120
)

// userIDPattern is the shape of a conversation ID: random, with no meaning. Requiring
// it keeps names, emails and other identifiers out of the user_id field.
var userIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// Demo replies. They say plainly that the data is a sample and that nobody is
// waiting on the other side, so a visitor is never told something that is not true.
const (
	demoBalanceReply = "Sample data: a checking balance of $1,250.50 USD. This demo is not connected to any real account."
	demoHandoffReply = "This demo has no contact center, so no agent will join the chat. A live bot would hand the conversation to a person here."
)

type WebchatOutbound struct {
	ReplyTo  string           `json:"reply_to"`
	Messages []WebchatMessage `json:"messages"`
}

type WebchatMessage struct {
	Kind    string          `json:"kind"` // text, menu
	Text    string          `json:"text,omitempty"`
	Prompt  string          `json:"prompt,omitempty"`
	Options []WebchatOption `json:"options,omitempty"`
}

type WebchatOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type EngineClient interface {
	ProcessMessage(in *botdialoggeneratorv1.InboundMessage) (*botdialoggeneratorv1.OutboundBatch, error)
}

type LocalEngineClient struct{}

func (c *LocalEngineClient) ProcessMessage(in *botdialoggeneratorv1.InboundMessage) (*botdialoggeneratorv1.OutboundBatch, error) {
	batch := &botdialoggeneratorv1.OutboundBatch{
		Tenant:      in.Tenant,
		WorkspaceId: in.WorkspaceId,
		Channel:     in.Channel,
		UserId:      in.UserId,
		ReplyTo:     in.MessageId,
	}

	text := ""
	if t := in.GetText(); t != nil {
		text = t.Value
	} else if ch := in.GetChoice(); ch != nil {
		text = ch.Value
	}

	switch text {
	case "balance":
		batch.Messages = append(batch.Messages, &botdialoggeneratorv1.OutMessage{
			Kind: &botdialoggeneratorv1.OutMessage_Text{
				Text: &botdialoggeneratorv1.Text{Value: demoBalanceReply},
			},
		})
	case "handoff":
		// Ask before transferring: the AI assistant can solve most requests, and a
		// person from the contact center stays one click away.
		batch.Messages = append(batch.Messages, menuMessage(
			"Would you like to chat with our AI assistant or talk to a person from the contact center?",
			&botdialoggeneratorv1.MenuOption{Id: "ai_assistant", Label: "AI assistant"},
			&botdialoggeneratorv1.MenuOption{Id: "contact_center", Label: "Contact center agent"},
		))
	case "ai_assistant":
		batch.Messages = append(batch.Messages,
			textMessage("You're chatting with the AI assistant. AI answers are not connected in this demo yet."),
			menuMessage("You can talk to a person at any time.",
				&botdialoggeneratorv1.MenuOption{Id: "contact_center", Label: "Talk to a person"},
				&botdialoggeneratorv1.MenuOption{Id: "main_menu", Label: "Back to main menu"},
			),
		)
	case "contact_center":
		batch.Messages = append(batch.Messages, textMessage(demoHandoffReply))
	default:
		batch.Messages = append(batch.Messages,
			&botdialoggeneratorv1.OutMessage{
				Kind: &botdialoggeneratorv1.OutMessage_Text{
					Text: &botdialoggeneratorv1.Text{Value: "Welcome to the Bot_Dialog_Generator demo!"},
				},
			},
			&botdialoggeneratorv1.OutMessage{
				Kind: &botdialoggeneratorv1.OutMessage_Menu{
					Menu: &botdialoggeneratorv1.Menu{
						Prompt: "What can we help you with today?",
						Options: []*botdialoggeneratorv1.MenuOption{
							{Id: "balance", Label: "Check balance"},
							{Id: "handoff", Label: "Connect to advisor"},
						},
					},
				},
			},
		)
	}

	return batch, nil
}

func textMessage(value string) *botdialoggeneratorv1.OutMessage {
	return &botdialoggeneratorv1.OutMessage{
		Kind: &botdialoggeneratorv1.OutMessage_Text{Text: &botdialoggeneratorv1.Text{Value: value}},
	}
}

func menuMessage(prompt string, options ...*botdialoggeneratorv1.MenuOption) *botdialoggeneratorv1.OutMessage {
	return &botdialoggeneratorv1.OutMessage{
		Kind: &botdialoggeneratorv1.OutMessage_Menu{Menu: &botdialoggeneratorv1.Menu{Prompt: prompt, Options: options}},
	}
}

// WebchatBinding says which workspace the web chat channel serves. The workspace
// always comes from here, never from the request: a client cannot reach another
// area's bot by sending a different workspace. Milestone 3 replaces this single
// binding with one per widget key.
type WebchatBinding struct {
	WorkspaceID string
}

// webchat wraps a web chat route: cross-origin headers, POST only, and a bounded JSON body.
func webchat(handle func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The widget is embedded on other sites and sends no cookies or credentials, so
		// any origin may call. The workspace comes from the binding, not from the caller.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		handle(w, r)
	}
}

func decodeWebchat(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return false
	}
	return true
}

// validUserID refuses a missing or meaningful user ID. A shared default would put
// every anonymous visitor in the same conversation.
func validUserID(w http.ResponseWriter, userID string) bool {
	if !userIDPattern.MatchString(userID) {
		http.Error(w, "user_id must be the random conversation ID created by the chat widget (8-128 letters, digits, - or _)", http.StatusBadRequest)
		return false
	}
	return true
}

// campaignContext keeps the allowed campaign keys, and only when the person opted in.
func campaignContext(payload WebchatInbound) map[string]string {
	if !payload.Consent.Campaigns {
		return nil
	}
	kept := map[string]string{}
	for _, key := range campaignContextKeys {
		if value := payload.Context[key]; value != "" && len(value) <= maxContextValue {
			kept[key] = value
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func NewGatewayHandler(engine EngineClient, binding WebchatBinding, opts ...GatewayOption) http.Handler {
	cfg := gatewayConfig{privacy: NewLocalPrivacyClient()}
	for _, opt := range opts {
		opt(&cfg)
	}
	base := httpserver.New("channel-gateway")
	mux := http.NewServeMux()

	mux.Handle("/livez", base)
	mux.Handle("/readyz", base)

	mux.HandleFunc("/channels/webchat/messages", webchat(func(w http.ResponseWriter, r *http.Request) {
		var payload WebchatInbound
		if !decodeWebchat(w, r, &payload) {
			return
		}
		if !validUserID(w, payload.UserID) {
			return
		}
		if len(payload.Text) > maxTextLen {
			http.Error(w, fmt.Sprintf("text must be at most %d characters", maxTextLen), http.StatusBadRequest)
			return
		}

		if payload.Tenant == "" {
			payload.Tenant = "demo"
		}
		if payload.Channel == "" {
			payload.Channel = "webchat"
		}
		if payload.MessageID == "" {
			payload.MessageID = fmt.Sprintf("msg-%d", time.Now().UnixMilli())
		}

		protoIn := &botdialoggeneratorv1.InboundMessage{
			Tenant:          payload.Tenant,
			WorkspaceId:     binding.WorkspaceID,
			Channel:         payload.Channel,
			UserId:          payload.UserID,
			MessageId:       payload.MessageID,
			TimestampUnixMs: time.Now().UnixMilli(),
			Context:         campaignContext(payload),
		}

		if payload.Choice != "" {
			protoIn.Body = &botdialoggeneratorv1.InboundMessage_Choice{
				Choice: &botdialoggeneratorv1.Choice{Value: payload.Choice},
			}
		} else {
			protoIn.Body = &botdialoggeneratorv1.InboundMessage_Text{
				Text: &botdialoggeneratorv1.Text{Value: payload.Text},
			}
		}

		batch, err := engine.ProcessMessage(protoIn)
		if err != nil {
			// The error can quote what the person typed, so it is not logged or returned.
			log.Printf("webchat: engine failed for workspace %s (%T)", binding.WorkspaceID, err)
			http.Error(w, "The assistant could not answer. Please try again.", http.StatusInternalServerError)
			return
		}

		outbound := WebchatOutbound{
			ReplyTo: batch.ReplyTo,
		}

		for _, m := range batch.Messages {
			if t := m.GetText(); t != nil {
				outbound.Messages = append(outbound.Messages, WebchatMessage{
					Kind: "text",
					Text: t.Value,
				})
			} else if menu := m.GetMenu(); menu != nil {
				var opts []WebchatOption
				for _, o := range menu.Options {
					opts = append(opts, WebchatOption{
						ID:    o.Id,
						Label: o.Label,
					})
				}
				outbound.Messages = append(outbound.Messages, WebchatMessage{
					Kind:    "menu",
					Prompt:  menu.Prompt,
					Options: opts,
				})
			}
		}

		writeJSON(w, http.StatusOK, outbound)
	}))

	// Proof that the person accepted the notice, kept by studio-api.
	mux.HandleFunc("/channels/webchat/consent", webchat(func(w http.ResponseWriter, r *http.Request) {
		var payload ConsentRequest
		if !decodeWebchat(w, r, &payload) || !validUserID(w, payload.UserID) {
			return
		}
		if len(payload.PolicyVersion) > 64 || len(payload.GuardianReference) > 256 {
			http.Error(w, "policy_version or guardian_reference is too long", http.StatusBadRequest)
			return
		}
		if err := cfg.privacy.RecordConsent(r.Context(), binding.WorkspaceID, payload); err != nil {
			privacyFailure(w, "record consent", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	// "Delete my data" from the widget menu.
	mux.HandleFunc("/channels/webchat/data-deletion", webchat(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			UserID string `json:"user_id"`
		}
		if !decodeWebchat(w, r, &payload) || !validUserID(w, payload.UserID) {
			return
		}
		receipt, err := cfg.privacy.RequestErasure(r.Context(), binding.WorkspaceID, payload.UserID)
		if err != nil {
			privacyFailure(w, "erase", err)
			return
		}
		writeJSON(w, http.StatusOK, receipt)
	}))

	return mux
}

func privacyFailure(w http.ResponseWriter, action string, err error) {
	log.Printf("webchat: could not %s: %v", action, err)
	status := http.StatusBadGateway
	if errors.Is(err, errPrivacyRejected) {
		status = http.StatusBadRequest
	}
	http.Error(w, "We could not complete the request. Please try again, or use the contact in the privacy policy.", status)
}
