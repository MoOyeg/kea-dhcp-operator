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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/kea"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// KeaControlAgentReconciler reconciles a KeaControlAgent object.
type KeaControlAgentReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	IsOpenShift bool
}

// +kubebuilder:rbac:groups=kea.openshift.io,resources=keacontrolagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keacontrolagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keacontrolagents/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile reconciles the KeaControlAgent custom resource.
func (r *KeaControlAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the CR.
	agent := &keav1alpha1.KeaControlAgent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("KeaControlAgent resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KeaControlAgent", "name", agent.Name)

	// 2. Build env var references for auth credentials.
	renderer := kea.NewCtrlAgentConfigRenderer(&agent.Spec)
	var secretEnvVars []corev1.EnvVar

	if agent.Spec.Authentication != nil && len(agent.Spec.Authentication.Clients) > 0 {
		placeholders := make(map[string]string, len(agent.Spec.Authentication.Clients))
		for i, c := range agent.Spec.Authentication.Clients {
			if c.PasswordSecretKeyRef != nil {
				envName := fmt.Sprintf("KEA_AUTH_PASSWORD_%d", i)
				ev, placeholder := secretKeyRefEnvVar(envName, c.PasswordSecretKeyRef)
				secretEnvVars = append(secretEnvVars, ev)
				placeholders[c.User] = placeholder
			}
		}
		renderer.ResolvedAuthPasswords = placeholders
	}

	configJSON, err := renderer.RenderJSON()
	if err != nil {
		setCondition(&agent.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "False", "RenderError", err.Error())
		agent.Status.Phase = "Error"
		if statusErr := r.Status().Update(ctx, agent); statusErr != nil {
			logger.Error(statusErr, "failed to update status after config render error")
		}
		r.Recorder.Eventf(agent, corev1.EventTypeWarning, "ConfigRenderError", "Failed to render configuration: %v", err)
		return ctrl.Result{}, fmt.Errorf("rendering config: %w", err)
	}

	// 3. Compute config hash.
	configHash := kea.ComputeHash(configJSON)

	setCondition(&agent.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "True", "ConfigRendered", "Configuration rendered successfully")

	component := "ctrl-agent"
	labels := resources.CommonLabels(agent.Name, component)

	// 4. Reconcile ConfigMap.
	cm := resources.BuildConfigMap(agent.Namespace, agent.Name, component, configJSON, labels)
	if err := reconcileResource(ctx, r.Client, r.Scheme, agent, cm); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ConfigMap: %w", err)
	}

	// 5. Reconcile ServiceAccount.
	sa := resources.BuildServiceAccount(agent.Namespace, agent.Name, labels)
	if err := reconcileResource(ctx, r.Client, r.Scheme, agent, sa); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ServiceAccount: %w", err)
	}

	// Auto-bind the kea-dhcp SCC to the service account in the default namespace.
	if r.IsOpenShift && agent.Namespace == DefaultNamespace {
		if err := ensureSCCRoleBinding(ctx, r.Client, agent.Namespace, agent.Name); err != nil {
			logger.Error(err, "failed to ensure SCC RoleBinding")
		}
	}

	// 6. Build and reconcile Deployment.
	replicas := agent.Spec.Replicas
	if replicas == nil {
		one := int32(1)
		replicas = &one
	}

	image := agent.Spec.Container.Image

	var nodeSelector map[string]string
	var tolerations []corev1.Toleration
	var affinity *corev1.Affinity
	var podAnnotations map[string]string
	if agent.Spec.Placement != nil {
		nodeSelector = agent.Spec.Placement.NodeSelector
		tolerations = agent.Spec.Placement.Tolerations
		affinity = agent.Spec.Placement.Affinity
		podAnnotations = agent.Spec.Placement.PodAnnotations
	}

	var tlsSecretName string
	if agent.Spec.TLS != nil {
		tlsSecretName = agent.Spec.TLS.SecretRef.Name
	}

	dp := resources.DeploymentParams{
		Namespace:          agent.Namespace,
		CRName:             agent.Name,
		Component:          component,
		Command:            resources.DefaultCommandForComponent(component),
		ConfigFileName:     resources.ConfigFileName(component),
		ConfigMapName:      resources.ConfigMapName(agent.Name, component),
		Image:              image,
		ImagePullPolicy:    agent.Spec.Container.ImagePullPolicy,
		Replicas:           replicas,
		Resources:          agent.Spec.Container.Resources,
		NodeSelector:       nodeSelector,
		Tolerations:        tolerations,
		Affinity:           affinity,
		TLSSecretName:      tlsSecretName,
		ServiceAccountName: resources.ServiceAccountName(agent.Name),
		ConfigHash:         configHash,
		ImagePullSecrets:   agent.Spec.Container.ImagePullSecrets,
		PodAnnotations:     podAnnotations,
		SecretEnvVars:      secretEnvVars,
	}
	deploy := resources.BuildDeployment(dp)
	if err := reconcileResource(ctx, r.Client, r.Scheme, agent, deploy); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Deployment: %w", err)
	}

	// 7. Always create a Service for the Control Agent HTTP endpoint.
	httpPort := int32(8000)
	if agent.Spec.HTTPPort != nil {
		httpPort = *agent.Spec.HTTPPort
	}
	svc := resources.BuildService(resources.ServiceParams{
		Namespace: agent.Namespace,
		CRName:    agent.Name,
		Component: component,
		Port:      httpPort,
		Protocol:  corev1.ProtocolTCP,
	})
	if err := reconcileResource(ctx, r.Client, r.Scheme, agent, svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Service: %w", err)
	}

	// 8. Update status.
	currentDeploy := &appsv1.Deployment{}
	var readyReplicas int32
	deployKey := types.NamespacedName{
		Name:      resources.DeploymentName(agent.Name, component),
		Namespace: agent.Namespace,
	}
	if err := r.Get(ctx, deployKey, currentDeploy); err == nil {
		readyReplicas = currentDeploy.Status.ReadyReplicas
	}

	agent.Status.ReadyReplicas = readyReplicas
	agent.Status.ConfigHash = configHash
	agent.Status.ConfigMapRef = resources.ConfigMapName(agent.Name, component)
	agent.Status.ObservedGeneration = agent.Generation

	if readyReplicas > 0 && readyReplicas == *replicas {
		agent.Status.Phase = "Running"
		setCondition(&agent.Status.Conditions, keav1alpha1.ConditionTypeReady, "True", "DeploymentReady", "All replicas are ready")
		setCondition(&agent.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "False", "DeploymentComplete", "Deployment is complete")
	} else {
		agent.Status.Phase = "Progressing"
		setCondition(&agent.Status.Conditions, keav1alpha1.ConditionTypeReady, "False", "DeploymentProgressing",
			fmt.Sprintf("%d/%d replicas ready", readyReplicas, *replicas))
		setCondition(&agent.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "True", "DeploymentProgressing", "Waiting for replicas")
	}

	if err := r.Status().Update(ctx, agent); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}

	r.Recorder.Event(agent, corev1.EventTypeNormal, "Reconciled", "Successfully reconciled KeaControlAgent")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeaControlAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keav1alpha1.KeaControlAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Complete(r)
}
