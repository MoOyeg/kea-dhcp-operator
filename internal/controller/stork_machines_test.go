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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const (
	testAgentNS  = "kea-system"
	testAgentSTS = "stork-ha-dhcp4"
	// testNoStateErr is the error Stork reports for a machine whose state pull failed.
	testNoStateErr = "Cannot get state of machine"
)

func agentFQDN(pod string) string {
	return pod + "." + testAgentSTS + "-hl." + testAgentNS + ".svc.cluster.local"
}

func TestOperatorAgentPod(t *testing.T) {
	tests := []struct {
		address string
		sts     string
		ordinal int
		ok      bool
	}{
		{agentFQDN(testAgentSTS + "-0"), testAgentSTS, 0, true},
		{agentFQDN(testAgentSTS + "-12"), testAgentSTS, 12, true},
		{"10.134.2.18", "", 0, false},
		{"stork-ha-dhcp4-0.stork-ha-dhcp4-hl.other-ns.svc.cluster.local", "", 0, false},
		{"stork-ha-dhcp4-0.some-other-svc.kea-system.svc.cluster.local", "", 0, false},
		{"stork-ha-dhcp4-x.stork-ha-dhcp4-hl.kea-system.svc.cluster.local", "", 0, false},
		{"kea.example.com", "", 0, false},
	}
	for _, tc := range tests {
		sts, ord, ok := operatorAgentPod(tc.address, testAgentNS)
		if ok != tc.ok || sts != tc.sts || ord != tc.ordinal {
			t.Errorf("operatorAgentPod(%q) = (%q, %d, %v), want (%q, %d, %v)", tc.address, sts, ord, ok, tc.sts, tc.ordinal, tc.ok)
		}
	}
}

// fakeMachineAPI records the refresh and delete calls made against it.
type fakeMachineAPI struct {
	machines  []storkMachine
	refreshed []int64
	deleted   []int64
}

func (f *fakeMachineAPI) ListMachines(context.Context) ([]storkMachine, error) {
	return f.machines, nil
}
func (f *fakeMachineAPI) RefreshMachineState(_ context.Context, id int64) error {
	f.refreshed = append(f.refreshed, id)
	return nil
}
func (f *fakeMachineAPI) DeleteMachine(_ context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func agentPod(name, ip string, running bool) *corev1.Pod {
	state := corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}
	if running {
		state = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testAgentNS,
			Labels: map[string]string{resources.LabelStorkServer: testStorkCR}},
		Spec: corev1.PodSpec{Hostname: name, Subdomain: testAgentSTS + "-hl"},
		Status: corev1.PodStatus{PodIP: ip, ContainerStatuses: []corev1.ContainerStatus{
			{Name: resources.StorkAgentContainerName, State: state},
		}},
	}
}

func machineTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := storkTestScheme(t)
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding apps to scheme: %v", err)
	}
	return scheme
}

