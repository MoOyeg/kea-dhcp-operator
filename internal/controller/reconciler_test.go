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

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestSetCondition(t *testing.T) {
	t.Run("appends new condition", func(t *testing.T) {
		var conditions []keav1alpha1.ConditionStatus
		setCondition(&conditions, keav1alpha1.ConditionTypeReady, "True", "Ready", "All good")

		if len(conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(conditions))
		}
		if conditions[0].Type != keav1alpha1.ConditionTypeReady {
			t.Errorf("expected type %q, got %q", keav1alpha1.ConditionTypeReady, conditions[0].Type)
		}
		if conditions[0].Status != "True" {
			t.Errorf("expected status True, got %q", conditions[0].Status)
		}
		if conditions[0].LastTransitionTime == "" {
			t.Error("expected LastTransitionTime to be set")
		}
	})

	t.Run("updates existing condition same status", func(t *testing.T) {
		conditions := []keav1alpha1.ConditionStatus{
			{Type: keav1alpha1.ConditionTypeReady, Status: "True", Reason: "OldReason", LastTransitionTime: "2026-01-01T00:00:00Z"},
		}
		setCondition(&conditions, keav1alpha1.ConditionTypeReady, "True", "NewReason", "updated msg")

		if len(conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(conditions))
		}
		if conditions[0].Reason != "NewReason" {
			t.Errorf("expected reason NewReason, got %q", conditions[0].Reason)
		}
		// LastTransitionTime should NOT change when status stays the same.
		if conditions[0].LastTransitionTime != "2026-01-01T00:00:00Z" {
			t.Errorf("expected LastTransitionTime unchanged, got %q", conditions[0].LastTransitionTime)
		}
	})

	t.Run("updates existing condition different status", func(t *testing.T) {
		conditions := []keav1alpha1.ConditionStatus{
			{Type: keav1alpha1.ConditionTypeReady, Status: "False", Reason: "NotReady", LastTransitionTime: "2026-01-01T00:00:00Z"},
		}
		setCondition(&conditions, keav1alpha1.ConditionTypeReady, "True", "Ready", "now ready")

		if conditions[0].Status != "True" {
			t.Errorf("expected status True, got %q", conditions[0].Status)
		}
		// LastTransitionTime SHOULD change when status transitions.
		if conditions[0].LastTransitionTime == "2026-01-01T00:00:00Z" {
			t.Error("expected LastTransitionTime to be updated on status transition")
		}
	})

	t.Run("appends second condition type", func(t *testing.T) {
		conditions := []keav1alpha1.ConditionStatus{
			{Type: keav1alpha1.ConditionTypeReady, Status: "True", Reason: "Ready"},
		}
		setCondition(&conditions, keav1alpha1.ConditionTypeProgressing, "False", "Done", "done")

		if len(conditions) != 2 {
			t.Fatalf("expected 2 conditions, got %d", len(conditions))
		}
		if conditions[1].Type != keav1alpha1.ConditionTypeProgressing {
			t.Errorf("expected second condition type Progressing, got %q", conditions[1].Type)
		}
	})
}

