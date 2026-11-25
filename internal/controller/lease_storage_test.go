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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const testUserClaim = "leases-pvc"

func existingClaim() *keav1alpha1.LeaseStorage {
	return &keav1alpha1.LeaseStorage{Type: keav1alpha1.LeaseStorageExistingClaim, ClaimName: testUserClaim}
}

func claimTemplate(size string) *keav1alpha1.LeaseStorage {
	q := resource.MustParse(size)
	return &keav1alpha1.LeaseStorage{Type: keav1alpha1.LeaseStorageClaimTemplate, Size: &q}
}

func TestPlanLeaseStorage(t *testing.T) {
	persistOff := false
	tests := []struct {
		name     string
		storage  *keav1alpha1.LeaseStorage
		db       *keav1alpha1.DatabaseConfig
		ha       bool
		replicas int32
		wantErr  string
		durable  bool
	}{
		{name: "unset is ephemeral", replicas: 1},
		{name: "standalone claim", storage: existingClaim(), replicas: 1, durable: true},
		{name: "HA claim template", storage: claimTemplate("2Gi"), ha: true, replicas: 2, durable: true},
		{name: "claim template defaults its size", storage: &keav1alpha1.LeaseStorage{Type: keav1alpha1.LeaseStorageClaimTemplate}, ha: true, replicas: 2, durable: true},
		{name: "claim needs a name", storage: &keav1alpha1.LeaseStorage{Type: keav1alpha1.LeaseStorageExistingClaim}, replicas: 1, wantErr: "needs claimName"},
		{name: "one claim cannot serve HA peers", storage: existingClaim(), ha: true, replicas: 2, wantErr: "use ClaimTemplate"},
		{name: "one claim, one writer", storage: existingClaim(), replicas: 2, wantErr: "supports one replica"},
		{name: "claim template needs HA", storage: claimTemplate("1Gi"), replicas: 1, wantErr: "use ExistingClaim"},
		{name: "database leases", storage: existingClaim(), replicas: 1,
			db: &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL}, wantErr: "only applies to memfile leases"},
		{name: "persist false", storage: existingClaim(), replicas: 1,
			db: &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMemfile, Persist: &persistOff}, wantErr: "persist: true"},
		{name: "lease file outside the volume", storage: existingClaim(), replicas: 1,
			db: &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMemfile, Name: "/tmp/leases.csv"}, wantErr: "outside /var/lib/kea"},
		{name: "lease file escaping the volume", storage: existingClaim(), replicas: 1,
			db: &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMemfile, Name: "/var/lib/kea/../x.csv"}, wantErr: "outside /var/lib/kea"},
		{name: "lease file inside the volume", storage: existingClaim(), replicas: 1,
			db: &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMemfile, Name: "/var/lib/kea/leases4.csv"}, durable: true},
		{name: "zero size", storage: claimTemplate("0"), ha: true, replicas: 2, wantErr: "must be positive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planLeaseStorage(tc.storage, tc.db, tc.ha, tc.replicas)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.durable() != tc.durable {
				t.Errorf("durable = %v, want %v", plan.durable(), tc.durable)
			}
		})
	}
}

func leasesDurableCondition(t *testing.T, r *KeaDhcp4ServerReconciler) *keav1alpha1.ConditionStatus {
	t.Helper()
	return conditionOf(getDhcp4(t, r.Client), keav1alpha1.ConditionTypeLeasesDurable)
}

func TestStandaloneLeaseClaim(t *testing.T) {
	server := standaloneDhcp4Server()
	server.Spec.LeaseStorage = existingClaim()
	r, c := newDhcp4Reconciler(t, nil, server)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	d := &appsv1.Deployment{}
	if err := c.Get(context.Background(), workloadKey(), d); err != nil {
		t.Fatal(err)
	}
	if d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("strategy %q, want Recreate", d.Spec.Strategy.Type)
	}
	found := false
	for _, v := range d.Spec.Template.Spec.Volumes {
		found = found || (v.Name == resources.LeaseVolumeName && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == testUserClaim)
	}
	if !found {
		t.Error("the lease volume is not the user's claim")
	}
	if cond := leasesDurableCondition(t, r); cond == nil || cond.Status != condTrue || cond.Reason != "PersistentVolume" {
		t.Errorf("LeasesDurable = %+v, want True/PersistentVolume", cond)
	}
}

func TestLeasesDurableReportsEphemeralStorage(t *testing.T) {
	r, _ := newDhcp4Reconciler(t, nil, standaloneDhcp4Server())
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if cond := leasesDurableCondition(t, r); cond == nil || cond.Status != condFalse || cond.Reason != "EmptyDir" {
		t.Errorf("LeasesDurable = %+v, want False/EmptyDir", cond)
	}
}

