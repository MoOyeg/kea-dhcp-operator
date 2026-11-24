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
	"strings"
	"sync/atomic"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const (
	testDhcp4CR      = "ha-test"
	testComponent4   = "dhcp4"
	testPeerPrimary  = testServerName1
	testPeerSecond   = "server2"
	testValidLife    = int32(3600)
	testChangedLife  = int32(7200)
	testFailMsgWrite = "injected ConfigMap write failure"
	testHAModeLB     = "load-balancing"
	testRoleSecond   = "secondary"
	condTrue         = string(metav1.ConditionTrue)
	condFalse        = string(metav1.ConditionFalse)
)

func ttlPercentJSON(raw string) *apiextensionsv1.JSON {
	return &apiextensionsv1.JSON{Raw: []byte(raw)}
}

func controllerTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := storkTestScheme(t)
	for _, add := range []func(*runtime.Scheme) error{appsv1.AddToScheme, rbacv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatalf("adding to scheme: %v", err)
		}
	}
	return scheme
}

// haDhcp4Server returns a two-peer load-balancing KeaDhcp4Server.
func haDhcp4Server(replicas int32, peers ...string) *keav1alpha1.KeaDhcp4Server {
	if len(peers) == 0 {
		peers = []string{testPeerPrimary, testPeerSecond}
	}
	ha := &keav1alpha1.HAConfig{Mode: testHAModeLB}
	for i, name := range peers {
		role := testRolePrimary
		if i > 0 {
			role = testRoleSecond
		}
		ha.Peers = append(ha.Peers, keav1alpha1.HAPeer{Name: name, Role: role})
	}
	life := testValidLife
	return &keav1alpha1.KeaDhcp4Server{
		ObjectMeta: metav1.ObjectMeta{Name: testDhcp4CR, Namespace: testNamespaceDefault, UID: "uid-ha-test"},
		Spec: keav1alpha1.KeaDhcp4ServerSpec{
			Replicas:         &replicas,
			InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"eth0"}},
			HighAvailability: ha,
			ValidLifetime:    &life,
			Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: "192.0.2.0/24",
				Pools: []keav1alpha1.Pool4{{Pool: "192.0.2.10 - 192.0.2.100"}}}},
		},
	}
}

// failingWrites makes Create and Update of the named ConfigMap fail while
// the returned flag is set.
func failingWrites(name string) (interceptor.Funcs, *atomic.Bool) {
	var fail atomic.Bool
	check := func(obj client.Object) error {
		if _, ok := obj.(*corev1.ConfigMap); ok && fail.Load() && obj.GetName() == name {
			return apierrors.NewForbidden(corev1.Resource("configmaps"), name, errors.New(testFailMsgWrite))
		}
		return nil
	}
	return interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if err := check(obj); err != nil {
				return err
			}
			return c.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if err := check(obj); err != nil {
				return err
			}
			return c.Update(ctx, obj, opts...)
		},
	}, &fail
}

func newDhcp4Reconciler(t *testing.T, funcs *interceptor.Funcs, objs ...client.Object) (*KeaDhcp4ServerReconciler, client.Client) {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).
		WithStatusSubresource(&keav1alpha1.KeaDhcp4Server{}).WithObjects(objs...)
	if funcs != nil {
		b = b.WithInterceptorFuncs(*funcs)
	}
	c := b.Build()
	return &KeaDhcp4ServerReconciler{Client: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(100)}, c
}

func reconcileDhcp4(r *KeaDhcp4ServerReconciler) error {
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: testDhcp4CR, Namespace: testNamespaceDefault},
	})
	return err
}

func getDhcp4(t *testing.T, c client.Client) *keav1alpha1.KeaDhcp4Server {
	t.Helper()
	s := &keav1alpha1.KeaDhcp4Server{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testDhcp4CR, Namespace: testNamespaceDefault}, s); err != nil {
		t.Fatalf("getting CR: %v", err)
	}
	return s
}

func conditionOf(s *keav1alpha1.KeaDhcp4Server, condType string) *keav1alpha1.ConditionStatus {
	for i := range s.Status.Conditions {
		if s.Status.Conditions[i].Type == condType {
			return &s.Status.Conditions[i]
		}
	}
	return nil
}

func haConfigMapCount(t *testing.T, c client.Client) int {
	t.Helper()
	list := &corev1.ConfigMapList{}
	if err := c.List(context.Background(), list, client.InNamespace(testNamespaceDefault)); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, cm := range list.Items {
		if strings.HasPrefix(cm.Name, testDhcp4CR+"-") {
			n++
		}
	}
	return n
}

