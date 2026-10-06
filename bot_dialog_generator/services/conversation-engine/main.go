package main

import (
	"log"
	"net/http"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/httpserver"
)

func main() {
	log.Fatal(http.ListenAndServe(":8082", httpserver.New("conversation-engine")))
}
