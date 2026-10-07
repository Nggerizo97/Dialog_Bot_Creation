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
	handler := NewGatewayHandler(engineClient, binding)
	log.Println("bot_dialog_generator channel-gateway listening on :8081")
	log.Fatal(http.ListenAndServe(":8081", handler))
}
