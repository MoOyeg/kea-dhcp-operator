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
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const (
	revOld            = "rev-old"
	revNew            = "rev-new"
	hashCfg           = "cfg-1"
	hashCfg2          = "cfg-2"
	subtestStandalone = "standalone"
)

func workloadWith(template, meta map[string]string) *appsv1.Deployment {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Annotations: meta}}
	d.Spec.Template.Annotations = template
	return d
}

func TestDecideCredentialRollout(t *testing.T) {
	stamped := func(rev string) map[string]string {
		return map[string]string{resources.ConfigHashAnnotation: hashCfg, resources.CredentialsRevisionAnnotation: rev}
	}
	legacy := map[string]string{resources.ConfigHashAnnotation: hashCfg} // written by a release without the annotation
	adopted := map[string]string{resources.AppliedCredentialsRevisionAnnotation: revOld}

	tests := []struct {
		name       string
		existing   client.Object
		configHash string
		autoRoll   bool
		approved   string
		want       credentialRollout
	}{
		{"new workload", nil, hashCfg, false, "", credentialRollout{stamp: revNew, applied: revNew}},
		{"already current", workloadWith(stamped(revNew), nil), hashCfg, false, "", credentialRollout{stamp: revNew, applied: revNew}},
		{"upgrade adopts without a restart", workloadWith(legacy, nil), hashCfg, false, "", credentialRollout{applied: revNew}},
		{"upgrade with a config change rolls anyway", workloadWith(legacy, nil), hashCfg2, false, "", credentialRollout{stamp: revNew, applied: revNew}},
		{"adopted and unchanged keeps the template", workloadWith(legacy, map[string]string{resources.AppliedCredentialsRevisionAnnotation: revNew}), hashCfg, false, "", credentialRollout{applied: revNew}},
		{"memfile rotation is held", workloadWith(stamped(revOld), nil), hashCfg, false, "", credentialRollout{stamp: revOld, applied: revOld, pending: revNew}},
		{"memfile rotation after adoption is held", workloadWith(legacy, adopted), hashCfg, false, "", credentialRollout{applied: revOld, pending: revNew}},
		{"database leases roll", workloadWith(stamped(revOld), nil), hashCfg, true, "", credentialRollout{stamp: revNew, applied: revNew}},
		{"approval of this revision rolls", workloadWith(stamped(revOld), nil), hashCfg, false, revNew, credentialRollout{stamp: revNew, applied: revNew}},
		{"approval of another revision does not", workloadWith(stamped(revOld), nil), hashCfg, false, revOld, credentialRollout{stamp: revOld, applied: revOld, pending: revNew}},
		{"a config change applies the held revision", workloadWith(stamped(revOld), nil), hashCfg2, false, "", credentialRollout{stamp: revNew, applied: revNew}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decideCredentialRollout(tc.existing, revNew, tc.configHash, tc.autoRoll, tc.approved)
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if got := decideCredentialRollout(workloadWith(stamped(revOld), nil), "", hashCfg, false, ""); got != (credentialRollout{}) {
		t.Errorf("no credentials referenced: got %+v, want nothing stamped", got)
	}
}

// memfileWithHostsDB is the configuration from the review: leases in memfile
// (pod-local emptyDir) and host reservations in MySQL with a credential Secret.
func memfileWithHostsDB(server *keav1alpha1.KeaDhcp4Server) *keav1alpha1.KeaDhcp4Server {
	persist := true
	server.Spec.LeaseDatabase = &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMemfile, Persist: &persist}
	server.Spec.HostsDatabase = &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: testHostsDBName, Host: testDBHost,
		CredentialsSecretRef: &corev1.LocalObjectReference{Name: testCredSecret}}
	return server
}

// workloadState returns the pod template and the object annotations of the
// DHCPv4 workload.
func workloadState(t *testing.T, c client.Client, ha bool) (map[string]string, map[string]string, corev1.PodTemplateSpec) {
	t.Helper()
	key := types.NamespacedName{Name: resources.DeploymentName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}
	if ha {
		s := &appsv1.StatefulSet{}
		if err := c.Get(context.Background(), key, s); err != nil {
			t.Fatal(err)
		}
		return s.Spec.Template.Annotations, s.Annotations, s.Spec.Template
	}
	d := &appsv1.Deployment{}
	if err := c.Get(context.Background(), key, d); err != nil {
		t.Fatal(err)
	}
	return d.Spec.Template.Annotations, d.Annotations, d.Spec.Template
}

// stripCredentialAnnotations rewrites the workload the way a release without
// credential tracking left it.
func stripCredentialAnnotations(t *testing.T, c client.Client, ha bool) {
	t.Helper()
	ctx := context.Background()
	key := types.NamespacedName{Name: resources.DeploymentName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}
	var obj client.Object = &appsv1.Deployment{}
	if ha {
		obj = &appsv1.StatefulSet{}
	}
	if err := c.Get(ctx, key, obj); err != nil {
		t.Fatal(err)
	}
	delete(podTemplateAnnotations(obj), resources.CredentialsRevisionAnnotation)
	anns := obj.GetAnnotations()
	delete(anns, resources.AppliedCredentialsRevisionAnnotation)
	obj.SetAnnotations(anns)
	if err := c.Update(ctx, obj); err != nil {
		t.Fatal(err)
	}
}

