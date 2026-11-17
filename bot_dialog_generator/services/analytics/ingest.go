package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"sync"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/httpserver"
)

// The analytics service counts things that happened; it never learns who did them or
// what they wrote. An event is accepted only if:
//
//   - the person opted into analytics (the studio banner or the chat prompt),
//   - its name is one of the events below,
//   - every property is on the allow-list and looks like a code, not like a sentence.
//
// There is no field for a message, a name, an email or a user ID, and unknown fields
// are refused rather than ignored, so a client cannot start sending them by accident.

// Event is one thing that happened in the studio or the chat widget.
type Event struct {
	Source      string            `json:"source"`
	Name        string            `json:"event"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	Consent     EventConsent      `json:"consent"`
	Props       map[string]string `json:"props,omitempty"`
}

// EventConsent repeats what the person opted into when the event was created.
type EventConsent struct {
	Analytics bool `json:"analytics"`
	Campaigns bool `json:"campaigns"`
}

// allowedEvents lists every event each source may send.
var allowedEvents = map[string][]string{
	"studio":  {"tab_viewed", "draft_saved", "bot_published", "test_chat_opened"},
	"webchat": {"chat_opened", "message_sent", "option_selected", "chat_closed"},
}

// allowedProps are the only properties kept. The campaign ones need their own opt-in.
var (
	allowedProps  = []string{"tab", "channel", "node_type"}
	campaignProps = []string{"utm_source", "utm_medium", "utm_campaign"}
)

// codeValue is what an identifier or a campaign tag looks like. Free text (spaces,
// punctuation, an @) does not match, so what a person typed cannot be sent as a property.
var codeValue = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

const maxEventBytes = 8 << 10

// Counter keeps totals per source and event. It holds no per-person or per-conversation
// record, so there is nothing to look up or delete for one person.
type Counter struct {
	mu     sync.Mutex
	totals map[string]int
}

func NewCounter() *Counter { return &Counter{totals: make(map[string]int)} }

func (c *Counter) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totals[key]++
}

// Totals returns a copy of the counts, keyed by "source/event" and, for properties,
// "source/event/prop=value".
func (c *Counter) Totals() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.totals))
	for k, v := range c.totals {
		out[k] = v
	}
	return out
}

func refuse(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "https://bot-dialog-generator.example.com/errors/analytics", "title": http.StatusText(status), "status": status, "detail": detail,
	})
}

// NewHandler serves the health checks and POST /events.
func NewHandler(counter *Counter) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", httpserver.New("analytics"))
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		// The chat widget is embedded on other sites and sends no cookies, so any origin may post.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			refuse(w, http.StatusMethodNotAllowed, "Send events with POST.")
			return
		}
		var event Event
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEventBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			refuse(w, http.StatusBadRequest, "The event has a field this service does not accept, or is not valid JSON.")
			return
		}
		if !event.Consent.Analytics {
			refuse(w, http.StatusForbidden, "Analytics events need the person's opt-in.")
			return
		}
		if !slices.Contains(allowedEvents[event.Source], event.Name) {
			refuse(w, http.StatusBadRequest, "Unknown source or event.")
			return
		}
		if event.WorkspaceID != "" && !codeValue.MatchString(event.WorkspaceID) {
			refuse(w, http.StatusBadRequest, "workspace_id is not an identifier.")
			return
		}
		kept := map[string]string{}
		for key, value := range event.Props {
			switch {
			case slices.Contains(allowedProps, key):
			case slices.Contains(campaignProps, key):
				if !event.Consent.Campaigns {
					continue // not opted into campaign measurement: dropped, not stored
				}
			default:
				refuse(w, http.StatusBadRequest, "Property "+key+" is not accepted.")
				return
			}
			if !codeValue.MatchString(value) {
				refuse(w, http.StatusBadRequest, "Property "+key+" must be a short code, not free text.")
				return
			}
			kept[key] = value
		}

		key := event.Source + "/" + event.Name
		counter.add(key)
		for prop, value := range kept {
			counter.add(key + "/" + prop + "=" + value)
		}
		w.WriteHeader(http.StatusAccepted)
	})
	return mux
}
