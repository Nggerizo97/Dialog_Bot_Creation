package botdef_test

import (
	"strings"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef"
	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
	"google.golang.org/protobuf/proto"
)

func TestCompilerSuccess(t *testing.T) {
	draft := &botdef.DraftVersion{
		Tenant:      "demo",
		WorkspaceID: "ws-customer-service",
		AppID:       "retail-assistant",
		Version:     "v1.0.0",
		EntryNodeID: "welcome",
		Nodes: []botdef.DraftNode{
			{
				ID:    "welcome",
				Type:  "Trigger",
				Title: "Welcome Trigger",
			},
			{
				ID:    "menu",
				Type:  "Menu",
				Title: "Main Menu",
				Properties: map[string]string{
					"prompt": "What can we help with?",
				},
			},
			{
				ID:    "response",
				Type:  "Response",
				Title: "Goodbye",
				Properties: map[string]string{
					"message": "Thank you for contacting us!",
				},
			},
		},
		Edges: []botdef.DraftEdge{
			{From: "welcome", To: "menu"},
			{From: "menu", To: "response", Condition: "choice == 'exit'"},
		},
	}

	artifact, err := botdef.Compile(draft)
	if err != nil {
		t.Fatalf("expected successful compilation, got: %v", err)
	}

	if artifact.SHA256 == "" {
		t.Error("expected non-empty SHA256 digest")
	}

	// Verify unmarshaling the emitted protobuf bytes
	var decoded botdialoggeneratorv1.BotDefinition
	if err := proto.Unmarshal(artifact.Bytes, &decoded); err != nil {
		t.Fatalf("failed to unmarshal compiled protobuf bytes: %v", err)
	}

	if decoded.AppId != "retail-assistant" {
		t.Errorf("expected AppId retail-assistant, got %s", decoded.AppId)
	}
	if decoded.WorkspaceId != "ws-customer-service" {
		t.Errorf("expected WorkspaceId ws-customer-service, got %q", decoded.WorkspaceId)
	}
	if len(decoded.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(decoded.Nodes))
	}
	if len(decoded.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(decoded.Edges))
	}
}

func TestCompilerMissingEntryTriggerFails(t *testing.T) {
	draft := &botdef.DraftVersion{
		Tenant:      "demo",
		WorkspaceID: "ws-customer-service",
		AppID:       "retail-assistant",
		Version:     "v1.0.0",
		EntryNodeID: "missing_entry",
		Nodes: []botdef.DraftNode{
			{ID: "node1", Type: "Response", Title: "Resp"},
		},
	}

	_, err := botdef.Compile(draft)
	if err == nil {
		t.Fatal("expected error for missing entry node, got nil")
	}
}

func TestCompilerEntryNodeMustBeTrigger(t *testing.T) {
	draft := &botdef.DraftVersion{
		Tenant:      "demo",
		WorkspaceID: "ws-customer-service",
		AppID:       "retail-assistant",
		Version:     "v1.0.0",
		EntryNodeID: "menu",
		Nodes: []botdef.DraftNode{
			{ID: "menu", Type: "Menu", Title: "Menu Node"},
		},
	}

	_, err := botdef.Compile(draft)
	if err == nil {
		t.Fatal("expected error when entry node is not Trigger, got nil")
	}
}

func TestCompilerBrokenEdgeTargetFails(t *testing.T) {
	draft := &botdef.DraftVersion{
		Tenant:      "demo",
		WorkspaceID: "ws-customer-service",
		AppID:       "retail-assistant",
		Version:     "v1.0.0",
		EntryNodeID: "welcome",
		Nodes: []botdef.DraftNode{
			{ID: "welcome", Type: "Trigger", Title: "Welcome Trigger"},
		},
		Edges: []botdef.DraftEdge{
			{From: "welcome", To: "non_existent_node"},
		},
	}

	_, err := botdef.Compile(draft)
	if err == nil {
		t.Fatal("expected error for non-existent edge target, got nil")
	}
}

func TestCompilerRequiresWorkspace(t *testing.T) {
	draft := &botdef.DraftVersion{
		Tenant:      "demo",
		AppID:       "retail-assistant",
		Version:     "v1.0.0",
		EntryNodeID: "welcome",
		Nodes:       []botdef.DraftNode{{ID: "welcome", Type: "Trigger", Title: "Welcome"}},
	}
	if _, err := botdef.Compile(draft); err == nil || !strings.Contains(err.Error(), "workspace_id") {
		t.Fatalf("expected a missing workspace_id error, got %v", err)
	}
}
