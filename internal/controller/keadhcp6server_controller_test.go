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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

var _ = Describe("KeaDhcp6Server Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-kd6"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespaceDefault, // TODO(user):Modify as needed
		}
		keadhcp6server := &keav1alpha1.KeaDhcp6Server{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KeaDhcp6Server")
			err := k8sClient.Get(ctx, typeNamespacedName, keadhcp6server)
			if err != nil && errors.IsNotFound(err) {
				resource := &keav1alpha1.KeaDhcp6Server{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespaceDefault,
					},
					Spec: keav1alpha1.KeaDhcp6ServerSpec{
						InterfacesConfig: keav1alpha1.InterfacesConfig{
							Interfaces: []string{"net1"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &keav1alpha1.KeaDhcp6Server{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KeaDhcp6Server")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KeaDhcp6ServerReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

var _ = Describe("KeaDhcp6Server interface validation", func() {
	ctx := context.Background()

	newServer := func(name string, interfaces []string) *keav1alpha1.KeaDhcp6Server {
		return &keav1alpha1.KeaDhcp6Server{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespaceDefault},
			Spec: keav1alpha1.KeaDhcp6ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: interfaces},
			},
		}
	}

	It("accepts IPv6 interface/address pairs and hyphenated interface names", func() {
		resource := newServer("kd6-iface-ok", []string{
			"br-ex",
			"net1/2001:db8::1",
			"ens1f0.500/fe80::1",
			"net1-vlan/2001:db8:0:1::53",
			"eth0/192.0.2.1",
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("rejects interface names with shell metacharacters or whitespace", func() {
		for _, bad := range []string{"net1;rm -rf /", "eth0 x", "eth0$(id)", "eth0,eth1"} {
			err := k8sClient.Create(ctx, newServer("kd6-iface-bad", []string{bad}))
			Expect(err).To(HaveOccurred(), "interface %q should be rejected", bad)
			Expect(err.Error()).To(ContainSubstring("should match"), "interface %q", bad)
		}
	})
})
