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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

var _ = Describe("KeaControlAgent Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-kca"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespaceDefault, // TODO(user):Modify as needed
		}
		keacontrolagent := &keav1alpha1.KeaControlAgent{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KeaControlAgent")
			err := k8sClient.Get(ctx, typeNamespacedName, keacontrolagent)
			if err != nil && errors.IsNotFound(err) {
				resource := &keav1alpha1.KeaControlAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespaceDefault,
					},
					Spec: keav1alpha1.KeaControlAgentSpec{
						Container: keav1alpha1.ContainerConfig{Image: testCtrlAgentImage},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &keav1alpha1.KeaControlAgent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KeaControlAgent")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KeaControlAgentReconciler{
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

// The CRD has no rule for the image or the socket type: a CRD rule would
// also reject the operator's status writes to objects stored before it
// existed (Kubernetes before 1.33). The API server stores such an object and
// the operator reports why it cannot run, without deploying it.
var _ = Describe("KeaControlAgent that cannot run", func() {
	ctx := context.Background()
	port := int32(8001)

	It("is stored, reported and not deployed", func() {
		for name, spec := range map[string]keav1alpha1.KeaControlAgentSpec{
			"kca-no-image": {},
			"kca-http": {
				Container: keav1alpha1.ContainerConfig{Image: testCtrlAgentImage},
				ControlSockets: &keav1alpha1.AgentControlSockets{
					D2: &keav1alpha1.ControlSocket{SocketType: testSocketTypeHTTP, SocketPort: &port}},
			},
		} {
			kca := &keav1alpha1.KeaControlAgent{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespaceDefault}, Spec: spec}
			Expect(k8sClient.Create(ctx, kca)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, kca)).To(Succeed()) })

			r := &KeaControlAgentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(10)}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(kca)})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kca), kca)).To(Succeed())
			cv := findCondition(kca.Status.Conditions, keav1alpha1.ConditionTypeConfigValid)
			Expect(cv).NotTo(BeNil())
			Expect(cv.Status).To(Equal(condFalse))
			Expect(cv.Message).To(ContainSubstring("docs/control-agent.md"))
			err = k8sClient.Get(ctx, types.NamespacedName{Name: name + "-ctrl-agent", Namespace: testNamespaceDefault}, &appsv1.Deployment{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "Deployment for %s: err = %v", name, err)
		}
	})
})
