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
	"os"
	"path/filepath"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// v0.0.36 defaulted the Control Agent image to one ISC never published.
const legacyCtrlAgentImage = "docker.cloudsmith.io/isc/docker/kea-ctrl-agent:3.0.2"

// The upgrade path: objects stored under the v0.0.36 CRDs (which accepted a
// Control Agent with no image) must still get status from the new operator
// once the new CRDs are installed, must recover when fixed, and must not warn
// on every reconcile. The CRDs therefore carry no rule for the image or socket
// type: on Kubernetes before 1.33 such a rule also rejects the operator's status
// writes to these objects. Run this on the oldest supported Kubernetes to
// catch a rule coming back. The v0.0.36 CRDs are testdata/v0.0.36-crds, copied from the
// release commit on main.
var _ = Describe("KeaControlAgent upgrade from v0.0.36", Ordered, func() {
	ctx := context.Background()
	var crdClient client.Client
	port := int32(8003)

	// installCRDs replaces the served schema of the KeaControlAgent and
	// KeaServer CRDs with the ones in dir.
	installCRDs := func(dir string) {
		for _, plural := range []string{"keacontrolagents", "keaservers"} {
			data, err := os.ReadFile(filepath.Join(dir, "kea.openshift.io_"+plural+".yaml"))
			Expect(err).NotTo(HaveOccurred())
			want := &apiextensionsv1.CustomResourceDefinition{}
			Expect(yaml.Unmarshal(data, want)).To(Succeed())
			Eventually(func() error {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				if err := crdClient.Get(ctx, types.NamespacedName{Name: want.Name}, crd); err != nil {
					return err
				}
				crd.Spec = want.Spec
				return crdClient.Update(ctx, crd)
			}).Should(Succeed())
		}
	}
	// servesDeprecation reports whether the API server answers a
	// KeaControlAgent request with this release's deprecation warning, that
	// is, whether it serves the new CRD rather than v0.0.36's.
	servesDeprecation := func() bool {
		var w warningCollector
		c := rest.CopyConfig(cfg)
		c.WarningHandler = &w
		dyn, err := dynamic.NewForConfig(c)
		Expect(err).NotTo(HaveOccurred())
		probe := &unstructured.Unstructured{}
		probe.SetGroupVersionKind(keav1alpha1.GroupVersion.WithKind("KeaControlAgent"))
		probe.SetName("kca-schema-probe")
		probe.SetNamespace(testNamespaceDefault)
		_, err = dyn.Resource(keav1alpha1.GroupVersion.WithResource("keacontrolagents")).Namespace(testNamespaceDefault).
			Create(ctx, probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).NotTo(HaveOccurred())
		return w.contains("KeaControlAgent is deprecated")
	}
	newCRDs := filepath.Join("..", "..", "config", "crd", "bases")
	legacyCRDs := filepath.Join("testdata", "v0.0.36-crds")

	BeforeAll(func() {
		s := runtime.NewScheme()
		Expect(apiextensionsv1.AddToScheme(s)).To(Succeed())
		var err error
		crdClient, err = client.New(cfg, client.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())

		By("serving the v0.0.36 CRDs")
		installCRDs(legacyCRDs)
		Eventually(servesDeprecation).Should(BeFalse())
		DeferCleanup(func() {
			installCRDs(newCRDs)
			Eventually(servesDeprecation).Should(BeTrue())
		})
	})

	warnings := func(r *record.FakeRecorder) []string {
		var out []string
		for {
			select {
			case e := <-r.Events:
				if strings.HasPrefix(e, corev1.EventTypeWarning) {
					out = append(out, e)
				}
			default:
				return out
			}
		}
	}

	It("reports, recovers and warns once for a KeaControlAgent stored by v0.0.36", func() {
		name := "kca-legacy"
		key := types.NamespacedName{Name: name, Namespace: testNamespaceDefault}

		By("storing a KeaControlAgent with no image and an HTTP socket under the v0.0.36 CRD")
		agent := &keav1alpha1.KeaControlAgent{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespaceDefault},
			Spec: keav1alpha1.KeaControlAgentSpec{ControlSockets: &keav1alpha1.AgentControlSockets{
				D2: &keav1alpha1.ControlSocket{SocketType: testSocketTypeHTTP, SocketPort: &port},
			}},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, agent))).To(Succeed()) })

		By("storing the workload v0.0.36 created for it, with the default image")
		labels := resources.CommonLabels(name, "ctrl-agent")
		workload := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-ctrl-agent", Namespace: testNamespaceDefault},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name: "kea-ctrl-agent", Image: legacyCtrlAgentImage}}},
				},
			},
		}
		Expect(controllerutil.SetControllerReference(agent, workload, scheme.Scheme)).To(Succeed())
		Expect(k8sClient.Create(ctx, workload)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(workload), workload)).To(Succeed())
		workloadVersion := workload.ResourceVersion

		By("upgrading to this release's CRDs")
		installCRDs(newCRDs)
		Eventually(servesDeprecation).Should(BeTrue())

		recorder := record.NewFakeRecorder(100)
		r := &KeaControlAgentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}
		reconcileN := func(n int) {
			for range n {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
				Expect(err).NotTo(HaveOccurred())
			}
		}
		expectStatus := func(status, reason string, phase string) {
			got := &keav1alpha1.KeaControlAgent{}
			Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
			cv := findCondition(got.Status.Conditions, keav1alpha1.ConditionTypeConfigValid)
			Expect(cv).NotTo(BeNil())
			Expect(cv.Status).To(Equal(status))
			Expect(cv.Reason).To(Equal(reason))
			Expect(got.Status.Phase).To(Equal(phase))
			d := findCondition(got.Status.Conditions, keav1alpha1.ConditionTypeDeprecated)
			Expect(d).NotTo(BeNil())
			Expect(d.Status).To(Equal(string(metav1.ConditionTrue)))
		}

		By("reconciling three times: the status is written, the workload is left alone, one warning")
		reconcileN(3)
		expectStatus(condFalse, "ImageRequired", keav1alpha1.PhaseError)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(workload), workload)).To(Succeed())
		Expect(workload.ResourceVersion).To(Equal(workloadVersion))
		Expect(workload.Spec.Template.Spec.Containers[0].Image).To(Equal(legacyCtrlAgentImage))
		w := warnings(recorder)
		Expect(w).To(HaveLen(1))
		Expect(w[0]).To(ContainSubstring("ImageRequired"))

		By("setting an image: the unchanged HTTP socket still blocks it, with one new warning")
		Expect(k8sClient.Get(ctx, key, agent)).To(Succeed())
		agent.Spec.Container.Image = testCtrlAgentImage
		Expect(k8sClient.Update(ctx, agent)).To(Succeed())
		reconcileN(3)
		expectStatus(condFalse, "UnsupportedControlSocket", keav1alpha1.PhaseError)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(workload), workload)).To(Succeed())
		Expect(workload.ResourceVersion).To(Equal(workloadVersion))
		w = warnings(recorder)
		Expect(w).To(HaveLen(1))
		Expect(w[0]).To(ContainSubstring("UnsupportedControlSocket"))

		By("switching the socket to UNIX: reconciliation resumes and Deprecated stays")
		Expect(k8sClient.Get(ctx, key, agent)).To(Succeed())
		agent.Spec.ControlSockets.D2 = &keav1alpha1.ControlSocket{SocketType: "unix", SocketName: "/var/run/kea/kea-ddns-ctrl-socket"}
		Expect(k8sClient.Update(ctx, agent)).To(Succeed())
		reconcileN(2)
		expectStatus(condTrue, "ConfigRendered", keav1alpha1.PhaseProgressing)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(workload), workload)).To(Succeed())
		Expect(workload.Spec.Template.Spec.Containers[0].Image).To(Equal(testCtrlAgentImage))
		Expect(warnings(recorder)).To(BeEmpty())
	})

	It("warns once for a KeaServer stored by v0.0.36 with an imageless controlAgent", func() {
		name := "ks-legacy"
		key := types.NamespacedName{Name: name, Namespace: testNamespaceDefault}

		By("storing the KeaServer and the child v0.0.36 created, under the v0.0.36 CRDs")
		installCRDs(legacyCRDs)
		Eventually(servesDeprecation).Should(BeFalse())
		server := &keav1alpha1.KeaServer{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespaceDefault},
			Spec:       keav1alpha1.KeaServerSpec{ControlAgent: &keav1alpha1.KeaControlAgentSpec{}},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, server))).To(Succeed()) })
		child := &keav1alpha1.KeaControlAgent{ObjectMeta: metav1.ObjectMeta{Name: name + "-ctrl-agent", Namespace: testNamespaceDefault}}
		Expect(controllerutil.SetControllerReference(server, child, scheme.Scheme)).To(Succeed())
		Expect(k8sClient.Create(ctx, child)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, child))).To(Succeed()) })

		By("upgrading to this release's CRDs and reconciling three times")
		installCRDs(newCRDs)
		Eventually(servesDeprecation).Should(BeTrue())
		recorder := record.NewFakeRecorder(100)
		r := &KeaServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: recorder}
		for range 3 {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}
		got := &keav1alpha1.KeaServer{}
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		d := findCondition(got.Status.Conditions, keav1alpha1.ConditionTypeDeprecated)
		Expect(d).NotTo(BeNil())
		Expect(d.Reason).To(Equal(reasonControlAgentDeprecated))
		w := warnings(recorder)
		Expect(w).To(HaveLen(1))
		Expect(w[0]).To(ContainSubstring(reasonControlAgentDeprecated))

		By("removing spec.controlAgent: the condition and the child go")
		Expect(k8sClient.Get(ctx, key, server)).To(Succeed())
		server.Spec.ControlAgent = nil
		Expect(k8sClient.Update(ctx, server)).To(Succeed())
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		Expect(findCondition(got.Status.Conditions, keav1alpha1.ConditionTypeDeprecated)).To(BeNil())
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(child), &keav1alpha1.KeaControlAgent{})
		Expect(errors.IsNotFound(err)).To(BeTrue(), "child KeaControlAgent: err = %v", err)
	})
})

// warningCollector records the API server's warning headers.
type warningCollector struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warningCollector) HandleWarningHeader(_ int, _ string, msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, msg)
}

func (w *warningCollector) contains(sub string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, m := range w.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}
