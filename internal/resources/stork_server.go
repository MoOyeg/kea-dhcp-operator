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

package resources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	DefaultStorkServerImage = "quay.io/mooyeg/stork-server:v2.4.0"
	DefaultStorkServerPort  = int32(8080)
	StorkServerComponent    = "stork-server"
)

// StorkServerParams holds parameters for building Stork server resources.
type StorkServerParams struct {
	Namespace          string
	CRName             string
	Image              string
	ImagePullPolicy    corev1.PullPolicy
	Replicas           *int32
	Resources          corev1.ResourceRequirements
	Port               int32
	EnableMetrics      bool
	ImagePullSecrets   []corev1.LocalObjectReference
	ServiceAccountName string

	// Database connection (via SecretKeyRef env vars).
	DBHost    string
	DBPort    int32
	DBName    string
	DBSSLMode string

	// Scheduling.
	NodeSelector   map[string]string
	Tolerations    []corev1.Toleration
	Affinity       *corev1.Affinity
	PodAnnotations map[string]string

	// SecretEnvVars holds env vars with ValueFrom.SecretKeyRef for DB credentials.
	SecretEnvVars []corev1.EnvVar

	// AdminSecretName is the name of the Secret containing the generated admin password.
	AdminSecretName string

	// ServerTokenSecretName is the name of the Secret containing the server agent token.
	ServerTokenSecretName string
}

// BuildStorkServerDeployment constructs a Deployment for the Stork server.
func BuildStorkServerDeployment(p StorkServerParams) *appsv1.Deployment {
	labels := CommonLabels(p.CRName, StorkServerComponent)

	image := p.Image
	if image == "" {
		image = DefaultStorkServerImage
	}

	port := p.Port
	if port == 0 {
		port = DefaultStorkServerPort
	}

	env := []corev1.EnvVar{
		{Name: "STORK_DATABASE_HOST", Value: p.DBHost},
		{Name: "STORK_DATABASE_PORT", Value: fmt.Sprintf("%d", p.DBPort)},
		{Name: "STORK_DATABASE_NAME", Value: p.DBName},
		{Name: "STORK_DATABASE_SSLMODE", Value: p.DBSSLMode},
		{Name: "STORK_REST_HOST", Value: "0.0.0.0"},
		{Name: "STORK_REST_PORT", Value: fmt.Sprintf("%d", port)},
	}

	if p.EnableMetrics {
		env = append(env, corev1.EnvVar{Name: "STORK_SERVER_ENABLE_METRICS", Value: "true"})
	}

	// Set the server agent token from the token Secret so agents can register.
	if p.ServerTokenSecretName != "" {
		env = append(env, corev1.EnvVar{
			Name: "STORK_SERVER_AGENT_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: p.ServerTokenSecretName},
					Key:                  "token",
				},
			},
		})
	}

	// Append SecretKeyRef env vars for DB credentials.
	env = append(env, p.SecretEnvVars...)

	replicas := p.Replicas
	if replicas == nil {
		one := int32(1)
		replicas = &one
	}

	annotations := make(map[string]string)
	for k, v := range p.PodAnnotations {
		annotations[k] = v
	}

	// Build init container for DB migration.
	initEnv := make([]corev1.EnvVar, len(env))
	copy(initEnv, env)
	initEnv = append(initEnv, p.SecretEnvVars...)

	migrationScript := `set -e
stork-tool db-init \
  --db-host "$STORK_DATABASE_HOST" \
  --db-port "$STORK_DATABASE_PORT" \
  --db-name "$STORK_DATABASE_NAME" \
  --db-user "$STORK_DATABASE_USER_NAME" \
  --db-password "$STORK_DATABASE_PASSWORD" \
  --db-sslmode "$STORK_DATABASE_SSLMODE"
stork-tool db-up \
  --db-host "$STORK_DATABASE_HOST" \
  --db-port "$STORK_DATABASE_PORT" \
  --db-name "$STORK_DATABASE_NAME" \
  --db-user "$STORK_DATABASE_USER_NAME" \
  --db-password "$STORK_DATABASE_PASSWORD" \
  --db-sslmode "$STORK_DATABASE_SSLMODE"
echo "Database migration complete"
`
	initContainer := corev1.Container{
		Name:            "db-migrate",
		Image:           image,
		ImagePullPolicy: p.ImagePullPolicy,
		Command:         []string{"sh", "-c", migrationScript},
		Env:             initEnv,
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	// Build the wrapper command that starts stork-server, waits for it to be
	// ready, then sets the admin password from the Secret via REST API.
	adminPasswordEnv := corev1.EnvVar{
		Name: "STORK_ADMIN_PASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: p.AdminSecretName},
				Key:                  "password",
			},
		},
	}
	env = append(env, adminPasswordEnv)

	wrapperScript := fmt.Sprintf(`#!/bin/sh
# Start stork-server in the background.
stork-server &
SERVER_PID=$!

# Wait for the server to become ready (up to 120 seconds).
for i in $(seq 1 60); do
  if curl -sf http://localhost:%d/api/version > /dev/null 2>&1; then
    break
  fi
  sleep 2
done

# Extra delay to ensure API is fully initialized.
sleep 5

# Save password to a file to avoid escaping issues in JSON.
printf '%%s' "$STORK_ADMIN_PASSWORD" > /tmp/admin_pw
ADMIN_PW="$(cat /tmp/admin_pw)"

# Try to log in with the desired password (already set from previous run).
RESP=$(curl -s -o /dev/null -w '%%{http_code}' -X POST http://localhost:%d/api/sessions \
  -H 'Content-Type: application/json' \
  -d "{\"authenticationMethodId\":\"internal\",\"identifier\":\"admin\",\"secret\":\"${ADMIN_PW}\"}")

if [ "$RESP" = "200" ]; then
  echo "Admin password already set correctly"
else
  # Log in with default password and change it.
  curl -s -c /tmp/cookies -X POST http://localhost:%d/api/sessions \
    -H 'Content-Type: application/json' \
    -d '{"authenticationMethodId":"internal","identifier":"admin","secret":"admin"}' > /tmp/login_result 2>&1

  LOGIN_CODE=$?
  echo "Login result: $(cat /tmp/login_result)"

  curl -s -b /tmp/cookies -X PUT http://localhost:%d/api/users/1/password \
    -H 'Content-Type: application/json' \
    -d "{\"oldpassword\":\"admin\",\"newpassword\":\"${ADMIN_PW}\"}" > /tmp/pw_result 2>&1

  if grep -q '"message"' /tmp/pw_result; then
    echo "Warning: password change response: $(cat /tmp/pw_result)"
  else
    echo "Admin password updated from Secret"
  fi
  rm -f /tmp/cookies /tmp/login_result /tmp/pw_result
fi
rm -f /tmp/admin_pw

# Forward signals to stork-server and wait.
trap "kill $SERVER_PID" TERM INT
wait $SERVER_PID
`, port, port, port, port)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(p.CRName, StorkServerComponent),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{initContainer},
					Containers: []corev1.Container{
						{
							Name:            "stork-server",
							Image:           image,
							ImagePullPolicy: p.ImagePullPolicy,
							Command:         []string{"sh", "-c", wrapperScript},
							Resources:       p.Resources,
							Env:             env,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/version",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/version",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       30,
							},
							SecurityContext: &corev1.SecurityContext{
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
					ServiceAccountName: p.ServiceAccountName,
					NodeSelector:       p.NodeSelector,
					Tolerations:        p.Tolerations,
					Affinity:           p.Affinity,
					ImagePullSecrets:   p.ImagePullSecrets,
				},
			},
		},
	}
}

