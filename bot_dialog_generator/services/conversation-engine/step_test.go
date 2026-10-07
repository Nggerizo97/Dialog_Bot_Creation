package main

import (
	"errors"
	"testing"

	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
)

func buildTestDefinition() *botdialoggeneratorv1.BotDefinition {
	return &botdialoggeneratorv1.BotDefinition{
		Tenant:      "demo",
		WorkspaceId: "ws-customer-service",
		AppId:       "assistant",
		Version:     "v1.0.0",
		EntryNodeId: "welcome",
		Nodes: []*botdialoggeneratorv1.Node{
			{
				Id:   "welcome",
				Type: "Trigger",
			},
			{
				Id:   "menu",
				Type: "Menu",
				Properties: map[string]string{
					"prompt": "How can we help today?",
				},
			},
			{
				Id:   "balance",
				Type: "Response",
				Properties: map[string]string{
					"message": "Hello {user}, your balance is $1000.",
				},
			},
			{
				Id:   "jump_node",
				Type: "Jump",
				Properties: map[string]string{
					"target_node_id": "balance",
				},
			},
		},
		Edges: []*botdialoggeneratorv1.Edge{
			{From: "welcome", To: "menu"},
			{From: "menu", To: "balance", Condition: "balance"},
			{From: "menu", To: "jump_node", Condition: "jump"},
		},
	}
}

func TestEngineStepFirstTurnReturnsMenu(t *testing.T) {
	def := buildTestDefinition()
	session := &Session{
		SessionID: "sess-001",
		UserID:    "usr-001",
		Variables: map[string]string{"user": "Alice"},
	}

	inbound := &botdialoggeneratorv1.InboundMessage{
		Tenant:      "demo",
		WorkspaceId: "ws-customer-service",
		Channel:     "webchat",
		UserId:      "usr-001",
		MessageId:   "msg-001",
		Body: &botdialoggeneratorv1.InboundMessage_Text{
			Text: &botdialoggeneratorv1.Text{Value: "Hi"},
		},
	}

	res, err := Step(def, session, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Session.CurrentNodeID != "menu" {
		t.Errorf("expected session at node 'menu', got '%s'", res.Session.CurrentNodeID)
	}
	if len(res.Batch.Messages) != 1 {
		t.Fatalf("expected 1 outbound message, got %d", len(res.Batch.Messages))
	}
	menu := res.Batch.Messages[0].GetMenu()
	if menu == nil {
		t.Fatal("expected menu message")
	}
	if menu.Prompt != "How can we help today?" {
		t.Errorf("unexpected prompt: %s", menu.Prompt)
	}
	if len(menu.Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(menu.Options))
	}
}

func TestEngineStepSecondTurnReturnsResponseWithVariables(t *testing.T) {
	def := buildTestDefinition()
	session := &Session{
		SessionID:     "sess-001",
		UserID:        "usr-001",
		CurrentNodeID: "menu",
		Variables:     map[string]string{"user": "Alice"},
		Turns:         1,
	}

	inbound := &botdialoggeneratorv1.InboundMessage{
		Tenant:      "demo",
		WorkspaceId: "ws-customer-service",
		Channel:     "webchat",
		UserId:      "usr-001",
		MessageId:   "msg-002",
		Body: &botdialoggeneratorv1.InboundMessage_Choice{
			Choice: &botdialoggeneratorv1.Choice{Value: "balance"},
		},
	}

	res, err := Step(def, session, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Session.CurrentNodeID != "balance" {
		t.Errorf("expected session at node 'balance', got '%s'", res.Session.CurrentNodeID)
	}
	if len(res.Batch.Messages) != 1 {
		t.Fatalf("expected 1 outbound message, got %d", len(res.Batch.Messages))
	}
	text := res.Batch.Messages[0].GetText()
	if text == nil {
		t.Fatal("expected text message")
	}
	expected := "Hello Alice, your balance is $1000."
	if text.Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, text.Value)
	}
}

func TestEngineStepJumpNode(t *testing.T) {
	def := buildTestDefinition()
	session := &Session{
		SessionID:     "sess-001",
		UserID:        "usr-001",
		CurrentNodeID: "menu",
		Variables:     map[string]string{"user": "Bob"},
		Turns:         1,
	}

	inbound := &botdialoggeneratorv1.InboundMessage{
		Tenant:      "demo",
		WorkspaceId: "ws-customer-service",
		Channel:     "webchat",
		UserId:      "usr-001",
		MessageId:   "msg-003",
		Body: &botdialoggeneratorv1.InboundMessage_Choice{
			Choice: &botdialoggeneratorv1.Choice{Value: "jump"},
		},
	}

	res, err := Step(def, session, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Session.CurrentNodeID != "balance" {
		t.Errorf("expected jump to forward to 'balance', got '%s'", res.Session.CurrentNodeID)
	}
	if len(res.Batch.Messages) != 1 {
		t.Fatalf("expected 1 outbound message, got %d", len(res.Batch.Messages))
	}
}

func TestEngineStepRefusesAnotherWorkspace(t *testing.T) {
	def := buildTestDefinition()
	hello := func(workspaceID string) *botdialoggeneratorv1.InboundMessage {
		return &botdialoggeneratorv1.InboundMessage{
			Tenant:      "demo",
			WorkspaceId: workspaceID,
			Channel:     "webchat",
			UserId:      "usr-1",
			MessageId:   "msg-1",
			Body:        &botdialoggeneratorv1.InboundMessage_Text{Text: &botdialoggeneratorv1.Text{Value: "Hello"}},
		}
	}
	cases := map[string]struct {
		session *Session
		in      *botdialoggeneratorv1.InboundMessage
	}{
		"message from another workspace": {&Session{SessionID: "s1"}, hello("ws-hr")},
		"message without a workspace":    {&Session{SessionID: "s1"}, hello("")},
		"session from another workspace": {&Session{SessionID: "s1", WorkspaceID: "ws-hr"}, hello("ws-customer-service")},
	}
	for name, c := range cases {
		if _, err := Step(def, c.session, c.in); !errors.Is(err, ErrWorkspaceMismatch) {
			t.Errorf("%s: err = %v, want ErrWorkspaceMismatch", name, err)
		}
	}

	res, err := Step(def, &Session{SessionID: "s1"}, hello("ws-customer-service"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Session.WorkspaceID != "ws-customer-service" || res.Batch.GetWorkspaceId() != "ws-customer-service" {
		t.Errorf("session and reply must carry the workspace: session=%q batch=%q", res.Session.WorkspaceID, res.Batch.GetWorkspaceId())
	}
}
