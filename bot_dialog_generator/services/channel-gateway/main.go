package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	engineClient := &LocalEngineClient{}
	// WEBCHAT_WORKSPACE_ID is the workspace the web chat serves (default: the demo
	// Customer service workspace).
	binding := WebchatBinding{WorkspaceID: os.Getenv("WEBCHAT_WORKSPACE_ID")}
	if binding.WorkspaceID == "" {
		binding.WorkspaceID = "ws-customer-service"
	}

	// STUDIO_API_URL and INTERNAL_API_TOKEN send consent proof and "delete my data"
	// requests to studio-api. Without them the gateway keeps consent proof in memory,
	// which is only acceptable for local development.
	var opts []GatewayOption
	privacyTarget := "memory (local development only)"
	if studioURL := os.Getenv("STUDIO_API_URL"); studioURL != "" {
		client, err := NewStudioPrivacyClient(studioURL, os.Getenv("INTERNAL_API_TOKEN"))
		if err != nil {
			log.Fatal(err)
		}
		opts = append(opts, WithPrivacyClient(client))
		privacyTarget = studioURL
	}

	handler := NewGatewayHandler(engineClient, binding, opts...)
	log.Printf("bot_dialog_generator channel-gateway listening on :8081 (privacy requests: %s)", privacyTarget)
	log.Fatal(http.ListenAndServe(":8081", handler))
}
