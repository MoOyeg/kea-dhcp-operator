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

// KeaDhcpDdnsReconciler reconciles a KeaDhcpDdns object.
type KeaDhcpDdnsReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	IsOpenShift bool
}

// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcpddns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcpddns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kea.openshift.io,resources=keadhcpddns/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile reconciles the KeaDhcpDdns custom resource.
func (r *KeaDhcpDdnsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the CR.
	ddns := &keav1alpha1.KeaDhcpDdns{}
	if err := r.Get(ctx, req.NamespacedName, ddns); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("KeaDhcpDdns resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KeaDhcpDdns", "name", ddns.Name)

	// 2. Build env var references for TSIG secrets.
	renderer := kea.NewDdnsConfigRenderer(&ddns.Spec)
	var secretEnvVars []corev1.EnvVar

	if len(ddns.Spec.TSIGKeys) > 0 {
		placeholders := make(map[string]string, len(ddns.Spec.TSIGKeys))
		for i, k := range ddns.Spec.TSIGKeys {
			if k.SecretRef != nil {
				envName := fmt.Sprintf("KEA_TSIG_SECRET_%d", i)
				ev, placeholder := secretKeyRefEnvVar(envName, k.SecretRef)
				secretEnvVars = append(secretEnvVars, ev)
				placeholders[k.Name] = placeholder
			}
		}
		renderer.ResolvedTSIGSecrets = placeholders
	}

	configJSON, err := renderer.RenderJSON()
	if err != nil {
		setCondition(&ddns.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "False", "RenderError", err.Error())
		ddns.Status.Phase = "Error"
		if statusErr := r.Status().Update(ctx, ddns); statusErr != nil {
			logger.Error(statusErr, "failed to update status after config render error")
		}
		r.Recorder.Eventf(ddns, corev1.EventTypeWarning, "ConfigRenderError", "Failed to render configuration: %v", err)
		return ctrl.Result{}, fmt.Errorf("rendering config: %w", err)
	}

	// 3. Compute config hash.
	configHash := kea.ComputeHash(configJSON)

	setCondition(&ddns.Status.Conditions, keav1alpha1.ConditionTypeConfigValid, "True", "ConfigRendered", "Configuration rendered successfully")

	component := "ddns"
	labels := resources.CommonLabels(ddns.Name, component)

	// 4. Reconcile ConfigMap.
	cm := resources.BuildConfigMap(ddns.Namespace, ddns.Name, component, configJSON, labels)
	if err := reconcileResource(ctx, r.Client, r.Scheme, ddns, cm); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ConfigMap: %w", err)
	}

	// 5. Reconcile ServiceAccount.
	sa := resources.BuildServiceAccount(ddns.Namespace, ddns.Name, labels)
	if err := reconcileResource(ctx, r.Client, r.Scheme, ddns, sa); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ServiceAccount: %w", err)
	}

	// Auto-bind the kea-dhcp SCC to the service account in the default namespace.
	if r.IsOpenShift && ddns.Namespace == DefaultNamespace {
		if err := ensureSCCRoleBinding(ctx, r.Client, ddns.Namespace, ddns.Name); err != nil {
			logger.Error(err, "failed to ensure SCC RoleBinding")
		}
	}

	// 6. Build and reconcile Deployment.
	replicas := ddns.Spec.Replicas
	if replicas == nil {
		one := int32(1)
		replicas = &one
	}

	image := ddns.Spec.Container.Image

	var nodeSelector map[string]string
	var tolerations []corev1.Toleration
	var affinity *corev1.Affinity
	var podAnnotations map[string]string
	if ddns.Spec.Placement != nil {
		nodeSelector = ddns.Spec.Placement.NodeSelector
		tolerations = ddns.Spec.Placement.Tolerations
		affinity = ddns.Spec.Placement.Affinity
		podAnnotations = ddns.Spec.Placement.PodAnnotations
	}

	dp := resources.DeploymentParams{
		Namespace:          ddns.Namespace,
		CRName:             ddns.Name,
		Component:          component,
		Command:            resources.DefaultCommandForComponent(component),
		ConfigFileName:     resources.ConfigFileName(component),
		ConfigMapName:      resources.ConfigMapName(ddns.Name, component),
		Image:              image,
		ImagePullPolicy:    ddns.Spec.Container.ImagePullPolicy,
		Replicas:           replicas,
		Resources:          ddns.Spec.Container.Resources,
		NodeSelector:       nodeSelector,
		Tolerations:        tolerations,
		Affinity:           affinity,
		ServiceAccountName: resources.ServiceAccountName(ddns.Name),
		ConfigHash:         configHash,
		ImagePullSecrets:   ddns.Spec.Container.ImagePullSecrets,
		PodAnnotations:     podAnnotations,
		SecretEnvVars:      secretEnvVars,
	}
	deploy := resources.BuildDeployment(dp)
	if err := reconcileResource(ctx, r.Client, r.Scheme, ddns, deploy); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Deployment: %w", err)
	}

	// 7. Create Service only if port is defined.
	if ddns.Spec.Port != nil {
		svc := resources.BuildService(resources.ServiceParams{
			Namespace: ddns.Namespace,
			CRName:    ddns.Name,
			Component: component,
			Port:      *ddns.Spec.Port,
			Protocol:  corev1.ProtocolUDP,
		})
		if err := reconcileResource(ctx, r.Client, r.Scheme, ddns, svc); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling Service: %w", err)
		}
	}

	// 8. Update status.
	currentDeploy := &appsv1.Deployment{}
	var readyReplicas int32
	deployKey := types.NamespacedName{
		Name:      resources.DeploymentName(ddns.Name, component),
		Namespace: ddns.Namespace,
	}
	if err := r.Get(ctx, deployKey, currentDeploy); err == nil {
		readyReplicas = currentDeploy.Status.ReadyReplicas
	}

	ddns.Status.ReadyReplicas = readyReplicas
	ddns.Status.ConfigHash = configHash
	ddns.Status.ConfigMapRef = resources.ConfigMapName(ddns.Name, component)
	ddns.Status.ObservedGeneration = ddns.Generation

	if readyReplicas > 0 && readyReplicas == *replicas {
		ddns.Status.Phase = "Running"
		setCondition(&ddns.Status.Conditions, keav1alpha1.ConditionTypeReady, "True", "DeploymentReady", "All replicas are ready")
		setCondition(&ddns.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "False", "DeploymentComplete", "Deployment is complete")
	} else {
		ddns.Status.Phase = "Progressing"
		setCondition(&ddns.Status.Conditions, keav1alpha1.ConditionTypeReady, "False", "DeploymentProgressing",
			fmt.Sprintf("%d/%d replicas ready", readyReplicas, *replicas))
		setCondition(&ddns.Status.Conditions, keav1alpha1.ConditionTypeProgressing, "True", "DeploymentProgressing", "Waiting for replicas")
	}

	if err := r.Status().Update(ctx, ddns); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}

	r.Recorder.Event(ddns, corev1.EventTypeNormal, "Reconciled", "Successfully reconciled KeaDhcpDdns")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeaDhcpDdnsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keav1alpha1.KeaDhcpDdns{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Complete(r)
}
