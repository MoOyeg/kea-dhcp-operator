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

	appsv1 "k8s.io/api/apps/v1"
)

// runInitScript runs the StatefulSet init script for one ordinal against a
// temporary copy of the per-ordinal configuration layout, the way the
// configuration Secrets are mounted, and returns its output and the path of
// the active configuration.
func runInitScript(t *testing.T, sts *appsv1.StatefulSet, files map[int]string, ordinal int) (string, string, error) {
	t.Helper()
	initContainers := sts.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 || len(initContainers[0].Command) != 3 {
		t.Fatalf("expected one init container running sh -c, got %+v", initContainers)
	}
	script := initContainers[0].Command[2]
	confName := ConfigFileName(ComponentDhcp4)

	root := t.TempDir()
	configs, active, run := filepath.Join(root, "configs"), filepath.Join(root, "active"), filepath.Join(root, "run")
	for ord, content := range files {
		dir := filepath.Join(configs, fmt.Sprint(ord))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, confName), []byte(content), 0o440); err != nil {
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
	cmd.Env = append(os.Environ(), fmt.Sprintf("HOSTNAME=%s-dhcp4-%d", testCRName, ordinal))
	out, err := cmd.CombinedOutput()
	return string(out), filepath.Join(active, confName), err
}

func haStatefulSet() *appsv1.StatefulSet {
	return BuildStatefulSet(StatefulSetParams{
		Namespace: testNamespace, CRName: testCRName, Component: ComponentDhcp4,
		Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
		ConfigMapNames: map[int]string{0: "cm-0", 1: "cm-1"}, ConfigFromSecrets: true,
	})
}

// The init container selects the ordinal's configuration, which already holds
// the credentials, and must never print it: container logs are readable far
// more widely than the Secret the credentials came from.
func TestStatefulSetInitScriptDoesNotPrintConfig(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	const password = "synthetic-p4ssw0rd-9c2"
	marker := func(ord int) string { return fmt.Sprintf("ordinal-%d-config-marker", ord) }
	files := map[int]string{}
	for ord := 0; ord <= 1; ord++ {
		files[ord] = fmt.Sprintf(`{"Dhcp4": {"lease-database": {"name": %q, "password": %q}}}`, marker(ord), password)
	}
	sts := haStatefulSet()
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if strings.HasPrefix(v.Name, "kea-config-") && (v.Secret == nil || !strings.HasPrefix(v.Secret.SecretName, testCRName+"-dhcp4-config-")) {
			t.Errorf("per-ordinal volume %s is not mounted from the configuration Secret: %+v", v.Name, v.VolumeSource)
		}
	}
	if env := sts.Spec.Template.Spec.InitContainers[0].Env; len(env) != 0 {
		t.Errorf("init container still receives env vars: %v", env)
	}

	out, active, err := runInitScript(t, sts, files, 1)
	if err != nil {
		t.Fatalf("init script failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(active)
	if err != nil {
		t.Fatalf("active config not written: %v", err)
	}
	if !strings.Contains(string(got), marker(1)) || !strings.Contains(string(got), password) {
		t.Errorf("wrong configuration selected: %s", got)
	}
	for _, leak := range []string{password, marker(1), `"lease-database"`} {
		if strings.Contains(out, leak) {
			t.Errorf("init container output contains %q; it must not print the configuration:\n%s", leak, out)
		}
	}
}

// A failed stage must stop the pod instead of starting Kea without a
// configuration.
func TestStatefulSetInitScriptFailsWithoutItsConfig(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	out, _, err := runInitScript(t, haStatefulSet(), map[int]string{0: "{}"}, 1)
	if err == nil {
		t.Fatalf("init script succeeded without the ordinal's configuration:\n%s", out)
	}
}
