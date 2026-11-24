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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const reasonModeChange = "ModeChangeRequiresMigration"

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
