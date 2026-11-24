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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const (
	reasonModeChange  = "ModeChangeRequiresMigration"
	reasonRenderError = "RenderError"
)

func standaloneDhcp4Server() *keav1alpha1.KeaDhcp4Server {
	s := haDhcp4Server(1)
	s.Spec.HighAvailability = nil
	return s
}

func workloadKey() types.NamespacedName {
	// The Deployment and the StatefulSet share one name.
	return types.NamespacedName{Name: resources.DeploymentName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}
}

// workloadVersions returns the resourceVersion of the Deployment and the
// StatefulSet, or "" for one that does not exist.
func workloadVersions(t *testing.T, c client.Client) (deploy, sts string) {
	t.Helper()
	ctx := context.Background()
	d := &appsv1.Deployment{}
	if err := c.Get(ctx, workloadKey(), d); err == nil {
		deploy = d.ResourceVersion
	} else if !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	s := &appsv1.StatefulSet{}
	if err := c.Get(ctx, workloadKey(), s); err == nil {
		sts = s.ResourceVersion
	} else if !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	return deploy, sts
}

func updateDhcp4Spec(t *testing.T, c client.Client, mutate func(*keav1alpha1.KeaDhcp4Server)) {
	t.Helper()
	s := getDhcp4(t, c)
	mutate(s)
	if err := c.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func assertModeChangeBlocked(t *testing.T, c client.Client, wantDeploy, wantSTS string) {
	t.Helper()
	gotDeploy, gotSTS := workloadVersions(t, c)
	if gotDeploy != wantDeploy || gotSTS != wantSTS {
		t.Errorf("workloads changed: Deployment %q -> %q, StatefulSet %q -> %q", wantDeploy, gotDeploy, wantSTS, gotSTS)
	}
	s := getDhcp4(t, c)
	if d := conditionOf(s, keav1alpha1.ConditionTypeDegraded); d == nil || d.Status != condTrue || d.Reason != reasonModeChange {
		t.Errorf("Degraded = %+v, want True/%s", d, reasonModeChange)
	}
	if r := conditionOf(s, keav1alpha1.ConditionTypeReady); r == nil || r.Status != condFalse {
		t.Errorf("Ready = %+v, want False while the requested topology is not applied", r)
	}
	if s.Status.Phase != keav1alpha1.PhaseError {
		t.Errorf("phase = %q, want %q so an umbrella KeaServer does not report the child ready", s.Status.Phase, keav1alpha1.PhaseError)
	}
}

// Adding high-availability to a running standalone server must not start a
// StatefulSet next to the Deployment: both would answer for the same pool.
func TestDhcp4StandaloneToHAIsBlocked(t *testing.T) {
	r, c := newDhcp4Reconciler(t, nil, standaloneDhcp4Server())
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	deploy, sts := workloadVersions(t, c)
	if deploy == "" || sts != "" {
		t.Fatalf("standalone reconcile: Deployment %q StatefulSet %q", deploy, sts)
	}

	updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
		ha := haDhcp4Server(2)
		s.Spec.HighAvailability = ha.Spec.HighAvailability
		s.Spec.Replicas = ha.Spec.Replicas
	})
	if err := reconcileDhcp4(r); err != nil {
		t.Fatalf("a blocked mode change is reported through status, not a retry loop: %v", err)
	}
	assertModeChangeBlocked(t, c, deploy, "")
	if n := haConfigMapCount(t, c); n != 1 {
		t.Errorf("%d ConfigMaps, want only the standalone one (no HA ConfigMaps written)", n)
	}

	// Reverting the spec clears the condition.
	updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
		one := int32(1)
		s.Spec.HighAvailability, s.Spec.Replicas = nil, &one
	})
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if d := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeDegraded); d == nil || d.Status != condFalse {
		t.Errorf("Degraded = %+v after reverting, want False", d)
	}
}

// Removing high-availability must not start a Deployment next to the
// StatefulSet.
func TestDhcp4HAToStandaloneIsBlocked(t *testing.T) {
	r, c := newDhcp4Reconciler(t, nil, haDhcp4Server(2))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	deploy, sts := workloadVersions(t, c)
	if deploy != "" || sts == "" {
		t.Fatalf("HA reconcile: Deployment %q StatefulSet %q", deploy, sts)
	}

	updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
		one := int32(1)
		s.Spec.HighAvailability, s.Spec.Replicas = nil, &one
	})
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	assertModeChangeBlocked(t, c, "", sts)
}

