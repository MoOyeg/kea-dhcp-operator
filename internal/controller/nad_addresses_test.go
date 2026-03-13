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
	"testing"
)

func TestComputeNADAddresses(t *testing.T) {
	tests := []struct {
		name     string
		subnet   string
		replicas int
		expected []string
	}{
		{
			name:     "empty subnet returns nil",
			subnet:   "",
			replicas: 2,
			expected: nil,
		},
		{
			name:     "zero replicas returns nil",
			subnet:   "192.168.50.0/24",
			replicas: 0,
			expected: nil,
		},
		{
			name:     "invalid CIDR returns nil",
			subnet:   "not-a-cidr",
			replicas: 2,
			expected: nil,
		},
		{
			name:     "single replica",
			subnet:   "192.168.50.0/24",
			replicas: 1,
			expected: []string{"192.168.50.2"},
		},
		{
			name:     "two replicas",
			subnet:   "192.168.50.0/24",
			replicas: 2,
			expected: []string{"192.168.50.2", "192.168.50.3"},
		},
		{
			name:     "three replicas",
			subnet:   "10.200.0.0/16",
			replicas: 3,
			expected: []string{"10.200.0.2", "10.200.0.3", "10.200.0.4"},
		},
		{
			name:     "non-zero base last octet",
			subnet:   "172.16.10.100/24",
			replicas: 2,
			expected: []string{"172.16.10.102", "172.16.10.103"},
		},
		{
			name:     "IPv6 subnet returns nil",
			subnet:   "2001:db8::/32",
			replicas: 2,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeNADAddresses(tc.subnet, tc.replicas)
			if tc.expected == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d addresses, got %d: %v", len(tc.expected), len(got), got)
			}
			for i, exp := range tc.expected {
				if got[i] != exp {
					t.Errorf("address[%d] = %q, want %q", i, got[i], exp)
				}
			}
		})
	}
}
