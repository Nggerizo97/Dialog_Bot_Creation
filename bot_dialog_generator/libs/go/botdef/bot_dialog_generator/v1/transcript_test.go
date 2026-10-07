package botdialoggeneratorv1_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
)

type GoldenTranscript struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	Input              any    `json:"input"`
	SelectedDefinition struct {
		AppID   string `json:"app_id"`
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	} `json:"selected_definition"`
	SessionBefore map[string]any `json:"session_before"`
	SessionDiff   map[string]any `json:"session_diff"`
	Outputs       struct {
		Tenant      string `json:"tenant"`
		WorkspaceID string `json:"workspace_id"`
		Channel     string `json:"channel"`
		UserID      string `json:"user_id"`
		ReplyTo     string `json:"reply_to"`
		Messages    []struct {
			Kind    string `json:"kind"`
			Value   string `json:"value,omitempty"`
			Prompt  string `json:"prompt,omitempty"`
			Options []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"options,omitempty"`
		} `json:"messages"`
	} `json:"outputs"`
	Effects []map[string]any `json:"effects"`
}

func findFixturesDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "tests", "fixtures", "transcripts")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join("..", "..", "..", "..", "..", "tests", "fixtures", "transcripts"), nil
}

func TestGoldenTranscriptsMatchSchemas(t *testing.T) {
	fixturesDir, err := findFixturesDir()
	if err != nil {
		t.Fatalf("could not resolve fixtures dir: %v", err)
	}

	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("failed to read fixtures directory %s: %v", fixturesDir, err)
	}

	if len(entries) == 0 {
		t.Fatalf("expected at least one fixture in %s, found 0", fixturesDir)
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(fixturesDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("failed to read fixture %s: %v", entry.Name(), err)
			continue
		}

		var fixture GoldenTranscript
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Errorf("failed to parse fixture %s: %v", entry.Name(), err)
			continue
		}

		if fixture.Name == "" {
			t.Errorf("fixture %s is missing 'name'", entry.Name())
		}
		if fixture.SelectedDefinition.Version == "" {
			t.Errorf("fixture %s is missing selected_definition.version", entry.Name())
		}
		if fixture.Outputs.WorkspaceID == "" {
			t.Errorf("fixture %s is missing outputs.workspace_id", entry.Name())
		}
		if len(fixture.Outputs.Messages) == 0 {
			t.Errorf("fixture %s is missing outputs.messages", entry.Name())
		}

		// Verify OutboundBatch proto mapping
		batch := &botdialoggeneratorv1.OutboundBatch{
			Tenant:      fixture.Outputs.Tenant,
			WorkspaceId: fixture.Outputs.WorkspaceID,
			Channel:     fixture.Outputs.Channel,
			UserId:      fixture.Outputs.UserID,
			ReplyTo:     fixture.Outputs.ReplyTo,
		}

		for _, m := range fixture.Outputs.Messages {
			outMsg := &botdialoggeneratorv1.OutMessage{}
			switch m.Kind {
			case "text":
				outMsg.Kind = &botdialoggeneratorv1.OutMessage_Text{
					Text: &botdialoggeneratorv1.Text{Value: m.Value},
				}
			case "menu":
				var opts []*botdialoggeneratorv1.MenuOption
				for _, opt := range m.Options {
					opts = append(opts, &botdialoggeneratorv1.MenuOption{
						Id:    opt.ID,
						Label: opt.Label,
					})
				}
				outMsg.Kind = &botdialoggeneratorv1.OutMessage_Menu{
					Menu: &botdialoggeneratorv1.Menu{
						Prompt:  m.Prompt,
						Options: opts,
					},
				}
			}
			batch.Messages = append(batch.Messages, outMsg)
		}

		if len(batch.Messages) != len(fixture.Outputs.Messages) {
			t.Errorf("fixture %s: expected %d messages, converted %d",
				entry.Name(), len(fixture.Outputs.Messages), len(batch.Messages))
		}
	}
}
