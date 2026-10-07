package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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
}

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
				Text: &botdialoggeneratorv1.Text{Value: "Your current checking balance is $1,250.50 USD."},
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
		batch.Messages = append(batch.Messages, textMessage("Connecting you with an available agent from the contact center now..."))
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

func NewGatewayHandler(engine EngineClient, binding WebchatBinding) http.Handler {
	base := httpserver.New("channel-gateway")
	mux := http.NewServeMux()

	mux.Handle("/livez", base)
	mux.Handle("/readyz", base)

	mux.HandleFunc("/channels/webchat/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Tenant-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload WebchatInbound
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
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
		if payload.UserID == "" {
			payload.UserID = "anon-user"
		}

		protoIn := &botdialoggeneratorv1.InboundMessage{
			Tenant:          payload.Tenant,
			WorkspaceId:     binding.WorkspaceID,
			Channel:         payload.Channel,
			UserId:          payload.UserID,
			MessageId:       payload.MessageID,
			TimestampUnixMs: time.Now().UnixMilli(),
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
			http.Error(w, fmt.Sprintf("engine failure: %v", err), http.StatusInternalServerError)
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

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(outbound)
	})

	return mux
}
