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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	testStorkUser  = "admin"
	testStorkPass  = "s3cret!A"
	testStorkToken = "0123456789abcdef0123456789abcdef"
	testStorkCR    = "stork"
)

// fakeStorkAPI mimics the two Stork 2.4 endpoints the operator calls: login
// sets a session cookie, and the token endpoint requires it.
func fakeStorkAPI(t *testing.T, token string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body["authenticationMethodId"] != "internal" || body["identifier"] != testStorkUser || body["secret"] != testStorkPass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
		_, _ = w.Write([]byte(`{"login":"admin"}`))
	})
	mux.HandleFunc("/api/machines-server-token", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err != nil || c.Value != "ok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchStorkServerToken(t *testing.T) {
	ctx := context.Background()

	t.Run("logs in and returns the server token", func(t *testing.T) {
		srv := fakeStorkAPI(t, testStorkToken)
		got, err := fetchStorkServerToken(ctx, srv.URL, testStorkUser, testStorkPass)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != testStorkToken {
			t.Errorf("token = %q, want %q", got, testStorkToken)
		}
	})

	t.Run("fails on wrong credentials", func(t *testing.T) {
		srv := fakeStorkAPI(t, testStorkToken)
		if _, err := fetchStorkServerToken(ctx, srv.URL, testStorkUser, "wrong"); err == nil {
			t.Fatal("expected an error for rejected login")
		}
	})

	t.Run("fails on an empty token", func(t *testing.T) {
		srv := fakeStorkAPI(t, "")
		if _, err := fetchStorkServerToken(ctx, srv.URL, testStorkUser, testStorkPass); err == nil {
			t.Fatal("expected an error for an empty token")
		}
	})
}

func storkTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{keav1alpha1.AddToScheme, corev1.AddToScheme, monitoringv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatalf("adding to scheme: %v", err)
		}
	}
	return scheme
}

func TestSyncServerToken(t *testing.T) {
	ctx := context.Background()
	scheme := storkTestScheme(t)
	server := &keav1alpha1.KeaStorkServer{ObjectMeta: metav1.ObjectMeta{Name: testStorkCR, Namespace: testNamespaceDefault}}
	labels := resources.CommonLabels(testStorkCR, resources.StorkServerComponent)
	adminName := resources.StorkAdminSecretName(testStorkCR)
	tokenName := resources.StorkServerTokenSecretName(testStorkCR)

	newClient := func() client.Client {
		admin := resources.BuildStorkServerAdminSecret(testNamespaceDefault, adminName, labels, testStorkPass)
		token := resources.BuildStorkServerTokenSecret(testNamespaceDefault, tokenName, labels, "")
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(admin, token).Build()
	}
	readToken := func(t *testing.T, c client.Client) string {
		t.Helper()
		s := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Name: tokenName, Namespace: testNamespaceDefault}, s); err != nil {
			t.Fatalf("reading token Secret: %v", err)
		}
		return string(s.Data[secretKeyToken])
	}

	t.Run("writes the server's token into the Secret", func(t *testing.T) {
		c := newClient()
		var gotURL, gotUser, gotPass string
		r := &KeaStorkServerReconciler{Client: c, Scheme: scheme,
			FetchServerToken: func(_ context.Context, url, user, pass string) (string, error) {
				gotURL, gotUser, gotPass = url, user, pass
				return testStorkToken, nil
			}}
		if err := r.syncServerToken(ctx, server, adminName, tokenName); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := readToken(t, c); got != testStorkToken {
			t.Errorf("Secret token = %q, want %q", got, testStorkToken)
		}
		wantURL := "http://stork-stork-server.default.svc.cluster.local:8080"
		if gotURL != wantURL || gotUser != testStorkUser || gotPass != testStorkPass {
			t.Errorf("fetcher called with (%q, %q, %q), want (%q, %q, %q)", gotURL, gotUser, gotPass, wantURL, testStorkUser, testStorkPass)
		}
	})

	t.Run("leaves the Secret alone when the server is unreachable", func(t *testing.T) {
		c := newClient()
		r := &KeaStorkServerReconciler{Client: c, Scheme: scheme,
			FetchServerToken: func(context.Context, string, string, string) (string, error) {
				return "", errors.New("connection refused")
			}}
		if err := r.syncServerToken(ctx, server, adminName, tokenName); err == nil {
			t.Fatal("expected an error")
		}
		if got := readToken(t, c); got != "" {
			t.Errorf("Secret token = %q, want it untouched", got)
		}
	})
}

func TestEnqueueOnStorkTokenSecret(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(storkTestScheme(t)).Build()
	var calls int
	mapFn := enqueueOnStorkTokenSecret(func(context.Context, client.Reader, string) []ctrl.Request {
		calls++
		return nil
	}, c)

	labels := resources.CommonLabels(testStorkCR, resources.StorkServerComponent)
	tokenSecret := resources.BuildStorkServerTokenSecret(testNamespaceDefault, resources.StorkServerTokenSecretName(testStorkCR), labels, "")
	adminSecret := resources.BuildStorkServerAdminSecret(testNamespaceDefault, resources.StorkAdminSecretName(testStorkCR), labels, "x")
	userSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "db-credentials", Namespace: testNamespaceDefault}}

	mapFn(ctx, tokenSecret)
	if calls != 1 {
		t.Fatalf("token Secret: listFn called %d times, want 1", calls)
	}
	mapFn(ctx, adminSecret)
	mapFn(ctx, userSecret)
	if calls != 1 {
		t.Errorf("other Secrets must not enqueue: listFn called %d times, want 1", calls)
	}
}

func TestDeleteStorkMonitoring(t *testing.T) {
	ctx := context.Background()
	scheme := storkTestScheme(t)
	const component = "dhcp4"

	owner := &keav1alpha1.KeaDhcp4Server{ObjectMeta: metav1.ObjectMeta{Name: "ha", Namespace: testNamespaceDefault, UID: "owner-uid"}}
	pmName := resources.PodMonitorName(owner.Name, component)
	svcName := resources.StorkMetricsServiceName(owner.Name, component)
	metricsSvc := func() *corev1.Service {
		return resources.BuildStorkMetricsService(resources.StorkMetricsServiceParams{
			Namespace: testNamespaceDefault, CRName: owner.Name, Component: component, AgentPort: 8080, PrometheusPort: 9547,
		})
	}
	exists := func(t *testing.T, c client.Client, name string, obj client.Object) bool {
		t.Helper()
		err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespaceDefault}, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatalf("getting %s: %v", name, err)
		}
		return err == nil
	}

	t.Run("deletes the objects the CR controls", func(t *testing.T) {
		pm := resources.BuildPodMonitor(testNamespaceDefault, owner.Name, component)
		svc := metricsSvc()
		for _, obj := range []client.Object{pm, svc} {
			if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
				t.Fatalf("setting owner: %v", err)
			}
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pm, svc).Build()

		if err := deleteStorkMonitoring(ctx, c, owner, component, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists(t, c, pmName, &monitoringv1.PodMonitor{}) {
			t.Error("PodMonitor still present")
		}
		if exists(t, c, svcName, &corev1.Service{}) {
			t.Error("Stork metrics Service still present")
		}
		// Nothing left: a second run is a no-op.
		if err := deleteStorkMonitoring(ctx, c, owner, component, false); err != nil {
			t.Errorf("second run: %v", err)
		}
	})

	t.Run("keeps a same-named object the CR does not control", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(metricsSvc()).Build()
		if err := deleteStorkMonitoring(ctx, c, owner, component, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists(t, c, svcName, &corev1.Service{}) {
			t.Error("unowned Service was deleted")
		}
	})
}
