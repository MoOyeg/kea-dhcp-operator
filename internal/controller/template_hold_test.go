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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const legacyMarker = " # rendered by an earlier operator release"

// simulateEarlierRelease rewrites the workload the way v0.0.35 left it: no
// operator annotations on the object, and a pod template rendered slightly
// differently (here: a marker on the main or init command).
func simulateEarlierRelease(t *testing.T, c client.Client, ha bool) corev1.PodTemplateSpec {
	t.Helper()
	ctx := context.Background()
	key := types.NamespacedName{Name: resources.DeploymentName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}
	obj, err := getWorkload(ctx, c, key, ha)
	if err != nil || obj == nil {
		t.Fatalf("workload: %v", err)
	}
	obj.SetAnnotations(nil)
	tmpl := podTemplate(obj)
	cmd := tmpl.Spec.Containers[0].Command
	if ha {
		cmd = tmpl.Spec.InitContainers[0].Command
	}
	cmd[len(cmd)-1] += legacyMarker
	delete(tmpl.Annotations, resources.CredentialsRevisionAnnotation)
	if err := c.Update(ctx, obj); err != nil {
		t.Fatal(err)
	}
	return *tmpl.DeepCopy()
}

func currentTemplate(t *testing.T, c client.Client, ha bool) (corev1.PodTemplateSpec, map[string]string) {
	t.Helper()
	obj, err := getWorkload(context.Background(), c,
		types.NamespacedName{Name: resources.DeploymentName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}, ha)
	if err != nil || obj == nil {
		t.Fatalf("workload: %v", err)
	}
	return *podTemplate(obj), obj.GetAnnotations()
}

func hasLegacyMarker(tmpl corev1.PodTemplateSpec) bool {
	for _, ctr := range append(tmpl.Spec.InitContainers, tmpl.Spec.Containers...) {
		if strings.Contains(strings.Join(ctr.Command, " "), legacyMarker) {
			return true
		}
	}
	return false
}

// Upgrading the operator must not restart pods that keep memfile leases, even
// though the new release renders their template differently; measured on a
// cluster, the StatefulSet roll left both HA peers with no leases.
func TestOperatorUpgradeHoldsMemfileTemplates(t *testing.T) {
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
			r, c := newDhcp4Reconciler(t, nil, server)
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			legacy := simulateEarlierRelease(t, c, ha)

			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			tmpl, anns := currentTemplate(t, c, ha)
			if !reflect.DeepEqual(tmpl, legacy) {
				t.Fatal("the upgrade replaced the pod template of a memfile workload, which restarts its pods")
			}
			if anns[resources.TemplateRevisionAnnotation] != resources.LegacyTemplateRevision {
				t.Errorf("recorded revision %q, want the held %q", anns[resources.TemplateRevisionAnnotation], resources.LegacyTemplateRevision)
			}
			cond := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeRestartPending)
			if cond == nil || cond.Status != condTrue || !strings.Contains(cond.Message, resources.ApproveRestartAnnotation+"="+resources.TemplateRevision) {
				t.Fatalf("RestartPending = %+v, want True naming the approval annotation", cond)
			}

			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			if again, _ := currentTemplate(t, c, ha); !reflect.DeepEqual(again, legacy) {
				t.Fatal("template changed on a repeated reconcile while held")
			}

			updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
				s.Annotations = map[string]string{resources.ApproveRestartAnnotation: resources.TemplateRevision}
			})
			if err := reconcileDhcp4(r); err != nil {
				t.Fatal(err)
			}
			tmpl, anns = currentTemplate(t, c, ha)
			if hasLegacyMarker(tmpl) || anns[resources.TemplateRevisionAnnotation] != resources.TemplateRevision {
				t.Errorf("approval did not apply the current template (revision %q)", anns[resources.TemplateRevisionAnnotation])
			}
			if cond := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeRestartPending); cond == nil || cond.Status != condFalse {
				t.Errorf("RestartPending = %+v after approval, want False", cond)
			}
		})
	}
}

// A spec change restarts the pods anyway, so it carries the new template.
func TestSpecChangeAppliesHeldTemplate(t *testing.T) {
	r, c := newDhcp4Reconciler(t, nil, haDhcp4Server(2))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	simulateEarlierRelease(t, c, true)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
		s.Spec.Container.ImagePullPolicy = corev1.PullAlways
		s.Generation++ // the API server bumps it on a spec change
	})
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	tmpl, anns := currentTemplate(t, c, true)
	if hasLegacyMarker(tmpl) || anns[resources.TemplateRevisionAnnotation] != resources.TemplateRevision {
		t.Errorf("spec change did not apply the current template (revision %q)", anns[resources.TemplateRevisionAnnotation])
	}
	if tmpl.Spec.Containers[0].ImagePullPolicy != corev1.PullAlways {
		t.Error("spec change not applied")
	}
}

