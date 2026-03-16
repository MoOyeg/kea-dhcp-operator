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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// KeaServerReconciler reconciles a KeaServer object.
// It acts as an umbrella controller that creates and manages child CRDs
// (KeaDhcp4Server, KeaDhcp6Server, KeaControlAgent, KeaDhcpDdns) based
// on the KeaServer spec.
type KeaServerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	IsOpenShift bool
	Recorder    record.EventRecorder
}

// +kubebuilder:rbac:groups=kea.openshift.io,resources=keaservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keaservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keaservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp4servers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcp6servers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keacontrolagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcpddns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keastorkservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile reconciles the KeaServer umbrella custom resource.
func (r *KeaServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the KeaServer CR.
	server := &keav1alpha1.KeaServer{}
	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("KeaServer resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KeaServer", "name", server.Name)

	// 2. Reconcile each component child CR.
	// DHCPv4
	if err := r.reconcileDhcp4(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling DHCPv4 child: %w", err)
	}

	// DHCPv6
	if err := r.reconcileDhcp6(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling DHCPv6 child: %w", err)
	}

	// Control Agent
	if err := r.reconcileControlAgent(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ControlAgent child: %w", err)
	}

	// DHCP-DDNS
	if err := r.reconcileDhcpDdns(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling DHCP-DDNS child: %w", err)
	}

	// Stork Server
	if err := r.reconcileStorkServer(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling StorkServer child: %w", err)
	}

	// 3. Aggregate status from child CRs.
	server.Status.Dhcp4Ready = r.isChildReady(ctx, server.Namespace, server.Name+"-dhcp4", &keav1alpha1.KeaDhcp4Server{})
	server.Status.Dhcp6Ready = r.isChildReady(ctx, server.Namespace, server.Name+"-dhcp6", &keav1alpha1.KeaDhcp6Server{})
	server.Status.ControlAgentReady = r.isChildReady(ctx, server.Namespace, server.Name+"-ctrl-agent", &keav1alpha1.KeaControlAgent{})
	server.Status.DhcpDdnsReady = r.isChildReady(ctx, server.Namespace, server.Name+"-ddns", &keav1alpha1.KeaDhcpDdns{})
	server.Status.StorkServerReady = r.isChildReady(ctx, server.Namespace, server.Name+"-stork-server", &keav1alpha1.KeaStorkServer{})

	server.Status.ObservedGeneration = server.Generation

	// Determine overall phase.
	allReady := true
	anyConfigured := false
	if server.Spec.Dhcp4 != nil {
		anyConfigured = true
		if !server.Status.Dhcp4Ready {
			allReady = false
		}
	}
	if server.Spec.Dhcp6 != nil {
		anyConfigured = true
		if !server.Status.Dhcp6Ready {
			allReady = false
		}
	}
	if server.Spec.ControlAgent != nil {
		anyConfigured = true
		if !server.Status.ControlAgentReady {
			allReady = false
		}
	}
	if server.Spec.DhcpDdns != nil {
		anyConfigured = true
		if !server.Status.DhcpDdnsReady {
			allReady = false
		}
	}
	if server.Spec.StorkServer != nil {
		anyConfigured = true
		if !server.Status.StorkServerReady {
			allReady = false
		}
	}

	if !anyConfigured {
		server.Status.Phase = "Pending"
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "False", "NoComponents", "No components configured")
	} else if allReady {
		server.Status.Phase = "Running"
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "True", "AllComponentsReady", "All configured components are ready")
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "False", "DeploymentComplete", "All components deployed")
	} else {
		server.Status.Phase = "Progressing"
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeReady, "False", "ComponentsProgressing", "Some components are not yet ready")
		setCondition(&server.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "True", "ComponentsProgressing", "Waiting for components")
	}

	if err := r.Status().Update(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}

	r.Recorder.Event(server, corev1.EventTypeNormal, "Reconciled", "Successfully reconciled KeaServer")

	return ctrl.Result{}, nil
}

// reconcileDhcp4 creates, updates, or deletes the KeaDhcp4Server child CR.
func (r *KeaServerReconciler) reconcileDhcp4(ctx context.Context, server *keav1alpha1.KeaServer) error {
	childName := server.Name + "-dhcp4"
	existing := &keav1alpha1.KeaDhcp4Server{}
	err := r.Get(ctx, types.NamespacedName{Name: childName, Namespace: server.Namespace}, existing)

	if server.Spec.Dhcp4 == nil {
		// Delete child if it exists.
		if err == nil {
			return r.Delete(ctx, existing)
		}
		return client.IgnoreNotFound(err)
	}

	// Create or update child.
	child := &keav1alpha1.KeaDhcp4Server{
		ObjectMeta: metav1.ObjectMeta{
			Name:      childName,
			Namespace: server.Namespace,
		},
	}
	_, opErr := controllerutil.CreateOrUpdate(ctx, r.Client, child, func() error {
		child.Spec = *server.Spec.Dhcp4
		return controllerutil.SetControllerReference(server, child, r.Scheme)
	})
	return opErr
}

