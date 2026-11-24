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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// TestStatefulSetInitScriptDoesNotPrintConfig runs the generated init script
// against a temporary copy of the per-ordinal config layout and checks two
// things: the ordinal's config is selected with its Secret placeholder
// expanded, and nothing the script prints contains the rendered configuration
// or the credential. Container logs are readable far more widely than the
// Secret the credential came from.
func TestStatefulSetInitScriptDoesNotPrintConfig(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	const (
		envName  = "KEA_LEASE_DB_PASSWORD"
		password = "synthetic-p4ssw0rd-9c2"
		ordinal  = 1
	)
	confName := ConfigFileName(ComponentDhcp4)
	marker := func(ord int) string { return fmt.Sprintf("ordinal-%d-config-marker", ord) }

	sts := BuildStatefulSet(StatefulSetParams{
		Namespace:      testNamespace,
		CRName:         testCRName,
		Component:      ComponentDhcp4,
		Command:        DefaultCommandForComponent(ComponentDhcp4),
		ConfigFileName: confName,
		ConfigMapNames: map[int]string{0: "cm-0", 1: "cm-1"},
		SecretEnvVars: []corev1.EnvVar{{
			Name: envName,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "db"},
				Key:                  "db-password",
			}},
		}},
	})
	initContainers := sts.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 || len(initContainers[0].Command) != 3 {
		t.Fatalf("expected one init container running sh -c, got %+v", initContainers)
	}
	script := initContainers[0].Command[2]

	root := t.TempDir()
	configs := filepath.Join(root, "configs")
	active := filepath.Join(root, "active")
	run := filepath.Join(root, "run")
	for ord := 0; ord <= 1; ord++ {
		dir := filepath.Join(configs, fmt.Sprint(ord))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := fmt.Sprintf(`{"Dhcp4": {"lease-database": {"name": %q, "password": "$%s"}}}`, marker(ord), envName)
		if err := os.WriteFile(filepath.Join(dir, confName), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{active, run} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The more specific paths go first: both live under ConfigMountPath.
	script = strings.ReplaceAll(script, ConfigsBaseMountPath, configs)
	script = strings.ReplaceAll(script, ActiveConfigMountPath, active)
	script = strings.ReplaceAll(script, RunMountPath, run)

	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "HOSTNAME="+testCRName+"-dhcp4-"+fmt.Sprint(ordinal), envName+"="+password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init script failed: %v\n%s", err, out)
	}

	got, err := os.ReadFile(filepath.Join(active, confName))
	if err != nil {
		t.Fatalf("active config not written: %v", err)
	}
	if !strings.Contains(string(got), marker(ordinal)) {
		t.Errorf("wrong ordinal selected: %s", got)
	}
	if !strings.Contains(string(got), password) {
		t.Errorf("credential placeholder not expanded in the active config: %s", got)
	}

	for _, leak := range []string{password, marker(ordinal), `"lease-database"`} {
		if strings.Contains(string(out), leak) {
			t.Errorf("init container output contains %q; it must not print the configuration:\n%s", leak, out)
		}
	}
}