func TestInvalidLeaseStorageFailsValidation(t *testing.T) {
	server := haDhcp4Server(2)
	server.Spec.LeaseStorage = existingClaim()
	r, c := newDhcp4Reconciler(t, nil, server)
	if err := reconcileDhcp4(r); err == nil {
		t.Fatal("an HA server with one shared claim reconciled without error")
	}
	if cond := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeConfigValid); cond == nil || cond.Status != condFalse {
		t.Errorf("ConfigurationValid = %+v, want False", cond)
	}
	if _, ok := statefulSetHash(t, c); ok {
		t.Error("a StatefulSet was created for an invalid storage spec")
	}
}

// A StatefulSet cannot change its claim templates in place. A storage change
// is refused like a mode change: reported, nothing written, no retry loop.
func TestHALeaseClaimTemplateChangeIsRefused(t *testing.T) {
	for name, change := range map[string]*keav1alpha1.LeaseStorage{
		"new size":          claimTemplate("5Gi"),
		"back to ephemeral": nil,
		"another storage class": func() *keav1alpha1.LeaseStorage {
			ls := claimTemplate("2Gi")
			c := "other"
			ls.StorageClassName = &c
			return ls
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			server := haDhcp4Server(2)
			server.Spec.LeaseStorage = claimTemplate("2Gi")
			r, c := newDhcp4Reconciler(t, nil, server)
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			sts := &appsv1.StatefulSet{}
			if err := c.Get(context.Background(), workloadKey(), sts); err != nil {
				t.Fatal(err)
			}
			if len(sts.Spec.VolumeClaimTemplates) != 1 {
				t.Fatalf("claim templates %+v", sts.Spec.VolumeClaimTemplates)
			}
			if cond := leasesDurableCondition(t, r); cond == nil || cond.Status != condTrue {
				t.Errorf("LeasesDurable = %+v, want True", cond)
			}
			before := sts.Spec.DeepCopy()

			updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) { s.Spec.LeaseStorage = change })
			if err := reconcileDhcp4(r); err != nil {
				t.Fatalf("a refused storage change is reported, not retried: %v", err)
			}
			if err := c.Get(context.Background(), workloadKey(), sts); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, &sts.Spec) {
				t.Error("the StatefulSet was modified")
			}
			if d := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeDegraded); d == nil || d.Reason != "StorageChangeRequiresMigration" {
				t.Errorf("Degraded = %+v, want StorageChangeRequiresMigration", d)
			}
		})
	}
}

// Leases on a persistent volume survive a restart, so neither a credential
// rotation nor an operator upgrade is held for them.
func TestDurableMemfileIsNotHeld(t *testing.T) {
	server := memfileWithHostsDB(haDhcp4Server(2))
	server.Spec.LeaseStorage = claimTemplate("1Gi")
	r, c := newDhcp4Reconciler(t, nil, server, credSecret(testCredSecret, "uid-1", testCredPassword))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	tmpl, _, _ := workloadState(t, c, true)
	first := tmpl[resources.CredentialsRevisionAnnotation]
	rotateSecret(t, c, "rotated")
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	tmpl, _, _ = workloadState(t, c, true)
	if tmpl[resources.CredentialsRevisionAnnotation] == first {
		t.Error("a credential rotation was held although the leases are on persistent volumes")
	}
	if cond := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeCredentialsRolloutPending); cond != nil && cond.Status == condTrue {
		t.Errorf("CredentialsRolloutPending = %+v", cond)
	}

	simulateEarlierRelease(t, c, true)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if tmpl, anns := currentTemplate(t, c, true); hasLegacyMarker(tmpl) || anns[resources.TemplateRevisionAnnotation] != resources.TemplateRevision {
		t.Error("an upgrade was held although the leases are on persistent volumes")
	}
}

func TestDhcp6StandaloneLeaseClaim(t *testing.T) {
	ctx := context.Background()
	server := &keav1alpha1.KeaDhcp6Server{
		ObjectMeta: metav1.ObjectMeta{Name: testDhcp4CR, Namespace: testNamespaceDefault, UID: "uid-v6-claim"},
		Spec: keav1alpha1.KeaDhcp6ServerSpec{InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testIfaceEth0}},
			LeaseStorage: existingClaim()},
	}
	c := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).WithStatusSubresource(&keav1alpha1.KeaDhcp6Server{}).WithObjects(server).Build()
	r := &KeaDhcp6ServerReconciler{Client: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(100)}
	if err := reconcileWith(r, testDhcp4CR); err != nil {
		t.Fatal(err)
	}
	d := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(testDhcp4CR, "dhcp6"), Namespace: testNamespaceDefault}, d); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range d.Spec.Template.Spec.Volumes {
		found = found || (v.Name == resources.LeaseVolumeName && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == testUserClaim)
	}
	if !found || d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("DHCPv6 Deployment does not use the claim with Recreate: %+v / %s", d.Spec.Template.Spec.Volumes, d.Spec.Strategy.Type)
	}
}