// reconcileDhcp6 creates, updates, or deletes the KeaDhcp6Server child CR.
func (r *KeaServerReconciler) reconcileDhcp6(ctx context.Context, server *keav1alpha1.KeaServer) error {
	childName := server.Name + "-dhcp6"
	existing := &keav1alpha1.KeaDhcp6Server{}
	err := r.Get(ctx, types.NamespacedName{Name: childName, Namespace: server.Namespace}, existing)

	if server.Spec.Dhcp6 == nil {
		if err == nil {
			return r.Delete(ctx, existing)
		}
		return client.IgnoreNotFound(err)
	}

	child := &keav1alpha1.KeaDhcp6Server{
		ObjectMeta: metav1.ObjectMeta{
			Name:      childName,
			Namespace: server.Namespace,
		},
	}
	_, opErr := controllerutil.CreateOrUpdate(ctx, r.Client, child, func() error {
		child.Spec = *server.Spec.Dhcp6
		return controllerutil.SetControllerReference(server, child, r.Scheme)
	})
	return opErr
}

// reconcileControlAgent creates, updates, or deletes the KeaControlAgent child CR.
func (r *KeaServerReconciler) reconcileControlAgent(ctx context.Context, server *keav1alpha1.KeaServer) error {
	childName := server.Name + "-ctrl-agent"
	existing := &keav1alpha1.KeaControlAgent{}
	err := r.Get(ctx, types.NamespacedName{Name: childName, Namespace: server.Namespace}, existing)

	if server.Spec.ControlAgent == nil {
		if err == nil {
			return r.Delete(ctx, existing)
		}
		return client.IgnoreNotFound(err)
	}

	child := &keav1alpha1.KeaControlAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      childName,
			Namespace: server.Namespace,
		},
	}
	_, opErr := controllerutil.CreateOrUpdate(ctx, r.Client, child, func() error {
		child.Spec = *server.Spec.ControlAgent
		return controllerutil.SetControllerReference(server, child, r.Scheme)
	})
	return opErr
}

// reconcileDhcpDdns creates, updates, or deletes the KeaDhcpDdns child CR.
func (r *KeaServerReconciler) reconcileDhcpDdns(ctx context.Context, server *keav1alpha1.KeaServer) error {
	childName := server.Name + "-ddns"
	existing := &keav1alpha1.KeaDhcpDdns{}
	err := r.Get(ctx, types.NamespacedName{Name: childName, Namespace: server.Namespace}, existing)

	if server.Spec.DhcpDdns == nil {
		if err == nil {
			return r.Delete(ctx, existing)
		}
		return client.IgnoreNotFound(err)
	}

	child := &keav1alpha1.KeaDhcpDdns{
		ObjectMeta: metav1.ObjectMeta{
			Name:      childName,
			Namespace: server.Namespace,
		},
	}
	_, opErr := controllerutil.CreateOrUpdate(ctx, r.Client, child, func() error {
		child.Spec = *server.Spec.DhcpDdns
		return controllerutil.SetControllerReference(server, child, r.Scheme)
	})
	return opErr
}

// reconcileStorkServer creates, updates, or deletes the KeaStorkServer child CR.
func (r *KeaServerReconciler) reconcileStorkServer(ctx context.Context, server *keav1alpha1.KeaServer) error {
	childName := server.Name + "-stork-server"
	existing := &keav1alpha1.KeaStorkServer{}
	err := r.Get(ctx, types.NamespacedName{Name: childName, Namespace: server.Namespace}, existing)

	if server.Spec.StorkServer == nil {
		if err == nil {
			return r.Delete(ctx, existing)
		}
		return client.IgnoreNotFound(err)
	}

	child := &keav1alpha1.KeaStorkServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      childName,
			Namespace: server.Namespace,
		},
	}
	_, opErr := controllerutil.CreateOrUpdate(ctx, r.Client, child, func() error {
		child.Spec = *server.Spec.StorkServer
		return controllerutil.SetControllerReference(server, child, r.Scheme)
	})
	return opErr
}

// isChildReady checks if a child CR is ready by examining both its Phase
// and its Ready condition. A child is considered ready when its Phase is
// "Running" and it has a Ready condition with status "True".
func (r *KeaServerReconciler) isChildReady(ctx context.Context, namespace, name string, obj client.Object) bool {
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Get(ctx, key, obj); err != nil {
		return false
	}

	var phase string
	var conditions []keav1alpha1.ConditionStatus

	switch child := obj.(type) {
	case *keav1alpha1.KeaDhcp4Server:
		phase = child.Status.Phase
		conditions = child.Status.Conditions
	case *keav1alpha1.KeaDhcp6Server:
		phase = child.Status.Phase
		conditions = child.Status.Conditions
	case *keav1alpha1.KeaControlAgent:
		phase = child.Status.Phase
		conditions = child.Status.Conditions
	case *keav1alpha1.KeaDhcpDdns:
		phase = child.Status.Phase
		conditions = child.Status.Conditions
	case *keav1alpha1.KeaStorkServer:
		phase = child.Status.Phase
		conditions = child.Status.Conditions
	default:
		return false
	}

	if phase != "Running" {
		return false
	}

	for _, c := range conditions {
		if c.Type == keav1alpha1.ConditionTypeReady {
			return c.Status == "True"
		}
	}

	// Phase is Running but no Ready condition yet — treat as ready
	// for backward compatibility with controllers that haven't set conditions.
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeaServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keav1alpha1.KeaServer{}).
		Owns(&keav1alpha1.KeaDhcp4Server{}).
		Owns(&keav1alpha1.KeaDhcp6Server{}).
		Owns(&keav1alpha1.KeaControlAgent{}).
		Owns(&keav1alpha1.KeaDhcpDdns{}).
		Owns(&keav1alpha1.KeaStorkServer{}).
		Complete(r)
}