func statefulSetHash(t *testing.T, c client.Client) (string, bool) {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	err := c.Get(context.Background(), types.NamespacedName{
		Name: resources.StatefulSetName(testDhcp4CR, testComponent4), Namespace: testNamespaceDefault}, sts)
	if apierrors.IsNotFound(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return sts.Spec.Template.Annotations[resources.ConfigHashAnnotation], true
}

// An HA spec that cannot be rendered must fail visibly and write nothing:
// before this was fixed the render error was only logged and the CR still
// reported ConfigurationValid=True with a StatefulSet built on missing configs.
func TestDhcp4HAInvalidSpecFailsBeforeAnyWrite(t *testing.T) {
	ttl, pct := int32(600), "0.5"
	invalidDDNS := haDhcp4Server(2)
	invalidDDNS.Spec.DDNSTTL = &ttl
	invalidDDNS.Spec.DDNSTTLPercent = ttlPercentJSON(pct)

	tests := []struct {
		name    string
		server  *keav1alpha1.KeaDhcp4Server
		wantErr string
	}{
		{"DDNS rejected by the renderer", invalidDDNS, "cannot specify both ddns-ttl-percent and ddns-ttl"},
		{"ordinal without a declared peer", haDhcp4Server(3), "ordinal 2 has no declared high-availability peer"},
		{"duplicate peer names", haDhcp4Server(2, testPeerPrimary, testPeerPrimary), `duplicate peer name "server1"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newDhcp4Reconciler(t, nil, tc.server)
			err := reconcileDhcp4(r)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Reconcile error = %v, want it to contain %q", err, tc.wantErr)
			}
			cond := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeConfigValid)
			if cond == nil || cond.Status != condFalse || !strings.Contains(cond.Message, tc.wantErr) {
				t.Errorf("ConfigurationValid = %+v, want False mentioning %q", cond, tc.wantErr)
			}
			if n := haConfigMapCount(t, c); n != 0 {
				t.Errorf("%d HA ConfigMaps written for an invalid spec, want 0", n)
			}
			if _, ok := statefulSetHash(t, c); ok {
				t.Error("StatefulSet created for an invalid spec")
			}
		})
	}
}

// A ConfigMap write failure must not advance the workload: the StatefulSet
// keeps the hash of the last complete configuration and the CR says the new
// configuration was not applied. Once the API recovers, one retry applies it
// and further reconciles change nothing.
func TestDhcp4HAConfigMapWriteFailureKeepsWorkload(t *testing.T) {
	funcs, fail := failingWrites(resources.HAConfigMapName(testDhcp4CR, testComponent4, 1))
	r, c := newDhcp4Reconciler(t, &funcs, haDhcp4Server(2))

	if err := reconcileDhcp4(r); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	applied, ok := statefulSetHash(t, c)
	if !ok || applied == "" {
		t.Fatal("initial reconcile did not create the StatefulSet with a config hash")
	}

	server := getDhcp4(t, c)
	life := testChangedLife
	server.Spec.ValidLifetime = &life
	if err := c.Update(context.Background(), server); err != nil {
		t.Fatal(err)
	}

	fail.Store(true)
	err := reconcileDhcp4(r)
	if err == nil || !strings.Contains(err.Error(), testFailMsgWrite) {
		t.Fatalf("Reconcile error = %v, want the injected write failure", err)
	}
	if got, _ := statefulSetHash(t, c); got != applied {
		t.Errorf("StatefulSet config hash moved to %q after a failed ConfigMap write, want %q", got, applied)
	}
	after := getDhcp4(t, c)
	if after.Status.ConfigHash != applied {
		t.Errorf("status.configHash = %q, want the applied %q", after.Status.ConfigHash, applied)
	}
	if d := conditionOf(after, keav1alpha1.ConditionTypeDegraded); d == nil || d.Status != condTrue || d.Reason != "ConfigApplyError" {
		t.Errorf("Degraded = %+v, want True/ConfigApplyError", d)
	}

	fail.Store(false)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatalf("reconcile after recovery: %v", err)
	}
	updated, _ := statefulSetHash(t, c)
	if updated == applied {
		t.Fatal("StatefulSet config hash unchanged after the configuration change was applied")
	}
	if d := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeDegraded); d == nil || d.Status != condFalse {
		t.Errorf("Degraded = %+v after recovery, want False", d)
	}

	if err := reconcileDhcp4(r); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	if again, _ := statefulSetHash(t, c); again != updated {
		t.Errorf("config hash changed on an unchanged reconcile: %q -> %q", updated, again)
	}
}