// An upgrade from a release without the guard can find both kinds already
// running. The operator must not pick one to scale or delete.
func TestDhcp4BothWorkloadsPresentIsReported(t *testing.T) {
	server := haDhcp4Server(2)
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(server, keav1alpha1.GroupVersion.WithKind("KeaDhcp4Server"))}
	meta := metav1.ObjectMeta{Name: workloadKey().Name, Namespace: testNamespaceDefault, OwnerReferences: owner}
	r, c := newDhcp4Reconciler(t, nil, server,
		&appsv1.Deployment{ObjectMeta: meta}, &appsv1.StatefulSet{ObjectMeta: meta})
	deploy, sts := workloadVersions(t, c)

	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	assertModeChangeBlocked(t, c, deploy, sts)
}

// Only workloads this CR controls count: a same-named StatefulSet owned by
// something else is not a previous mode of this server.
func TestDhcp4UnownedWorkloadOfOtherKindIsIgnored(t *testing.T) {
	foreign := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: workloadKey().Name, Namespace: testNamespaceDefault}}
	r, c := newDhcp4Reconciler(t, nil, standaloneDhcp4Server(), foreign)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if deploy, _ := workloadVersions(t, c); deploy == "" {
		t.Error("Deployment not created next to an unowned StatefulSet")
	}
	if d := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeDegraded); d != nil && d.Status == condTrue {
		t.Errorf("Degraded = %+v, want no mode-change condition for an unowned object", d)
	}
}

// ModeChangeRequiresMigration tells the operator it is safe to delete the
// serving workload, so it must only appear for a target that would start: an
// invalid or credential-less target reports that instead, and the serving
// workload stays untouched either way.
func TestDhcp4MigrationConditionRequiresAValidTarget(t *testing.T) {
	ttl := int32(600)
	tests := []struct {
		name       string
		target     func(s *keav1alpha1.KeaDhcp4Server)
		objs       []client.Object
		wantReason string
		wantErr    bool
	}{
		{
			name: "invalid DDNS",
			target: func(s *keav1alpha1.KeaDhcp4Server) {
				s.Spec.DDNSTTL, s.Spec.DDNSTTLPercent = &ttl, ttlPercentJSON("0.5")
			},
			wantReason: reasonRenderError, wantErr: true,
		},
		{
			name: "duplicate HA peers",
			target: func(s *keav1alpha1.KeaDhcp4Server) {
				s.Spec.HighAvailability.Peers[1].Name = s.Spec.HighAvailability.Peers[0].Name
			},
			wantReason: reasonRenderError, wantErr: true,
		},
		{
			name: "missing lease database credentials",
			target: func(s *keav1alpha1.KeaDhcp4Server) {
				s.Spec.LeaseDatabase = &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: testKeaName, Host: testDBHost,
					CredentialsSecretRef: &corev1.LocalObjectReference{Name: "not-created-yet"}}
			},
			wantReason: reasonCredentialsUnavailable,
		},
		{
			name:       "valid target",
			target:     func(*keav1alpha1.KeaDhcp4Server) {},
			wantReason: reasonModeChange,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newDhcp4Reconciler(t, nil, append([]client.Object{standaloneDhcp4Server()}, tc.objs...)...)
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			deploy, _ := workloadVersions(t, c)

			updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
				ha := haDhcp4Server(2)
				s.Spec.HighAvailability, s.Spec.Replicas = ha.Spec.HighAvailability, ha.Spec.Replicas
				tc.target(s)
			})
			err := reconcileDhcp4(r)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Reconcile error = %v, want error: %v", err, tc.wantErr)
			}

			s := getDhcp4(t, c)
			if gotDeploy, gotSTS := workloadVersions(t, c); gotDeploy != deploy || gotSTS != "" {
				t.Errorf("workloads changed: Deployment %q -> %q, StatefulSet %q", deploy, gotDeploy, gotSTS)
			}
			valid := conditionOf(s, keav1alpha1.ConditionTypeConfigValid)
			degraded := conditionOf(s, keav1alpha1.ConditionTypeDegraded)
			switch tc.wantReason {
			case reasonModeChange:
				if degraded == nil || degraded.Reason != reasonModeChange || valid == nil || valid.Status != condTrue {
					t.Errorf("valid target: Degraded %+v, ConfigurationValid %+v; want the migration condition with a valid configuration", degraded, valid)
				}
			case reasonRenderError:
				if valid == nil || valid.Status != condFalse {
					t.Errorf("ConfigurationValid = %+v, want False", valid)
				}
				if degraded != nil && degraded.Reason == reasonModeChange {
					t.Error("an invalid target was reported as ready for migration")
				}
			default:
				if degraded == nil || degraded.Reason != tc.wantReason {
					t.Errorf("Degraded = %+v, want reason %s", degraded, tc.wantReason)
				}
			}
		})
	}
}
