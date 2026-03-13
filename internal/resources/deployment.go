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

package resources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Default per-component images for Kea deployments.
const (
	DefaultKeaDhcp4Image     = "docker.cloudsmith.io/isc/docker/kea-dhcp4:3.0.2"
	DefaultKeaDhcp6Image     = "docker.cloudsmith.io/isc/docker/kea-dhcp6:3.0.2"
	DefaultKeaCtrlAgentImage = "docker.cloudsmith.io/isc/docker/kea-ctrl-agent:3.0.2"
	DefaultKeaDhcpDdnsImage  = "docker.cloudsmith.io/isc/docker/kea-dhcp-ddns:3.0.2"
)

// Volume/mount constants for Kea deployments.
const (
	ConfigVolumeName         = "kea-config"
	ConfigMountPath          = "/etc/kea"
	ConfigTemplateMountPath  = "/etc/kea-template"
	ConfigWritableVolumeName = "kea-config-rendered"
	RunVolumeName            = "kea-run"
	RunMountPath             = "/var/run/kea"
	LeaseVolumeName          = "kea-leases"
	LeaseMountPath           = "/var/lib/kea"
	TLSVolumeName            = "kea-tls"
	TLSMountPath             = "/etc/kea/tls"
	ConfigHashAnnotation     = "kea.openshift.io/config-hash"
)

// defaultComponentImage maps a component name to its default container image.
var defaultComponentImage = map[string]string{
	"dhcp4":      DefaultKeaDhcp4Image,
	"dhcp6":      DefaultKeaDhcp6Image,
	"ctrl-agent": DefaultKeaCtrlAgentImage,
	"ddns":       DefaultKeaDhcpDdnsImage,
}

// defaultComponentCommand maps a component name to the absolute path of the
// Kea binary inside the ISC container images. Using absolute paths allows
// the stork-agent sidecar to detect Kea daemons via /proc without needing
// to resolve the process's current working directory (which requires SYS_PTRACE).
var defaultComponentCommand = map[string]string{
	"dhcp4":      "/usr/sbin/kea-dhcp4",
	"dhcp6":      "/usr/sbin/kea-dhcp6",
	"ctrl-agent": "/usr/sbin/kea-ctrl-agent",
	"ddns":       "/usr/sbin/kea-dhcp-ddns",
}

// DefaultImageForComponent returns the default container image for the given
// Kea component. If the component is not recognized, it returns an empty string.
func DefaultImageForComponent(component string) string {
	return defaultComponentImage[component]
}

// DefaultCommandForComponent returns the absolute path to the Kea binary for
// the given component. If the component is not recognized, it returns an empty string.
func DefaultCommandForComponent(component string) string {
	return defaultComponentCommand[component]
}

// DeploymentParams holds all parameters needed to build a Kea component
// Deployment.
type DeploymentParams struct {
	Namespace          string
	CRName             string
	Component          string
	Command            string // e.g. kea-dhcp4, kea-dhcp6, kea-ctrl-agent, kea-dhcp-ddns
	ConfigFileName     string
	ConfigMapName      string
	Image              string
	ImagePullPolicy    corev1.PullPolicy
	Replicas           *int32
	Resources          corev1.ResourceRequirements
	HostNetwork        bool
	NodeSelector       map[string]string
	Tolerations        []corev1.Toleration
	Affinity           *corev1.Affinity
	TLSSecretName      string
	ServiceAccountName string
	ConfigHash         string
	ImagePullSecrets   []corev1.LocalObjectReference
	PodAnnotations     map[string]string
	// NADInterface is the network-attachment-definition interface name (e.g., "net1").
	// When set, the container command assigns an IP on this interface before starting Kea.
	NADInterface string
	// NADSubnet is the subnet in CIDR notation for the NAD interface (e.g., "10.200.0.0/24").
	// The pod IP will be NADSubnet base + 2 (e.g., 10.200.0.2).
	NADSubnet string
	// StorkAgent holds resolved stork-agent sidecar params. Nil means no sidecar.
	StorkAgent *StorkSidecarParams
	// SecretEnvVars are environment variables sourced from Secrets (via ValueFrom.SecretKeyRef).
	// The Kea config uses env var placeholders (e.g., $KEA_LEASE_DB_PASSWORD) that are expanded
	// at container startup via envsubst before Kea reads the config.
	SecretEnvVars []corev1.EnvVar
}

