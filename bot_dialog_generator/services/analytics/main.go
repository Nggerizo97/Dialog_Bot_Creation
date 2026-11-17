package main

import (
	"log"
	"net/http"
)

func main() {
	log.Println("bot_dialog_generator analytics listening on :8084 (opt-in events only; totals are kept in memory)")
	log.Fatal(http.ListenAndServe(":8084", NewHandler(NewCounter())))
}
