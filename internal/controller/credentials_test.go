/*
Copyright 2024.

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
	"errors"
	"sort"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const (
	testCredSecret   = "db-creds"
	testOtherSecret  = "unrelated"
	testCredPassword = "synthetic-pw-1"
	testOtherNS      = "other"
	testKeyUsername  = "username"
	testKeyPassword  = "password"
	testDBHost       = "db.example"
	testKeaName      = "kea"
	testIfaceEth0    = "eth0"
)

func credSecret(name, uid, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespaceDefault, UID: types.UID(uid)},
		Data:       map[string][]byte{testKeyUsername: []byte(testKeaName), testKeyPassword: []byte(password)},
	}
}

func secretEnv(envName, secret, key string, optional bool) corev1.EnvVar {
	sel := &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: key}
	if optional {
		sel.Optional = &optional
	}
	return corev1.EnvVar{Name: envName, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: sel}}
}

func TestCredentialsRevision(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).
		WithObjects(credSecret(testCredSecret, "uid-1", testCredPassword), credSecret(testOtherSecret, "uid-2", "x")).Build()
	envs := []corev1.EnvVar{
		secretEnv("KEA_LEASE_DB_USER", testCredSecret, testKeyUsername, false),
		secretEnv("KEA_LEASE_DB_PASSWORD", testCredSecret, testKeyPassword, false),
	}
	revision := func(envs []corev1.EnvVar) string {
		t.Helper()
		tok, err := credentialsRevision(ctx, c, testNamespaceDefault, envs)
		if err != nil {
			t.Fatalf("credentialsRevision: %v", err)
		}
		return tok
	}

	if tok := revision(nil); tok != "" {
		t.Errorf("no references: token %q, want empty", tok)
	}
	first := revision(envs)
	if first == "" || strings.Contains(first, testCredPassword) {
		t.Fatalf("token %q must be non-empty and carry no Secret data", first)
	}
	if again := revision([]corev1.EnvVar{envs[1], envs[0]}); again != first {
		t.Errorf("token depends on env var order: %q vs %q", again, first)
	}

	other := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: testOtherSecret, Namespace: testNamespaceDefault}, other); err != nil {
		t.Fatal(err)
	}
	other.Data[testKeyPassword] = []byte("changed")
	if err := c.Update(ctx, other); err != nil {
		t.Fatal(err)
	}
	if tok := revision(envs); tok != first {
		t.Errorf("an unrelated Secret changed the token: %q -> %q", first, tok)
	}

	s := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: testCredSecret, Namespace: testNamespaceDefault}, s); err != nil {
		t.Fatal(err)
	}
	s.Data[testKeyPassword] = []byte("rotated")
	if err := c.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	rotated := revision(envs)
	if rotated == first {
		t.Error("rotating the referenced Secret did not change the token")
	}

	// Recreating the Secret with the same data still changes the token: the
	// pods must restart to read the new object.
	if err := c.Delete(ctx, s); err != nil {
		t.Fatal(err)
	}
	if _, err := credentialsRevision(ctx, c, testNamespaceDefault, envs); !errors.Is(err, errCredentialsUnavailable) {
		t.Fatalf("missing Secret: err = %v, want errCredentialsUnavailable", err)
	}
	if err := c.Create(ctx, credSecret(testCredSecret, "uid-3", "rotated")); err != nil {
		t.Fatal(err)
	}
	if tok := revision(envs); tok == rotated || tok == first {
		t.Error("recreating the Secret did not change the token")
	}

	missingKey := []corev1.EnvVar{secretEnv("X", testCredSecret, "no-such-key", false)}
	if _, err := credentialsRevision(ctx, c, testNamespaceDefault, missingKey); !errors.Is(err, errCredentialsUnavailable) ||
		!strings.Contains(err.Error(), `no key "no-such-key"`) {
		t.Errorf("missing key: err = %v, want errCredentialsUnavailable naming the key", err)
	}
}

// An optional reference may be missing, but when the Secret exists its
// changes must still roll the pods: env vars are only read at startup.
func TestCredentialsRevisionTracksOptionalSecrets(t *testing.T) {
	ctx := context.Background()
	const optName = "optional-creds"
	c := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).
		WithObjects(credSecret(optName, "uid-a", "first")).Build()
	envs := []corev1.EnvVar{secretEnv("KEA_AUTH_PASSWORD_0", optName, testKeyPassword, true)}
	revision := func(step string) string {
		t.Helper()
		tok, err := credentialsRevision(ctx, c, testNamespaceDefault, envs)
		if err != nil {
			t.Fatalf("%s: optional reference must never be an error: %v", step, err)
		}
		if tok == "" {
			t.Fatalf("%s: optional reference produced no token", step)
		}
		return tok
	}

	present := revision("present")
	s := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: optName, Namespace: testNamespaceDefault}, s); err != nil {
		t.Fatal(err)
	}
	s.Data[testKeyPassword] = []byte("second")
	if err := c.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	changed := revision("changed")
	if changed == present {
		t.Error("updating an optional Secret did not change the token")
	}
	if err := c.Delete(ctx, s); err != nil {
		t.Fatal(err)
	}
	gone := revision("deleted")
	if gone == changed {
		t.Error("deleting an optional Secret did not change the token")
	}
	if again := revision("still deleted"); again != gone {
		t.Errorf("token of an absent optional Secret is unstable: %q -> %q", gone, again)
	}
	if err := c.Create(ctx, credSecret(optName, "uid-b", "third")); err != nil {
		t.Fatal(err)
	}
	if back := revision("recreated"); back == gone || back == changed || back == present {
		t.Error("recreating an optional Secret did not produce a new token")
	}

	// An optional Secret without the key is allowed too (the env var is unset).
	missingKey := []corev1.EnvVar{secretEnv("X", optName, "no-such-key", true)}
	if _, err := credentialsRevision(ctx, c, testNamespaceDefault, missingKey); err != nil {
		t.Errorf("optional reference to a missing key: %v", err)
	}
}

func TestWithCredentialsRevisionDoesNotModifyInput(t *testing.T) {
	spec := map[string]string{"k8s.v1.cni.cncf.io/networks": "kea-system/vlan500"}
	out := withCredentialsRevision(spec, "abc")
	if len(spec) != 1 {
		t.Errorf("input map modified: %v", spec)
	}
	if out[resources.CredentialsRevisionAnnotation] != "abc" || out["k8s.v1.cni.cncf.io/networks"] == "" {
		t.Errorf("output = %v", out)
	}
	if got := withCredentialsRevision(spec, ""); len(got) != 1 {
		t.Errorf("empty token added an annotation: %v", got)
	}
}

func dhcp4WithLeaseDB(name, ns, secret string) *keav1alpha1.KeaDhcp4Server {
	s := standaloneDhcp4Server()
	s.Name, s.Namespace, s.UID = name, ns, types.UID("uid-"+name)
	if secret != "" {
		s.Spec.LeaseDatabase = &keav1alpha1.DatabaseConfig{
			Type: keav1alpha1.DatabaseTypeMySQL, Name: testKeaName, Host: testDBHost,
			CredentialsSecretRef: &corev1.LocalObjectReference{Name: secret},
		}
	}
	return s
}

// A Secret event enqueues only the CRs in its namespace that reference it.
func TestEnqueueReferencingCRsDhcp4(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).WithObjects(
		dhcp4WithLeaseDB("uses-it", testNamespaceDefault, testCredSecret),
		dhcp4WithLeaseDB("no-db", testNamespaceDefault, ""),
		dhcp4WithLeaseDB("other-secret", testNamespaceDefault, testOtherSecret),
		dhcp4WithLeaseDB("other-namespace", testOtherNS, testCredSecret),
	).Build()
	mapFn := enqueueReferencingCRs(c, listDhcp4SecretRefs)

	got := mapFn(context.Background(), credSecret(testCredSecret, "uid", "x"))
	if len(got) != 1 || got[0].Name != "uses-it" || got[0].Namespace != testNamespaceDefault {
		t.Errorf("requests = %v, want only default/uses-it", got)
	}
}

func TestAnyOfDropsDuplicates(t *testing.T) {
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "a", Namespace: "n"}}
	one := func(context.Context, client.Object) []ctrl.Request { return []ctrl.Request{req} }
	if got := anyOf(one, one)(context.Background(), &corev1.Secret{}); len(got) != 1 {
		t.Errorf("requests = %v, want one", got)
	}
}

func deploymentCredentials(t *testing.T, c client.Client) (string, []string) {
	t.Helper()
	d := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: resources.DeploymentName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}, d); err != nil {
		t.Fatalf("getting Deployment: %v", err)
	}
	var names []string
	for _, ctr := range d.Spec.Template.Spec.Containers {
		for _, ev := range ctr.Env {
			if ev.ValueFrom != nil && ev.ValueFrom.SecretKeyRef != nil {
				names = append(names, ev.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	return d.Spec.Template.Annotations[resources.CredentialsRevisionAnnotation], names
}

// Rotating the referenced Secret rolls the pods exactly once; a missing
// Secret is reported without touching the running workload.
func TestDhcp4CredentialRotationRollsPods(t *testing.T) {
	ctx := context.Background()
	server := dhcp4WithLeaseDB(testDhcp4CR, testNamespaceDefault, testCredSecret)
	server.Spec.HostsDatabases = []keav1alpha1.DatabaseConfig{{
		Type: keav1alpha1.DatabaseTypeMySQL, Name: "hosts", Host: testDBHost,
		CredentialsSecretRef: &corev1.LocalObjectReference{Name: testOtherSecret},
	}}
	r, c := newDhcp4Reconciler(t, nil, server,
		credSecret(testCredSecret, "uid-1", testCredPassword), credSecret(testOtherSecret, "uid-2", "x"))
	r.Recorder = record.NewFakeRecorder(100)

	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	first, envSecrets := deploymentCredentials(t, c)
	if first == "" {
		t.Fatal("no credentials-revision annotation on the pod template")
	}
	// The watch must cover exactly the Secrets the pods reference.
	sort.Strings(envSecrets)
	envSet := map[string]bool{}
	for _, n := range envSecrets {
		envSet[n] = true
	}
	watched := dhcp4SecretNames(getDhcp4(t, c))
	for _, n := range watched {
		if !envSet[n] {
			t.Errorf("watched Secret %q is not referenced by the pods", n)
		}
		delete(envSet, n)
	}
	if len(envSet) != 0 {
		t.Errorf("pods reference Secrets the watch ignores: %v", envSet)
	}

	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if again, _ := deploymentCredentials(t, c); again != first {
		t.Errorf("unchanged Secrets changed the token: %q -> %q", first, again)
	}

	s := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: testCredSecret, Namespace: testNamespaceDefault}, s); err != nil {
		t.Fatal(err)
	}
	s.Data[testKeyPassword] = []byte("rotated")
	if err := c.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	rotated, _ := deploymentCredentials(t, c)
	if rotated == first {
		t.Fatal("rotating the Secret did not change the pod template")
	}

	if err := c.Delete(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := reconcileDhcp4(r); err != nil {
		t.Fatalf("a missing Secret is reported through status, not a retry loop: %v", err)
	}
	if kept, _ := deploymentCredentials(t, c); kept != rotated {
		t.Errorf("workload changed while its Secret was missing: %q -> %q", rotated, kept)
	}
	if d := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeDegraded); d == nil || d.Status != condTrue || d.Reason != reasonCredentialsUnavailable {
		t.Errorf("Degraded = %+v, want True/%s", d, reasonCredentialsUnavailable)
	}

	if err := c.Create(ctx, credSecret(testCredSecret, "uid-9", "rotated")); err != nil {
		t.Fatal(err)
	}
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if recreated, _ := deploymentCredentials(t, c); recreated == rotated {
		t.Error("recreating the Secret did not change the pod template")
	}
	if d := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeDegraded); d == nil || d.Status != condFalse {
		t.Errorf("Degraded = %+v after the Secret returned, want False", d)
	}
}

// credentialComponent describes one CR kind for the parity test below.
type credentialComponent struct {
	name      string
	component string
	cr        client.Object
	status    client.Object
	reconcile func(c client.Client) error
	watched   func(c client.Client) []string
}

func reconcileWith(r interface {
	Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)
}, name string) error {
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespaceDefault}})
	return err
}

// Every component that carries credentials into Kea through SecretKeyRef env
// vars must stamp the credentials revision on its pod template, and the
// Secret watch must map exactly the Secrets its pods reference.
func TestCredentialComponentsStampRevisionAndWatchTheirSecrets(t *testing.T) {
	const crName = "cred-parity"
	meta := metav1.ObjectMeta{Name: crName, Namespace: testNamespaceDefault, UID: "uid-parity"}
	hostsDB := keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: "hosts", Host: testDBHost,
		CredentialsSecretRef: &corev1.LocalObjectReference{Name: testOtherSecret}}
	leaseDB := &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: testKeaName, Host: testDBHost,
		CredentialsSecretRef: &corev1.LocalObjectReference{Name: testCredSecret}}
	keyRef := func(secret string) *corev1.SecretKeySelector {
		return &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: testKeyPassword}
	}
	get := func(c client.Client, obj client.Object) client.Object {
		if err := c.Get(context.Background(), types.NamespacedName{Name: crName, Namespace: testNamespaceDefault}, obj); err != nil {
			t.Fatal(err)
		}
		return obj
	}

	components := []credentialComponent{
		{
			name: "KeaDhcp6Server", component: "dhcp6",
			cr: &keav1alpha1.KeaDhcp6Server{ObjectMeta: meta, Spec: keav1alpha1.KeaDhcp6ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testIfaceEth0}},
				LeaseDatabase:    leaseDB, HostsDatabases: []keav1alpha1.DatabaseConfig{hostsDB}}},
			status: &keav1alpha1.KeaDhcp6Server{},
			reconcile: func(c client.Client) error {
				return reconcileWith(&KeaDhcp6ServerReconciler{Client: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(100)}, crName)
			},
			watched: func(c client.Client) []string {
				return dhcp6SecretNames(get(c, &keav1alpha1.KeaDhcp6Server{}).(*keav1alpha1.KeaDhcp6Server))
			},
		},
		{
			name: "KeaControlAgent", component: "ctrl-agent",
			cr: &keav1alpha1.KeaControlAgent{ObjectMeta: meta, Spec: keav1alpha1.KeaControlAgentSpec{
				Authentication: &keav1alpha1.AuthConfig{Type: "basic", Clients: []keav1alpha1.AuthClient{
					{User: "admin", PasswordSecretKeyRef: keyRef(testCredSecret)},
					{User: "viewer", PasswordSecretKeyRef: keyRef(testOtherSecret)},
				}}}},
			status: &keav1alpha1.KeaControlAgent{},
			reconcile: func(c client.Client) error {
				return reconcileWith(&KeaControlAgentReconciler{Client: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(100)}, crName)
			},
			watched: func(c client.Client) []string {
				return ctrlAgentSecretNames(get(c, &keav1alpha1.KeaControlAgent{}).(*keav1alpha1.KeaControlAgent))
			},
		},
		{
			name: "KeaDhcpDdns", component: "ddns",
			cr: &keav1alpha1.KeaDhcpDdns{ObjectMeta: meta, Spec: keav1alpha1.KeaDhcpDdnsSpec{
				TSIGKeys: []keav1alpha1.TSIGKey{{Name: "k1", Algorithm: "HMAC-SHA256", SecretRef: keyRef(testCredSecret)}}}},
			status: &keav1alpha1.KeaDhcpDdns{},
			reconcile: func(c client.Client) error {
				return reconcileWith(&KeaDhcpDdnsReconciler{Client: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(100)}, crName)
			},
			watched: func(c client.Client) []string {
				return ddnsSecretNames(get(c, &keav1alpha1.KeaDhcpDdns{}).(*keav1alpha1.KeaDhcpDdns))
			},
		},
	}

	for _, tc := range components {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).WithStatusSubresource(tc.status).
				WithObjects(tc.cr, credSecret(testCredSecret, "uid-1", testCredPassword), credSecret(testOtherSecret, "uid-2", "x")).Build()
			if err := tc.reconcile(c); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			d := &appsv1.Deployment{}
			if err := c.Get(context.Background(), types.NamespacedName{
				Name: resources.DeploymentName(crName, tc.component), Namespace: testNamespaceDefault}, d); err != nil {
				t.Fatalf("getting Deployment: %v", err)
			}
			if d.Spec.Template.Annotations[resources.CredentialsRevisionAnnotation] == "" {
				t.Error("no credentials-revision annotation on the pod template")
			}
			referenced := map[string]bool{}
			for _, ctr := range d.Spec.Template.Spec.Containers {
				for _, ev := range ctr.Env {
					if ev.ValueFrom != nil && ev.ValueFrom.SecretKeyRef != nil {
						referenced[ev.ValueFrom.SecretKeyRef.Name] = true
					}
				}
			}
			watched := map[string]bool{}
			for _, n := range tc.watched(c) {
				watched[n] = true
			}
			if len(referenced) == 0 || len(watched) != len(referenced) {
				t.Fatalf("watched Secrets %v, pods reference %v", watched, referenced)
			}
			for n := range referenced {
				if !watched[n] {
					t.Errorf("pods reference Secret %q but the watch ignores it", n)
				}
			}
		})
	}
}
