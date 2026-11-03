/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// storkTokenFetcher returns the agent registration token held by a Stork server.
type storkTokenFetcher func(ctx context.Context, baseURL, username, password string) (string, error)

// fetchStorkServerToken logs into the Stork REST API and reads the server token
// that agents must present to be authorized automatically. Stork generates this
// token itself and keeps it in its database; it cannot be set from outside, so
// the operator has to read it back rather than choose one.
func fetchStorkServerToken(ctx context.Context, baseURL, username, password string) (string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", fmt.Errorf("creating cookie jar: %w", err)
	}
	httpClient := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	login, err := json.Marshal(map[string]string{
		"authenticationMethodId": "internal",
		"identifier":             username,
		"secret":                 password,
	})
	if err != nil {
		return "", fmt.Errorf("encoding login request: %w", err)
	}
	if _, err := storkRequest(ctx, httpClient, http.MethodPost, baseURL+"/api/sessions", login); err != nil {
		return "", fmt.Errorf("logging into Stork server: %w", err)
	}

	body, err := storkRequest(ctx, httpClient, http.MethodGet, baseURL+"/api/machines-server-token", nil)
	if err != nil {
		return "", fmt.Errorf("reading Stork server token: %w", err)
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decoding Stork server token: %w", err)
	}
	if resp.Token == "" {
		return "", fmt.Errorf("stork server returned an empty token")
	}
	return resp.Token, nil
}

// storkRequest sends one request to the Stork REST API and returns the body of
// a 2xx response.
func storkRequest(ctx context.Context, c *http.Client, method, url string, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s %s: HTTP %d", method, url, resp.StatusCode)
	}
	return body, nil
}
