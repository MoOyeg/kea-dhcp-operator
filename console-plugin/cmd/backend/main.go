package main

import (
	"log"
	"net/http"
	"os"

	"github.com/openshift/ocp-kea-dhcp/console-plugin/pkg/discovery"
	"github.com/openshift/ocp-kea-dhcp/console-plugin/pkg/handler"
)

func main() {
	port := os.Getenv("BACKEND_PORT")
	if port == "" {
		port = "9444"
	}

	disc, err := discovery.NewServiceDiscovery()
	if err != nil {
		log.Fatalf("Failed to initialize service discovery: %v", err)
	}

	h := handler.NewHandler(disc)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/leases4", h.GetLeases4)
	mux.HandleFunc("/api/v1/leases6", h.GetLeases6)
	mux.HandleFunc("/api/v1/agents", h.ListAgents)
	mux.HandleFunc("/healthz", h.Healthz)

	log.Printf("Backend proxy starting on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
