package middleware

import (
	"log"
	"net/http"
)

func WebsocketRelay(h http.Handler) http.Handler {
	log.Printf("ASDASD")
	return h
}