// BuildStorkServerService constructs a Service for the Stork server web UI and REST API.
func BuildStorkServerService(namespace, crName string, port int32) *corev1.Service {
	labels := CommonLabels(crName, StorkServerComponent)

	if port == 0 {
		port = DefaultStorkServerPort
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName(crName, StorkServerComponent),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildStorkServerAdminSecret constructs a Secret containing the Stork admin credentials.
func BuildStorkServerAdminSecret(namespace, secretName string, labels map[string]string, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte("admin"),
			"password": []byte(password),
		},
	}
}

// BuildStorkServerTokenSecret constructs a Secret containing the Stork server agent token.
// Agents use this token to register with the server non-interactively.
func BuildStorkServerTokenSecret(namespace, secretName string, labels map[string]string, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}
}

// BuildStorkServerRoute constructs an OpenShift Route (unstructured) for the
// Stork server web UI with edge TLS termination.
func BuildStorkServerRoute(namespace, crName string) *unstructured.Unstructured {
	labels := CommonLabels(crName, StorkServerComponent)
	svcName := ServiceName(crName, StorkServerComponent)
	routeName := ServiceName(crName, StorkServerComponent)

	labelsIface := make(map[string]interface{}, len(labels))
	for k, v := range labels {
		labelsIface[k] = v
	}

	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "route.openshift.io",
		Version: "v1",
		Kind:    "Route",
	})
	route.Object = map[string]interface{}{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata": map[string]interface{}{
			"name":      routeName,
			"namespace": namespace,
			"labels":    labelsIface,
		},
		"spec": map[string]interface{}{
			"to": map[string]interface{}{
				"kind": "Service",
				"name": svcName,
			},
			"port": map[string]interface{}{
				"targetPort": "http",
			},
			"tls": map[string]interface{}{
				"termination": "edge",
			},
		},
	}

	return route
}
