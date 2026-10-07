package main

import (
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef"
)

// demoSeed is the demo organization used locally and in tests: two areas whose
// members are listed in the README. Both stores load the same data, so one test
// suite covers both.
type demoSeed struct {
	Workspaces []Workspace
	Members    []seedMember
	Groups     []seedGroup
	Bots       []seedBot
}

type seedMember struct {
	WorkspaceID string
	Member
}

type seedGroup struct {
	WorkspaceID string
	GroupGrant
}

type seedBot struct {
	Bot      Bot
	Versions []Version
}

func newDemoSeed(now time.Time) demoSeed {
	menuNodes := []botdef.DraftNode{
		{ID: "welcome", Type: "Trigger", Title: "Welcome", Detail: "New conversation"},
		{ID: "menu", Type: "Menu", Title: "What can we help with?", Detail: "3 routes", Properties: map[string]string{"prompt": "What can we help you with today?"}},
		{ID: "balance", Type: "Service", Title: "Check balance", Detail: "Accounts API", Properties: map[string]string{"endpoint": "/mock/accounts/balance"}},
		{ID: "handoff", Type: "Response", Title: "Connect to advisor", Detail: "Text message", Properties: map[string]string{"message": "Please hold while we connect you to an advisor."}},
	}
	menuEdges := []botdef.DraftEdge{
		{From: "welcome", To: "menu"},
		{From: "menu", To: "balance", Condition: "balance"},
		{From: "menu", To: "handoff", Condition: "handoff"},
	}
	published := now.Add(-26 * time.Hour)
	return demoSeed{
		Workspaces: []Workspace{
			{ID: "ws-customer-service", Name: "Customer service", CreatedAt: now},
			{ID: "ws-hr", Name: "Human resources", CreatedAt: now},
		},
		Members: []seedMember{
			{"ws-customer-service", Member{Subject: "alice", Role: RoleOwner}},
			{"ws-customer-service", Member{Subject: "erin", Role: RoleEditor}},
			{"ws-customer-service", Member{Subject: "carol", Role: RoleAnalyst}},
			{"ws-hr", Member{Subject: "bob", Role: RoleOwner}},
		},
		Groups: []seedGroup{
			{"ws-hr", GroupGrant{GroupID: "hr-team", DisplayName: "hr-team", Role: RoleEditor}},
		},
		Bots: []seedBot{
			{
				Bot: Bot{
					ID: "retail-assistant", WorkspaceID: "ws-customer-service", Tenant: "demo",
					Name: "Retail assistant", Description: "Customer support bot for account services",
					DraftVersion: "v18", ActiveVersion: "v17", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now,
				},
				Versions: []Version{
					{
						ID: "v17", BotID: "retail-assistant", Version: "v17", Status: "published", EntryNodeID: "welcome",
						Nodes: menuNodes, Edges: menuEdges, PublishedAt: &published,
						ArtifactURI: artifactURI("ws-customer-service", "retail-assistant", "v17"),
						CreatedAt:   now.Add(-30 * time.Hour), UpdatedAt: published,
					},
					{
						ID: "v18", BotID: "retail-assistant", Version: "v18", Status: "draft", EntryNodeID: "welcome",
						Nodes: menuNodes, Edges: menuEdges, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
					},
				},
			},
			{
				Bot: Bot{
					ID: "hr-helpdesk", WorkspaceID: "ws-hr", Tenant: "demo",
					Name: "HR helpdesk", Description: "Answers questions about leave and payroll",
					DraftVersion: "v1", CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
				},
				Versions: []Version{*starterDraft("hr-helpdesk", "v1", now)},
			},
		},
	}
}