// Database-backed leases survive a restart, so the upgrade applies at once.
func TestOperatorUpgradeAppliesTemplateWithDatabaseLeases(t *testing.T) {
	server := haDhcp4Server(2)
	server.Spec.LeaseDatabase = &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: testKeaName, Host: testDBHost,
		CredentialsSecretRef: &corev1.LocalObjectReference{Name: testCredSecret}}
	r, c := newDhcp4Reconciler(t, nil, server, credSecret(testCredSecret, "uid-1", testCredPassword))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	simulateEarlierRelease(t, c, true)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if tmpl, anns := currentTemplate(t, c, true); hasLegacyMarker(tmpl) || anns[resources.TemplateRevisionAnnotation] != resources.TemplateRevision {
		t.Error("database-backed workload kept the old template")
	}
}

func TestDecideTemplateHold(t *testing.T) {
	owner := &keav1alpha1.KeaDhcp4Server{ObjectMeta: metav1.ObjectMeta{Generation: 3}}
	workload := func(rev, gen string) *appsv1.StatefulSet {
		s := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		if rev != "" {
			s.Annotations[resources.TemplateRevisionAnnotation] = rev
		}
		if gen != "" {
			s.Annotations[resources.AppliedGenerationAnnotation] = gen
		}
		s.Spec.Template.Annotations = map[string]string{resources.ConfigHashAnnotation: hashCfg}
		return s
	}
	tests := []struct {
		name       string
		existing   client.Object
		durable    bool
		configHash string
		wantHeld   bool
	}{
		{"new workload", nil, false, hashCfg, false},
		{"same revision (e.g. a Stork token change) is never held", workload(resources.TemplateRevision, "3"), false, hashCfg, false},
		{"legacy memfile workload is held", workload("", ""), false, hashCfg, true},
		{"older revision, same generation, is held", workload(resources.LegacyTemplateRevision, "3"), false, hashCfg, true},
		{"older revision with a spec change applies", workload(resources.LegacyTemplateRevision, "2"), false, hashCfg, false},
		{"older revision with a config change applies", workload(resources.LegacyTemplateRevision, "3"), false, hashCfg2, false},
		{"database leases apply", workload("", ""), true, hashCfg, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := decideTemplateHold(tc.existing, owner, tc.durable, tc.configHash, "")
			if held := h.keep != nil; held != tc.wantHeld {
				t.Errorf("held = %v, want %v (%+v)", held, tc.wantHeld, h)
			}
			if (h.pending != "") != tc.wantHeld {
				t.Errorf("pending = %q", h.pending)
			}
		})
	}
}

// The DHCPv6 controller applies the same hold to its Deployment.
func TestOperatorUpgradeHoldsMemfileTemplateDhcp6(t *testing.T) {
	ctx := context.Background()
	server := &keav1alpha1.KeaDhcp6Server{
		ObjectMeta: metav1.ObjectMeta{Name: testDhcp4CR, Namespace: testNamespaceDefault, UID: "uid-v6"},
		Spec:       keav1alpha1.KeaDhcp6ServerSpec{InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testIfaceEth0}}},
	}
	c := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).WithStatusSubresource(&keav1alpha1.KeaDhcp6Server{}).WithObjects(server).Build()
	r := &KeaDhcp6ServerReconciler{Client: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(100)}
	if err := reconcileWith(r, testDhcp4CR); err != nil {
		t.Fatal(err)
	}
	key := types.NamespacedName{Name: resources.DeploymentName(testDhcp4CR, "dhcp6"), Namespace: testNamespaceDefault}
	d := &appsv1.Deployment{}
	if err := c.Get(ctx, key, d); err != nil {
		t.Fatal(err)
	}
	d.Annotations = nil
	d.Spec.Template.Spec.Containers[0].Command[len(d.Spec.Template.Spec.Containers[0].Command)-1] += legacyMarker
	if err := c.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	legacy := *d.Spec.Template.DeepCopy()
	if err := reconcileWith(r, testDhcp4CR); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, d); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Spec.Template, legacy) {
		t.Fatal("the upgrade replaced the pod template of a memfile DHCPv6 server")
	}
	got := &keav1alpha1.KeaDhcp6Server{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(server), got); err != nil {
		t.Fatal(err)
	}
	pending := false
	for _, cond := range got.Status.Conditions {
		pending = pending || (cond.Type == keav1alpha1.ConditionTypeRestartPending && cond.Status == condTrue)
	}
	if !pending {
		t.Error("RestartPending not reported for the DHCPv6 server")
	}
}
