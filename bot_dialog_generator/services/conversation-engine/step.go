package main

import (
	"fmt"
	"strings"

	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
)

// Session represents the conversation session state pinned to a definition version.
type Session struct {
	SessionID         string            `json:"session_id"`
	UserID            string            `json:"user_id"`
	Tenant            string            `json:"tenant"`
	Channel           string            `json:"channel"`
	DefinitionVersion string            `json:"definition_version"`
	CurrentNodeID     string            `json:"current_node_id"`
	Variables         map[string]string `json:"variables"`
	Turns             int               `json:"turns"`
	Status            string            `json:"status"` // active, ended, transferred
}

// StepResult holds the deterministic output of one conversation turn.
type StepResult struct {
	Session *Session
	Batch   *botdialoggeneratorv1.OutboundBatch
	Effects []map[string]any
}

// Step executes one deterministic turn of conversation using the pure step function.
func Step(def *botdialoggeneratorv1.BotDefinition, session *Session, in *botdialoggeneratorv1.InboundMessage) (*StepResult, error) {
	if def == nil {
		return nil, fmt.Errorf("bot definition cannot be nil")
	}
	if session == nil {
		return nil, fmt.Errorf("session cannot be nil")
	}

	// Clone session state
	nextSession := &Session{
		SessionID:         session.SessionID,
		UserID:            session.UserID,
		Tenant:            session.Tenant,
		Channel:           session.Channel,
		DefinitionVersion: def.Version,
		CurrentNodeID:     session.CurrentNodeID,
		Variables:         make(map[string]string),
		Turns:             session.Turns + 1,
		Status:            "active",
	}
	for k, v := range session.Variables {
		nextSession.Variables[k] = v
	}

	// Index definition nodes and edges
	nodesByID := make(map[string]*botdialoggeneratorv1.Node, len(def.Nodes))
	for _, n := range def.Nodes {
		nodesByID[n.Id] = n
	}

	outgoingEdges := make(map[string][]*botdialoggeneratorv1.Edge)
	for _, e := range def.Edges {
		outgoingEdges[e.From] = append(outgoingEdges[e.From], e)
	}

	batch := &botdialoggeneratorv1.OutboundBatch{
		Tenant:  in.Tenant,
		Channel: in.Channel,
		UserId:  in.UserId,
		ReplyTo: in.MessageId,
	}
	var effects []map[string]any

	// Extract inbound text or choice value
	var inputValue string
	if text := in.GetText(); text != nil {
		inputValue = text.Value
		nextSession.Variables["last_input"] = inputValue
	} else if choice := in.GetChoice(); choice != nil {
		inputValue = choice.Value
		nextSession.Variables["last_input"] = inputValue
		nextSession.Variables["selected_choice"] = inputValue
	}

	// Determine start node for this step
	currentNodeID := nextSession.CurrentNodeID
	if currentNodeID == "" {
		currentNodeID = def.EntryNodeId
	} else if current, ok := nodesByID[currentNodeID]; ok && current.Type == "Menu" {
		// Advance from Menu using user's choice or input
		var nextNodeID string
		edges := outgoingEdges[currentNodeID]
		for _, edge := range edges {
			if edge.Condition == "" || strings.EqualFold(edge.Condition, inputValue) || edge.Condition == fmt.Sprintf("choice == '%s'", inputValue) {
				nextNodeID = edge.To
				break
			}
		}
		if nextNodeID == "" && len(edges) > 0 {
			// Fallback to first branch
			nextNodeID = edges[0].To
		}
		if nextNodeID != "" {
			currentNodeID = nextNodeID
		}
	}

	// Step forward until waiting for user input or terminal node reached
	maxSteps := 20
	for stepCount := 0; stepCount < maxSteps; stepCount++ {
		node, exists := nodesByID[currentNodeID]
		if !exists {
			return nil, fmt.Errorf("node '%s' not found in definition", currentNodeID)
		}
		nextSession.CurrentNodeID = currentNodeID

		switch node.Type {
		case "Trigger":
			// Advance to next node
			edges := outgoingEdges[currentNodeID]
			if len(edges) > 0 {
				currentNodeID = edges[0].To
				continue
			}
			return &StepResult{Session: nextSession, Batch: batch, Effects: effects}, nil

		case "Response":
			msg := node.Properties["message"]
			if msg == "" {
				msg = node.Properties["text"]
			}
			if msg == "" {
				msg = node.Id
			}

			// Variable substitution
			for k, v := range nextSession.Variables {
				msg = strings.ReplaceAll(msg, "{"+k+"}", v)
			}

			batch.Messages = append(batch.Messages, &botdialoggeneratorv1.OutMessage{
				Kind: &botdialoggeneratorv1.OutMessage_Text{
					Text: &botdialoggeneratorv1.Text{Value: msg},
				},
			})

			// Check for next transition
			edges := outgoingEdges[currentNodeID]
			if len(edges) > 0 {
				currentNodeID = edges[0].To
				continue
			}
			return &StepResult{Session: nextSession, Batch: batch, Effects: effects}, nil

		case "Menu":
			prompt := node.Properties["prompt"]
			if prompt == "" {
				prompt = "Please select an option:"
			}
			var options []*botdialoggeneratorv1.MenuOption
			for _, edge := range outgoingEdges[currentNodeID] {
				label := edge.Condition
				if label == "" {
					label = edge.To
				}
				options = append(options, &botdialoggeneratorv1.MenuOption{
					Id:    edge.To,
					Label: label,
				})
			}
			batch.Messages = append(batch.Messages, &botdialoggeneratorv1.OutMessage{
				Kind: &botdialoggeneratorv1.OutMessage_Menu{
					Menu: &botdialoggeneratorv1.Menu{
						Prompt:  prompt,
						Options: options,
					},
				},
			})
			// Wait for user choice at this node
			return &StepResult{Session: nextSession, Batch: batch, Effects: effects}, nil

		case "Jump":
			target := node.Properties["target_node_id"]
			if target == "" {
				edges := outgoingEdges[currentNodeID]
				if len(edges) > 0 {
					target = edges[0].To
				}
			}
			if target != "" {
				currentNodeID = target
				continue
			}
			return &StepResult{Session: nextSession, Batch: batch, Effects: effects}, nil

		case "Service":
			effects = append(effects, map[string]any{
				"type":     "service_call",
				"endpoint": node.Properties["endpoint"],
				"node_id":  node.Id,
			})
			edges := outgoingEdges[currentNodeID]
			if len(edges) > 0 {
				currentNodeID = edges[0].To
				continue
			}
			return &StepResult{Session: nextSession, Batch: batch, Effects: effects}, nil

		default:
			// Unknown node type - stop
			return &StepResult{Session: nextSession, Batch: batch, Effects: effects}, nil
		}
	}

	return &StepResult{Session: nextSession, Batch: batch, Effects: effects}, nil
}