func rotateSecret(t *testing.T, c client.Client, password string) {
	t.Helper()
	s := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testCredSecret, Namespace: testNamespaceDefault}, s); err != nil {
		t.Fatal(err)
	}
	s.Data[testKeyPassword] = []byte(password)
	if err := c.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

// With memfile leases, neither an operator upgrade nor a rotation of the
// hosts-database Secret may restart the pods on its own: that would empty
// the lease files and let Kea hand out addresses that are still in use.
func TestMemfileLeasesHoldCredentialRollouts(t *testing.T) {
	for _, ha := range []bool{false, true} {
		name := subtestStandalone
		if ha {
			name = "ha"
		}
		t.Run(name, func(t *testing.T) {
			server := standaloneDhcp4Server()
			if ha {
				server = haDhcp4Server(2)
			}
			r, c := newDhcp4Reconciler(t, nil, memfileWithHostsDB(server), credSecret(testCredSecret, "uid-1", testCredPassword))
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}

			// Upgrade from a release without credential tracking: no restart.
			stripCredentialAnnotations(t, c, ha)
			_, _, before := workloadState(t, c, ha)
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			tmpl, meta, after := workloadState(t, c, ha)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("upgrade changed the pod template, which restarts the pods:\nbefore %v\nafter  %v", before.Annotations, after.Annotations)
			}
			adopted := meta[resources.AppliedCredentialsRevisionAnnotation]
			if adopted == "" || tmpl[resources.CredentialsRevisionAnnotation] != "" {
				t.Fatalf("upgrade should adopt the revision on the workload object only: object %v, template %v", meta, tmpl)
			}

			// Rotating the hosts-database Secret is held and reported.
			rotateSecret(t, c, "rotated")
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			if _, _, held := workloadState(t, c, ha); !reflect.DeepEqual(before, held) {
				t.Fatalf("credential rotation restarted pods that keep memfile leases: %v", held.Annotations)
			}
			cond := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeCredentialsRolloutPending)
			if cond == nil || cond.Status != condTrue || !strings.Contains(cond.Message, resources.ApproveCredentialRolloutAnnotation+"=") {
				t.Fatalf("CredentialsRolloutPending = %+v, want True naming the approval annotation", cond)
			}
			pending := cond.Message[strings.Index(cond.Message, resources.ApproveCredentialRolloutAnnotation+"=")+len(resources.ApproveCredentialRolloutAnnotation)+1:]
			pending = strings.Fields(pending)[0]
			if pending == adopted {
				t.Fatal("pending revision equals the adopted one")
			}

			// Stable while held.
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			if _, _, still := workloadState(t, c, ha); !reflect.DeepEqual(before, still) {
				t.Fatal("pod template changed on a repeated reconcile while the rollout is held")
			}

			// Approving that revision applies it.
			updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
				s.Annotations = map[string]string{resources.ApproveCredentialRolloutAnnotation: pending}
			})
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			tmpl, meta, _ = workloadState(t, c, ha)
			if tmpl[resources.CredentialsRevisionAnnotation] != pending || meta[resources.AppliedCredentialsRevisionAnnotation] != pending {
				t.Errorf("approved revision not applied: template %v, object %v", tmpl, meta)
			}
			if cond := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeCredentialsRolloutPending); cond == nil || cond.Status != condFalse {
				t.Errorf("CredentialsRolloutPending = %+v after approval, want False", cond)
			}

			// A later rotation is not covered by the old approval, but a
			// configuration change (which restarts the pods anyway) applies it.
			rotateSecret(t, c, "rotated-again")
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			if tmpl, _, _ = workloadState(t, c, ha); tmpl[resources.CredentialsRevisionAnnotation] != pending {
				t.Fatal("an approval for one revision applied a later one")
			}
			updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
				life := testChangedLife
				s.Spec.ValidLifetime = &life
			})
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			tmpl, meta, _ = workloadState(t, c, ha)
			if tmpl[resources.CredentialsRevisionAnnotation] == pending || tmpl[resources.CredentialsRevisionAnnotation] != meta[resources.AppliedCredentialsRevisionAnnotation] {
				t.Errorf("configuration change did not carry the held revision: template %v, object %v", tmpl, meta)
			}
		})
	}
}

// Database-backed leases survive a restart, so their rotations still roll
// automatically, but an upgrade still does not restart anything.
func TestDatabaseLeasesUpgradeAdoptsWithoutRestart(t *testing.T) {
	r, c := newDhcp4Reconciler(t, nil, dhcp4WithLeaseDB(testDhcp4CR, testNamespaceDefault, testCredSecret),
		credSecret(testCredSecret, "uid-1", testCredPassword))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	stripCredentialAnnotations(t, c, false)
	_, _, before := workloadState(t, c, false)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if _, _, after := workloadState(t, c, false); !reflect.DeepEqual(before, after) {
		t.Fatal("upgrade changed the pod template")
	}
	rotateSecret(t, c, "rotated")
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if tmpl, _, _ := workloadState(t, c, false); tmpl[resources.CredentialsRevisionAnnotation] == "" {
		t.Error("rotation with database leases did not roll the pods")
	}
}
