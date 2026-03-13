package keaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// KeaCommand represents a command to send to the Kea Control Agent.
type KeaCommand struct {
	Command   string      `json:"command"`
	Service   []string    `json:"service"`
	Arguments interface{} `json:"arguments,omitempty"`
}

// KeaResponse represents a single response from the Kea Control Agent.
type KeaResponse struct {
	Result    int             `json:"result"`
	Text      string          `json:"text"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Lease4 represents an IPv4 DHCP lease.
type Lease4 struct {
	IPAddress string `json:"ip-address"`
	HWAddress string `json:"hw-address"`
	ClientID  string `json:"client-id,omitempty"`
	ValidLft  int64  `json:"valid-lft"`
	CLTT      int64  `json:"cltt"`
	SubnetID  int    `json:"subnet-id"`
	FqdnFwd   bool   `json:"fqdn-fwd"`
	FqdnRev   bool   `json:"fqdn-rev"`
	Hostname  string `json:"hostname"`
	State     int    `json:"state"`
}

// Lease6 represents an IPv6 DHCP lease.
type Lease6 struct {
	IPAddress    string `json:"ip-address"`
	DUID         string `json:"duid"`
	IAID         int    `json:"iaid"`
	SubnetID     int    `json:"subnet-id"`
	ValidLft     int64  `json:"valid-lft"`
	CLTT         int64  `json:"cltt"`
	PreferredLft int64  `json:"preferred-lft"`
	Hostname     string `json:"hostname"`
	State        int    `json:"state"`
	Type         string `json:"type"`
	PrefixLen    int    `json:"prefix-len,omitempty"`
	FqdnFwd      bool   `json:"fqdn-fwd"`
	FqdnRev      bool   `json:"fqdn-rev"`
}

// Lease4Result holds the parsed arguments from a lease4-get-page response.
type Lease4Result struct {
	Leases []Lease4 `json:"leases"`
	Count  int      `json:"count"`
}

// Lease6Result holds the parsed arguments from a lease6-get-page response.
type Lease6Result struct {
	Leases []Lease6 `json:"leases"`
	Count  int      `json:"count"`
}

// KeaClient communicates with Kea Control Agent REST API endpoints.
type KeaClient struct {
	httpClient *http.Client
}

// NewKeaClient creates a new KeaClient with a default 10-second timeout.
func NewKeaClient() *KeaClient {
	return &KeaClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendCommand sends a command to the Kea Control Agent at the given URL and
// returns the array of responses.
func (kc *KeaClient) SendCommand(ctx context.Context, agentURL string, cmd KeaCommand) ([]KeaResponse, error) {
	body, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := kc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to %s: %w", agentURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d from %s: %s", resp.StatusCode, agentURL, string(respBody))
	}

	var responses []KeaResponse
	if err := json.Unmarshal(respBody, &responses); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return responses, nil
}

// GetLeases4 retrieves all DHCPv4 leases from the given Kea Control Agent.
func (kc *KeaClient) GetLeases4(ctx context.Context, agentURL string) ([]Lease4, error) {
	cmd := KeaCommand{
		Command: "lease4-get-page",
		Service: []string{"dhcp4"},
		Arguments: map[string]interface{}{
			"from":  "start",
			"limit": 10000,
		},
	}

	responses, err := kc.SendCommand(ctx, agentURL, cmd)
	if err != nil {
		return nil, err
	}

	if len(responses) == 0 {
		return nil, fmt.Errorf("empty response from %s", agentURL)
	}

	first := responses[0]

	// Result 3 means empty (no leases found) -- not an error
	if first.Result == 3 {
		return []Lease4{}, nil
	}

	if first.Result != 0 {
		return nil, fmt.Errorf("kea error (result=%d) from %s: %s", first.Result, agentURL, first.Text)
	}

	var result Lease4Result
	if err := json.Unmarshal(first.Arguments, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal lease4 arguments: %w", err)
	}

	return result.Leases, nil
}

// GetLeases6 retrieves all DHCPv6 leases from the given Kea Control Agent.
func (kc *KeaClient) GetLeases6(ctx context.Context, agentURL string) ([]Lease6, error) {
	cmd := KeaCommand{
		Command: "lease6-get-page",
		Service: []string{"dhcp6"},
		Arguments: map[string]interface{}{
			"from":  "start",
			"limit": 10000,
		},
	}

	responses, err := kc.SendCommand(ctx, agentURL, cmd)
	if err != nil {
		return nil, err
	}

	if len(responses) == 0 {
		return nil, fmt.Errorf("empty response from %s", agentURL)
	}

	first := responses[0]

	// Result 3 means empty (no leases found) -- not an error
	if first.Result == 3 {
		return []Lease6{}, nil
	}

	if first.Result != 0 {
		return nil, fmt.Errorf("kea error (result=%d) from %s: %s", first.Result, agentURL, first.Text)
	}

	var result Lease6Result
	if err := json.Unmarshal(first.Arguments, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal lease6 arguments: %w", err)
	}

	return result.Leases, nil
}
