package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

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

func (s *CloudHistoryServer) read() {
	data, err := os.ReadFile("history.json")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}

	if len(data) == 0 {
		return
	}

	if err := json.Unmarshal(data, &s.history); err != nil {
		log.Fatal(err)
	}

	for id, history := range s.history {
		fmt.Println(id, "history:", len(history))
	}
}

func (s *CloudHistoryServer) flush() {
	log.Println("Flushing history")
	data, err := json.Marshal(s.history)
	if err != nil {
		log.Fatal(err)
	}
	if len(data) == 0 {
		fmt.Println("Empty history")
		return
	}

	fmt.Println("Writing history", len(data))

	if err := os.WriteFile("history.json", data, 0600); err != nil {
		log.Fatal(err)
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

	fmt.Printf("Create History Version: %d operations - %s \n", len(version.Operations), id)

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

	fmt.Println("Get History Version:", id)

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
	s.read()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /runtime/{id}/history", s.createHistoryVersionHandler)
	mux.HandleFunc("GET /runtime/{id}/history", s.getHistoryHandler)

	log.Printf("Starting server on %s", addr)
	ctx := context.Background()
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		if err := http.ListenAndServe(addr, loghandler{inner: mux}); err != nil { //nolint:gosec // it's ok
			fmt.Println(err)
		}
	}()
	<-ctx.Done()

	s.flush()
	return nil
}

func main() {
	// Start server in a goroutine
	server := NewCloudHistoryServer()
	if err := server.Start("localhost:9333"); err != nil {
		log.Fatal("Server failed:", err)
	}
}

type loghandler struct {
	inner http.Handler
}

func (l loghandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	fmt.Println(request.Method, request.URL.String())
	l.inner.ServeHTTP(writer, request)
}
