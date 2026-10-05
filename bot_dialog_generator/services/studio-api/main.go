package main

import (
	"log"
	"net/http"
)

func main() {
	store := NewMemoryStore()
	handler := NewStudioHandler(store)
	log.Println("bot_dialog_generator studio-api listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}