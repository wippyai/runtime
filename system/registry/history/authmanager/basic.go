package authmanager

import (
	"encoding/base64"
	"net/http"
)

// Basic sets basic credentials.
type Basic struct {
	username string
	password string
}

func NewBasic(username string, password string) *Basic {
	return &Basic{username: username, password: password}
}

func (b Basic) SetAuth(header http.Header) {
	header.Add("Authorization", basicAuth(b.username, b.password))
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}
