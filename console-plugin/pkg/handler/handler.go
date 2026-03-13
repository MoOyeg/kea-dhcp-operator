package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/openshift/ocp-kea-dhcp/console-plugin/pkg/discovery"
	"github.com/openshift/ocp-kea-dhcp/console-plugin/pkg/keaclient"
)

// Handler serves the REST API endpoints for the console plugin backend.
type Handler struct {
	discovery *discovery.ServiceDiscovery
	keaClient *keaclient.KeaClient
}

// LeaseResponse is the aggregated response returned for lease queries.
type LeaseResponse struct {
	Leases interface{} `json:"leases"`
	Count  int         `json:"count"`
	Agents []string    `json:"agents"`
}

// AgentResponse lists discovered Kea Control Agent endpoints.
type AgentResponse struct {
	Agents []discovery.AgentEndpoint `json:"agents"`
}

// ErrorResponse is returned when an error occurs.
type ErrorResponse struct {
	Error string `json:"error"`
}

// NewHandler creates a new Handler with the given service discovery and a new Kea client.
func NewHandler(disc *discovery.ServiceDiscovery) *Handler {
	return &Handler{
		discovery: disc,
		keaClient: keaclient.NewKeaClient(),
	}
}

// GetLeases4 handles GET /api/v1/leases4 and returns aggregated DHCPv4 leases
// from all discovered Kea Control Agents.
func (h *Handler) GetLeases4(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespaceFilter := r.URL.Query().Get("namespace")

	agents, err := h.discovery.DiscoverAgents(ctx)
	if err != nil {
		log.Printf("ERROR: failed to discover agents: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to discover agents")
		return
	}

	var allLeases []keaclient.Lease4
	var queriedAgents []string

	for _, agent := range agents {
		if namespaceFilter != "" && agent.Namespace != namespaceFilter {
			continue
		}

		leases, err := h.keaClient.GetLeases4(ctx, agent.URL)
		if err != nil {
			log.Printf("WARNING: failed to get leases4 from %s: %v", agent.URL, err)
			continue
		}

		allLeases = append(allLeases, leases...)
		queriedAgents = append(queriedAgents, agent.URL)
	}

	if allLeases == nil {
		allLeases = []keaclient.Lease4{}
	}

	writeJSON(w, http.StatusOK, LeaseResponse{
		Leases: allLeases,
		Count:  len(allLeases),
		Agents: queriedAgents,
	})
}

// GetLeases6 handles GET /api/v1/leases6 and returns aggregated DHCPv6 leases
// from all discovered Kea Control Agents.
func (h *Handler) GetLeases6(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespaceFilter := r.URL.Query().Get("namespace")

	agents, err := h.discovery.DiscoverAgents(ctx)
	if err != nil {
		log.Printf("ERROR: failed to discover agents: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to discover agents")
		return
	}

	var allLeases []keaclient.Lease6
	var queriedAgents []string

	for _, agent := range agents {
		if namespaceFilter != "" && agent.Namespace != namespaceFilter {
			continue
		}

		leases, err := h.keaClient.GetLeases6(ctx, agent.URL)
		if err != nil {
			log.Printf("WARNING: failed to get leases6 from %s: %v", agent.URL, err)
			continue
		}

		allLeases = append(allLeases, leases...)
		queriedAgents = append(queriedAgents, agent.URL)
	}

	if allLeases == nil {
		allLeases = []keaclient.Lease6{}
	}

	writeJSON(w, http.StatusOK, LeaseResponse{
		Leases: allLeases,
		Count:  len(allLeases),
		Agents: queriedAgents,
	})
}

// ListAgents handles GET /api/v1/agents and returns all discovered Kea Control Agent endpoints.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agents, err := h.discovery.DiscoverAgents(ctx)
	if err != nil {
		log.Printf("ERROR: failed to discover agents: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to discover agents")
		return
	}

	if agents == nil {
		agents = []discovery.AgentEndpoint{}
	}

	writeJSON(w, http.StatusOK, AgentResponse{
		Agents: agents,
	})
}

// Healthz handles GET /healthz and returns a simple health check response.
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON marshals data as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("ERROR: failed to encode JSON response: %v", err)
	}
}

// writeError writes an error response as JSON.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
