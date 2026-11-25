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
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// A password the old sed substitution corrupted in several ways at once.
const punctuationPassword = `a&b|c"d'e\f$KEA_LEASE_DB_USER` + "`g`\nh ü"

func leasePassword(t *testing.T, configJSON []byte) string {
	t.Helper()
	var root map[string]interface{}
	if err := json.Unmarshal(configJSON, &root); err != nil {
		t.Fatalf("configuration is not valid JSON: %v", err)
	}
	dhcp4, _ := root["Dhcp4"].(map[string]interface{})
	db, _ := dhcp4["lease-database"].(map[string]interface{})
	pw, _ := db["password"].(string)
	return pw
}

// Each HA ordinal gets its own configuration Secret with the credential
// rendered in exactly, the StatefulSet mounts those Secrets, and the init
// container no longer receives credentials.
func TestDhcp4HAConfigSecrets(t *testing.T) {
	server := haDhcp4Server(2)
	server.Spec.LeaseDatabase = &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: testKeaName, Host: testDBHost,
		CredentialsSecretRef: &corev1.LocalObjectReference{Name: testCredSecret}}
	r, c := newDhcp4Reconciler(t, nil, server, credSecret(testCredSecret, "uid-1", punctuationPassword))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	hash, _ := statefulSetHash(t, c)
	rev := resources.ShortRevision(hash)
	for i := 0; i < 2; i++ {
		secret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Name: resources.HAConfigSecretRevisionName(testDhcp4CR, testComponent4, i, rev), Namespace: testNamespaceDefault}, secret); err != nil {
			t.Fatalf("ordinal %d configuration Secret: %v", i, err)
		}
		cfg := secret.Data[resources.ConfigFileName(testComponent4)]
		if got := leasePassword(t, cfg); got != punctuationPassword {
			t.Errorf("ordinal %d: password %q, want %q", i, got, punctuationPassword)
		}
		if !strings.Contains(string(cfg), fmt.Sprintf(`"this-server-name": %q`, server.Spec.HighAvailability.Peers[i].Name)) {
			t.Errorf("ordinal %d Secret holds another ordinal's configuration", i)
		}
		cm := &corev1.ConfigMap{}
		if err := c.Get(ctx, types.NamespacedName{Name: resources.HAConfigMapRevisionName(testDhcp4CR, testComponent4, i, rev), Namespace: testNamespaceDefault}, cm); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(cm.Data[resources.ConfigFileName(testComponent4)], "a&b|c") {
			t.Errorf("ordinal %d ConfigMap contains the credential", i)
		}
	}
	sts := &appsv1.StatefulSet{}
	if err := c.Get(ctx, workloadKey(), sts); err != nil {
		t.Fatal(err)
	}
	secretVolumes := 0
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.Secret != nil && strings.HasSuffix(v.Secret.SecretName, "-"+rev) && strings.HasPrefix(v.Secret.SecretName, resources.ConfigSecretName(testDhcp4CR, testComponent4)+"-") {
			secretVolumes++
		}
	}
	if secretVolumes != 2 {
		t.Errorf("StatefulSet mounts %d configuration Secrets, want 2", secretVolumes)
	}
	if env := sts.Spec.Template.Spec.InitContainers[0].Env; len(env) != 0 {
		t.Errorf("init container still receives env vars: %v", env)
	}
}

// The credential reaches the mounted configuration byte for byte, and never
// appears in the CR status or in Events, including the events and status of
// a reconcile that fails because the Secret went missing.
func TestCredentialValuesStayOutOfStatusAndEvents(t *testing.T) {
	server := dhcp4WithLeaseDB(testDhcp4CR, testNamespaceDefault, testCredSecret)
	r, c := newDhcp4Reconciler(t, nil, server, credSecret(testCredSecret, "uid-1", punctuationPassword))
	recorder := record.NewFakeRecorder(100)
	r.Recorder = recorder
	ctx := context.Background()

	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.ConfigSecretName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}, secret); err != nil {
		t.Fatal(err)
	}
	if got := leasePassword(t, secret.Data[resources.ConfigFileName(testComponent4)]); got != punctuationPassword {
		t.Errorf("password %q, want %q", got, punctuationPassword)
	}

	// Remove the Secret so the failure paths emit their events too.
	if err := c.Delete(ctx, credSecret(testCredSecret, "uid-1", "")); err != nil {
		t.Fatal(err)
	}
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}

	status, _ := json.Marshal(getDhcp4(t, c).Status)
	workload := &appsv1.Deployment{}
	if err := c.Get(ctx, workloadKey(), workload); err != nil {
		t.Fatal(err)
	}
	annotations, _ := json.Marshal([]map[string]string{workload.Annotations, workload.Spec.Template.Annotations})
	close(recorder.Events)
	var events []string
	for e := range recorder.Events {
		events = append(events, e)
	}
	for _, leak := range []string{"a&b|c", "\\f$KEA", "`g`"} {
		for where, text := range map[string]string{"status": string(status), "annotations": string(annotations), "events": strings.Join(events, "\n")} {
			if strings.Contains(text, leak) {
				t.Errorf("credential fragment %q appears in %s", leak, where)
			}
		}
	}
}
