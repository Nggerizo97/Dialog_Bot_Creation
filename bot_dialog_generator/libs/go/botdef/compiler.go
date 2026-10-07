package botdef

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
	"google.golang.org/protobuf/proto"
)

// DraftNode represents an authored node in Studio.
type DraftNode struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"` // Trigger, Response, Menu, Service, Jump
	Title      string            `json:"title"`
	Detail     string            `json:"detail,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// DraftEdge represents a connection between two nodes.
type DraftEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

// DraftVersion represents the uncompiled bot draft.
type DraftVersion struct {
	Tenant      string      `json:"tenant"`
	WorkspaceID string      `json:"workspace_id"`
	AppID       string      `json:"app_id"`
	Version     string      `json:"version"`
	EntryNodeID string      `json:"entry_node_id"`
	Nodes       []DraftNode `json:"nodes"`
	Edges       []DraftEdge `json:"edges"`
}

// CompiledArtifact contains the serialized protobuf and cryptographic digest.
type CompiledArtifact struct {
	Definition *botdialoggeneratorv1.BotDefinition
	Bytes      []byte
	SHA256     string
}

// Compile validates a draft and produces a canonical BotDefinition protobuf.
func Compile(draft *DraftVersion) (*CompiledArtifact, error) {
	if draft.Tenant == "" {
		return nil, errors.New("draft missing tenant")
	}
	if draft.WorkspaceID == "" {
		return nil, errors.New("draft missing workspace_id")
	}
	if draft.AppID == "" {
		return nil, errors.New("draft missing app_id")
	}
	if draft.Version == "" {
		return nil, errors.New("draft missing version")
	}
	if len(draft.Nodes) == 0 {
		return nil, errors.New("draft must contain at least one node")
	}

	nodeMap := make(map[string]DraftNode, len(draft.Nodes))
	var entryFound bool

	for _, node := range draft.Nodes {
		if node.ID == "" {
			return nil, errors.New("node ID cannot be empty")
		}
		if _, exists := nodeMap[node.ID]; exists {
			return nil, fmt.Errorf("duplicate node ID: %s", node.ID)
		}
		nodeMap[node.ID] = node

		if node.ID == draft.EntryNodeID {
			entryFound = true
			if node.Type != "Trigger" {
				return nil, fmt.Errorf("entry node '%s' must be of type Trigger, got %s", node.ID, node.Type)
			}
		}
	}

	if !entryFound {
		return nil, fmt.Errorf("entry node '%s' not found in draft nodes", draft.EntryNodeID)
	}

	// Validate edges
	for _, edge := range draft.Edges {
		if _, ok := nodeMap[edge.From]; !ok {
			return nil, fmt.Errorf("edge references non-existent source node: %s", edge.From)
		}
		if _, ok := nodeMap[edge.To]; !ok {
			return nil, fmt.Errorf("edge references non-existent target node: %s", edge.To)
		}
	}

	// Construct canonical Protobuf definition
	def := &botdialoggeneratorv1.BotDefinition{
		Tenant:      draft.Tenant,
		WorkspaceId: draft.WorkspaceID,
		AppId:       draft.AppID,
		Version:     draft.Version,
		EntryNodeId: draft.EntryNodeID,
	}

	for _, n := range draft.Nodes {
		def.Nodes = append(def.Nodes, &botdialoggeneratorv1.Node{
			Id:         n.ID,
			Type:       n.Type,
			Properties: n.Properties,
		})
	}

	for _, e := range draft.Edges {
		def.Edges = append(def.Edges, &botdialoggeneratorv1.Edge{
			From:      e.From,
			To:        e.To,
			Condition: e.Condition,
		})
	}

	data, err := proto.Marshal(def)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bot definition protobuf: %w", err)
	}

	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	return &CompiledArtifact{
		Definition: def,
		Bytes:      data,
		SHA256:     hashHex,
	}, nil
}
