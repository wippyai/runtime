package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	registryhistory "github.com/ponyruntime/pony/system/registry/history"
)

type CloudHistoryServer struct {
	mu      sync.RWMutex
	history map[string]registryhistory.CloudHistory
}

func NewCloudHistoryServer() *CloudHistoryServer {
	return &CloudHistoryServer{
		history: make(map[string]registryhistory.CloudHistory),
	}
}

func (s *CloudHistoryServer) createHistoryVersionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")

	var version registryhistory.CloudVersion
	if err := json.NewDecoder(r.Body).Decode(&version); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	// Add the new version to history
	s.history[id] = append(s.history[id], version)
	defer s.mu.Unlock()

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "created"}); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
	}
}

func (s *CloudHistoryServer) getHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get ID from query parameter
	id := r.PathValue("id")

	s.mu.RLock()
	history, exists := s.history[id]
	s.mu.RUnlock()

	if !exists {
		// Return empty history if ID doesn't exist
		history = make(registryhistory.CloudHistory, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(history); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
	}
}

func (s *CloudHistoryServer) Start(addr string) error {
	http.HandleFunc("POST runtime/{id}/history", s.createHistoryVersionHandler)
	http.HandleFunc("GET runtime/{id}/history", s.getHistoryHandler)

	log.Printf("Starting server on %s", addr)
	return http.ListenAndServe(addr, nil) //nolint:gosec // it's ok
}

func main() {
	// Start server in a goroutine
	server := NewCloudHistoryServer()
	if err := server.Start(":9333"); err != nil {
		log.Fatal("Server failed:", err)
	}
}