// BuildDeployment constructs a Kubernetes Deployment for a Kea component based
// on the provided parameters.
func BuildDeployment(p DeploymentParams) *appsv1.Deployment {
	labels := CommonLabels(p.CRName, p.Component)

	image := p.Image
	if image == "" {
		image = DefaultImageForComponent(p.Component)
	}

	hasSecretEnvVars := len(p.SecretEnvVars) > 0

	// Volumes: config (from ConfigMap), run (emptyDir), leases (emptyDir).
	// When secret env vars are present, the ConfigMap mounts at a template path
	// and a writable emptyDir is used at the actual config path.
	configVolumeMountPath := ConfigMountPath
	if hasSecretEnvVars {
		configVolumeMountPath = ConfigTemplateMountPath
	}

	volumes := []corev1.Volume{
		{
			Name: ConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: p.ConfigMapName,
					},
				},
			},
		},
		{
			Name: RunVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: LeaseVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Volume mounts for the Kea container.
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      ConfigVolumeName,
			MountPath: configVolumeMountPath,
			ReadOnly:  true,
		},
		{
			Name:      RunVolumeName,
			MountPath: RunMountPath,
		},
		{
			Name:      LeaseVolumeName,
			MountPath: LeaseMountPath,
		},
	}

	// When secrets are referenced via env vars, add a writable volume for the
	// rendered config (env var placeholders are expanded at startup).
	if hasSecretEnvVars {
		volumes = append(volumes, corev1.Volume{
			Name: ConfigWritableVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      ConfigWritableVolumeName,
			MountPath: ConfigMountPath,
		})
	}

	// Optional TLS secret volume.
	if p.TLSSecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: TLSVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: p.TLSSecretName,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      TLSVolumeName,
			MountPath: TLSMountPath,
			ReadOnly:  true,
		})
	}

	// Security context: drop ALL, add NET_RAW and NET_BIND_SERVICE.
	addCaps := []corev1.Capability{
		"NET_RAW",
		"NET_BIND_SERVICE",
	}
	if p.NADInterface != "" {
		addCaps = append(addCaps, "NET_ADMIN")
	}
	securityContext := &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add:  addCaps,
		},
	}

	// Pod template annotations: merge user-supplied annotations with config hash.
	podAnnotations := make(map[string]string, len(p.PodAnnotations)+1)
	for k, v := range p.PodAnnotations {
		podAnnotations[k] = v
	}
	podAnnotations[ConfigHashAnnotation] = p.ConfigHash

	mainCmd := []string{p.Command, "-c", ConfigMountPath + "/" + p.ConfigFileName}
	if p.NADInterface != "" && p.NADSubnet != "" &&
		ValidateNADShellInputs(p.NADInterface, p.NADSubnet, p.Command) == nil {
		// Assign a static IP on the NAD interface before starting Kea.
		// For a single-replica Deployment the IP is subnet base + 2.
		mainCmd = []string{"sh", "-c", fmt.Sprintf(
			`IFS='./' read a b c d prefix <<EOF
%s
EOF
ip_last=$((d + 2)); `+
				`ip addr add ${a}.${b}.${c}.${ip_last}/${prefix} dev %s 2>/dev/null || true; `+
				`echo "Assigned ${a}.${b}.${c}.${ip_last}/${prefix} to %s"; `+
				`exec %s -c %s/%s`,
			p.NADSubnet, p.NADInterface, p.NADInterface,
			p.Command, ConfigMountPath, p.ConfigFileName,
		)}
	}

	// When secret env vars are present, wrap the command to expand placeholders.
	if hasSecretEnvVars {
		mainCmd = buildSecretEnvSubstCommand(p.SecretEnvVars, mainCmd)
	}

	container := corev1.Container{
		Name:            p.Component,
		Image:           image,
		ImagePullPolicy: p.ImagePullPolicy,
		Command:         mainCmd,
		Env:             p.SecretEnvVars,
		Resources:       p.Resources,
		VolumeMounts:    volumeMounts,
		SecurityContext: securityContext,
	}

	containers := []corev1.Container{container}

	// Stork agent sidecar: shares PID namespace and config volume.
	var shareProcessNamespace *bool
	if p.StorkAgent != nil {
		storkContainer := buildStorkAgentContainer(p.StorkAgent, ConfigVolumeName)
		containers = append(containers, storkContainer)
		t := true
		shareProcessNamespace = &t
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(p.CRName, p.Component),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: p.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:    p.ServiceAccountName,
					HostNetwork:           p.HostNetwork,
					ShareProcessNamespace: shareProcessNamespace,
					Containers:            containers,
					Volumes:               volumes,
					NodeSelector:          p.NodeSelector,
					Tolerations:           p.Tolerations,
					Affinity:              p.Affinity,
					ImagePullSecrets:      p.ImagePullSecrets,
				},
			},
		},
	}

	return deploy
}

// buildSecretEnvSubstCommand builds a shell command that:
// 1. Copies the config template to a writable volume
// 2. Substitutes env var placeholders using sed
// 3. Execs the original command
// The env var names (e.g., KEA_LEASE_DB_PASSWORD) are expanded via ${VAR} in sed.
func buildSecretEnvSubstCommand(envVars []corev1.EnvVar, origCmd []string) []string {
	// Build sed expressions for each env var placeholder.
	sedExpr := ""
	for _, ev := range envVars {
		// Replace literal $VAR_NAME in the config with the env var value.
		// Use | as sed delimiter to avoid conflicts with base64/URL characters.
		sedExpr += fmt.Sprintf(`sed -i "s|\$%s|${%s}|g" %s/*.json; `, ev.Name, ev.Name, ConfigMountPath)
	}

	// Build the full command: copy template -> sed substitute -> exec original command.
	origCmdStr := ""
	for i, arg := range origCmd {
		if i > 0 {
			origCmdStr += " "
		}
		origCmdStr += arg
	}
	script := fmt.Sprintf(
		`cp %s/* %s/ && %sexec %s`,
		ConfigTemplateMountPath, ConfigMountPath,
		sedExpr,
		origCmdStr,
	)
	return []string{"sh", "-c", script}
}