func TestReconcileAgentMachines(t *testing.T) {
	ctx := context.Background()
	server := &keav1alpha1.KeaStorkServer{ObjectMeta: metav1.ObjectMeta{Name: testStorkCR, Namespace: testAgentNS}}
	replicas := int32(3)
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: testAgentSTS, Namespace: testAgentNS},
		Spec: appsv1.StatefulSetSpec{Replicas: &replicas}}
	const visited = "2026-11-03T19:30:17.000Z"

	c := fake.NewClientBuilder().WithScheme(machineTestScheme(t)).WithObjects(
		sts,
		agentPod(testAgentSTS+"-0", "10.0.0.10", true),  // machine 1: no state yet -> refresh
		agentPod(testAgentSTS+"-1", "10.0.0.11", true),  // machine 2: healthy -> leave alone
		agentPod(testAgentSTS+"-3", "10.0.0.13", false), // not running, no machine -> not pending
		// Labelled for another server: must not count.
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: testAgentNS,
			Labels: map[string]string{resources.LabelStorkServer: "someone-else"}}},
	).Build()

	api := &fakeMachineAPI{machines: []storkMachine{
		{ID: 1, Address: agentFQDN(testAgentSTS + "-0"), Authorized: true, Error: testNoStateErr},
		{ID: 2, Address: agentFQDN(testAgentSTS + "-1"), Authorized: true, LastVisitedAt: visited},
		{ID: 3, Address: agentFQDN(testAgentSTS + "-2"), Authorized: true, LastVisitedAt: visited}, // pod missing, ordinal 2 < 3 replicas: being recreated -> keep
		{ID: 4, Address: agentFQDN(testAgentSTS + "-7"), Authorized: true, LastVisitedAt: visited}, // ordinal 7 >= 3: scaled away -> delete
		{ID: 5, Address: "stork-x-0.stork-x-hl.kea-system.svc.cluster.local", Authorized: true},    // StatefulSet gone -> delete
		{ID: 6, Address: "10.0.0.99", Authorized: true, Error: testNoStateErr},                     // IP, not ours to judge -> keep
		{ID: 7, Address: "kea-dhcp.example.com", Authorized: true},                                 // foreign agent -> keep
		{ID: 8, Address: "10.0.0.11", Authorized: true, Error: "stale"},                            // pod IP of a live pod -> live, refresh
	}}

	pending, err := reconcileAgentMachines(ctx, c, server, api)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sort.Slice(api.refreshed, func(i, j int) bool { return api.refreshed[i] < api.refreshed[j] })
	sort.Slice(api.deleted, func(i, j int) bool { return api.deleted[i] < api.deleted[j] })

	if want := []int64{1, 8}; !equalIDs(api.refreshed, want) {
		t.Errorf("refreshed = %v, want %v", api.refreshed, want)
	}
	if want := []int64{4, 5}; !equalIDs(api.deleted, want) {
		t.Errorf("deleted = %v, want %v", api.deleted, want)
	}
	if !pending {
		t.Error("pending = false, want true while machine 1 has no state")
	}

	t.Run("not pending once every running agent has state", func(t *testing.T) {
		api := &fakeMachineAPI{machines: []storkMachine{
			{ID: 1, Address: agentFQDN(testAgentSTS + "-0"), Authorized: true, LastVisitedAt: visited},
			{ID: 2, Address: agentFQDN(testAgentSTS + "-1"), Authorized: true, LastVisitedAt: visited},
		}}
		pending, err := reconcileAgentMachines(ctx, c, server, api)
		if err != nil || pending || len(api.refreshed) != 0 || len(api.deleted) != 0 {
			t.Errorf("pending=%v err=%v refreshed=%v deleted=%v, want a no-op", pending, err, api.refreshed, api.deleted)
		}
	})

	t.Run("pending while a running agent has not registered", func(t *testing.T) {
		api := &fakeMachineAPI{machines: []storkMachine{
			{ID: 2, Address: agentFQDN(testAgentSTS + "-1"), Authorized: true, LastVisitedAt: visited},
		}}
		if pending, err := reconcileAgentMachines(ctx, c, server, api); err != nil || !pending {
			t.Errorf("pending=%v err=%v, want pending for unregistered %s-0", pending, err, testAgentSTS)
		}
	})

	t.Run("does not refresh an unauthorized machine", func(t *testing.T) {
		api := &fakeMachineAPI{machines: []storkMachine{
			{ID: 1, Address: agentFQDN(testAgentSTS + "-0"), Authorized: false},
		}}
		if _, err := reconcileAgentMachines(ctx, c, server, api); err != nil || len(api.refreshed) != 0 {
			t.Errorf("err=%v refreshed=%v, want no refresh", err, api.refreshed)
		}
	})
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestStorkSession checks the REST calls against a fake Stork server that
// requires the session cookie, as Stork 2.4 does.
func TestStorkSession(t *testing.T) {
	var refreshed, deleted []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
		_, _ = w.Write([]byte(`{"login":"admin"}`))
	})
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie("session"); err != nil || c.Value != "ok" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/api/machines", authed(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"id": 4, "address": "a.b.kea-system.svc.cluster.local", "agentPort": 8080, "authorized": true, "lastVisitedAt": nil, "error": testNoStateErr},
		}, "total": 1})
	}))
	mux.HandleFunc("/api/machines/4/state", authed(func(w http.ResponseWriter, r *http.Request) {
		refreshed = append(refreshed, r.Method)
		_, _ = w.Write([]byte(`{}`))
	}))
	mux.HandleFunc("/api/machines/4", authed(func(w http.ResponseWriter, r *http.Request) {
		deleted = append(deleted, r.Method)
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	api, err := connectStork(ctx, srv.URL, testStorkUser, testStorkPass)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	ms, err := api.ListMachines(ctx)
	if err != nil || len(ms) != 1 {
		t.Fatalf("ListMachines = %v, %v", ms, err)
	}
	if m := ms[0]; m.ID != 4 || m.AgentPort != 8080 || !m.Authorized || m.hasState() {
		t.Errorf("decoded machine = %+v, want id 4, port 8080, authorized, no state", m)
	}
	if err := api.RefreshMachineState(ctx, 4); err != nil || len(refreshed) != 1 || refreshed[0] != http.MethodGet {
		t.Errorf("refresh: err=%v calls=%v, want one GET", err, refreshed)
	}
	if err := api.DeleteMachine(ctx, 4); err != nil || len(deleted) != 1 || deleted[0] != http.MethodDelete {
		t.Errorf("delete: err=%v calls=%v, want one DELETE", err, deleted)
	}
}

func TestStorkServersForAgentPod(t *testing.T) {
	pod := agentPod(testAgentSTS+"-0", "10.0.0.10", true)
	reqs := storkServersForAgentPod(context.Background(), pod)
	if len(reqs) != 1 || reqs[0].Name != testStorkCR || reqs[0].Namespace != testAgentNS {
		t.Errorf("requests = %v, want %s/%s", reqs, testAgentNS, testStorkCR)
	}
	unlabelled := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: testAgentNS}}
	if reqs := storkServersForAgentPod(context.Background(), unlabelled); len(reqs) != 0 {
		t.Errorf("unlabelled pod enqueued %v", reqs)
	}
}
