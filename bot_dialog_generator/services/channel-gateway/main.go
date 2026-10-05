package main

import (
	"log"
	"net/http"
)

func main() {
	engineClient := &LocalEngineClient{}
	handler := NewGatewayHandler(engineClient)
	log.Println("bot_dialog_generator channel-gateway listening on :8081")
	log.Fatal(http.ListenAndServe(":8081", handler))
}