func TestDbCredentialEnvVars(t *testing.T) {
	t.Run("nil db returns empty", func(t *testing.T) {
		envs, user, pass := dbCredentialEnvVars("PREFIX", nil)
		if len(envs) != 0 || user != "" || pass != "" {
			t.Errorf("expected empty results for nil db, got envs=%d user=%q pass=%q", len(envs), user, pass)
		}
	})

	t.Run("db without credentialsSecretRef returns empty", func(t *testing.T) {
		db := &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL}
		envs, user, pass := dbCredentialEnvVars("PREFIX", db)
		if len(envs) != 0 || user != "" || pass != "" {
			t.Errorf("expected empty results, got envs=%d user=%q pass=%q", len(envs), user, pass)
		}
	})

	t.Run("db with credentialsSecretRef generates env vars", func(t *testing.T) {
		db := &keav1alpha1.DatabaseConfig{
			Type: keav1alpha1.DatabaseTypeMySQL,
			CredentialsSecretRef: &corev1.LocalObjectReference{Name: "my-secret"},
		}
		envs, user, pass := dbCredentialEnvVars("KEA_DB", db)
		if len(envs) != 2 {
			t.Fatalf("expected 2 env vars, got %d", len(envs))
		}
		if envs[0].Name != "KEA_DB_USER" {
			t.Errorf("expected env name KEA_DB_USER, got %q", envs[0].Name)
		}
		if envs[0].ValueFrom.SecretKeyRef.Name != "my-secret" {
			t.Errorf("expected secret name my-secret, got %q", envs[0].ValueFrom.SecretKeyRef.Name)
		}
		if envs[0].ValueFrom.SecretKeyRef.Key != "username" {
			t.Errorf("expected key 'username', got %q", envs[0].ValueFrom.SecretKeyRef.Key)
		}
		if envs[1].Name != "KEA_DB_PASSWORD" {
			t.Errorf("expected env name KEA_DB_PASSWORD, got %q", envs[1].Name)
		}
		if user != "$KEA_DB_USER" {
			t.Errorf("expected user placeholder $KEA_DB_USER, got %q", user)
		}
		if pass != "$KEA_DB_PASSWORD" {
			t.Errorf("expected pass placeholder $KEA_DB_PASSWORD, got %q", pass)
		}
	})
}

func TestFillPeerURLs(t *testing.T) {
	t.Run("nil HA is a no-op", func(t *testing.T) {
		fillPeerURLs(nil, "ns", "myserver", "dhcp4", 8000)
	})

	t.Run("fills empty URLs using naming convention", func(t *testing.T) {
		ha := &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Mode:           "load-balancing",
			Peers: []keav1alpha1.HAPeer{
				{Name: "server1", Role: "primary"},
				{Name: "server2", Role: "secondary"},
			},
		}
		fillPeerURLs(ha, "dhcptest1", "dhcp4-ha", "dhcp4", 8000)

		expected0 := "http://dhcp4-ha-dhcp4-0.dhcp4-ha-dhcp4-hl.dhcptest1.svc.cluster.local:8000/"
		expected1 := "http://dhcp4-ha-dhcp4-1.dhcp4-ha-dhcp4-hl.dhcptest1.svc.cluster.local:8000/"

		if ha.Peers[0].URL != expected0 {
			t.Errorf("peer 0 URL = %q, want %q", ha.Peers[0].URL, expected0)
		}
		if ha.Peers[1].URL != expected1 {
			t.Errorf("peer 1 URL = %q, want %q", ha.Peers[1].URL, expected1)
		}
	})

	t.Run("does not overwrite explicit URLs", func(t *testing.T) {
		ha := &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Mode:           "load-balancing",
			Peers: []keav1alpha1.HAPeer{
				{Name: "server1", URL: "http://custom:9000/", Role: "primary"},
				{Name: "server2", Role: "secondary"},
			},
		}
		fillPeerURLs(ha, "ns", "myserver", "dhcp4", 8000)

		if ha.Peers[0].URL != "http://custom:9000/" {
			t.Errorf("peer 0 URL was overwritten: %q", ha.Peers[0].URL)
		}
		expected1 := "http://myserver-dhcp4-1.myserver-dhcp4-hl.ns.svc.cluster.local:8000/"
		if ha.Peers[1].URL != expected1 {
			t.Errorf("peer 1 URL = %q, want %q", ha.Peers[1].URL, expected1)
		}
	})

	t.Run("uses custom port", func(t *testing.T) {
		ha := &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Peers: []keav1alpha1.HAPeer{
				{Name: "server1", Role: "primary"},
			},
		}
		fillPeerURLs(ha, "ns", "myserver", "dhcp4", 9090)

		expected := "http://myserver-dhcp4-0.myserver-dhcp4-hl.ns.svc.cluster.local:9090/"
		if ha.Peers[0].URL != expected {
			t.Errorf("peer 0 URL = %q, want %q", ha.Peers[0].URL, expected)
		}
	})
}
