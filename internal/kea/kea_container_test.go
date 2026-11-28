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

package kea

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// shippedKeaImages are the images the operator deploys when a CR names none.
// The container tests run these, so a change to an operator default changes
// what the tests check. The Control Agent has no default image (ISC publishes
// none); its tests build a fixture, see ctrlAgentFixture.
var shippedKeaImages = map[string]string{
	resources.ComponentDhcp4: resources.DefaultKeaDhcp4Image,
	resources.ComponentDhcp6: resources.DefaultKeaDhcp6Image,
	resources.ComponentDdns:  resources.DefaultKeaDhcpDdnsImage,
}

// keaContainers runs the Kea container images in a test.
type keaContainers struct {
	podman string
}

// keaContainerCheck prepares a test that runs the Kea container images. Those
// tests need podman and network access, so they only run when
// KEA_CONFIG_CHECK=1; once requested, a missing podman is a failure rather
// than a silent skip.
//
// The images are the operator's defaults (shippedKeaImages). KEA_IMAGE_REPO
// replaces their repository (e.g. a mirror) and KEA_IMAGE_TAG their tag (e.g.
// a Kea version the operator does not ship yet).
func keaContainerCheck(t *testing.T) keaContainers {
	t.Helper()
	if os.Getenv("KEA_CONFIG_CHECK") != "1" {
		t.Skip("set KEA_CONFIG_CHECK=1 to run the Kea container images")
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Fatal("KEA_CONFIG_CHECK=1 but podman is not installed")
	}
	return keaContainers{podman: podman}
}

// keaTestImage is the image the container tests run for component: the
// operator's default, with repo and tag replacing its repository and tag when
// they are set.
func keaTestImage(component, repo, tag string) string {
	shipped, ok := shippedKeaImages[component]
	if !ok {
		panic("no shipped Kea image for component " + component)
	}
	i := strings.LastIndex(shipped, ":")
	name, shippedTag := shipped[:i], shipped[i+1:]
	if repo != "" {
		name = strings.TrimSuffix(repo, "/") + name[strings.LastIndex(name, "/"):]
	}
	if tag == "" {
		tag = shippedTag
	}
	return name + ":" + tag
}

// pulledDigests maps each image pulled by this test binary to its digest, so
// every image is pulled once however many tests run it.
var pulledDigests sync.Map

// image returns the image to run for component (a resources.Component*
// value) and logs it with its digest,
// so every test's output records exactly which Kea build it checked. The
// first call for an image pulls it.
func (k keaContainers) image(t *testing.T, component string) string {
	t.Helper()
	image := keaTestImage(component, os.Getenv("KEA_IMAGE_REPO"), os.Getenv("KEA_IMAGE_TAG"))
	digest, ok := pulledDigests.Load(image)
	if !ok {
		if out, err := exec.Command(k.podman, "pull", "-q", image).CombinedOutput(); err != nil {
			t.Fatalf("pulling %s: %v\n%s", image, err, out)
		}
		out, err := exec.Command(k.podman, "image", "inspect", "--format", "{{.Digest}}", image).Output()
		if err != nil {
			t.Fatalf("inspecting %s: %v", image, err)
		}
		digest, _ = pulledDigests.LoadOrStore(image, strings.TrimSpace(string(out)))
	}
	t.Logf("Kea image: %s@%s", image, digest)
	return image
}

// pinned returns image (already returned by k.image) as <repository>@<digest>.
func pinned(image string) string {
	digest, ok := pulledDigests.Load(image)
	if !ok {
		panic("pinned called before image " + image + " was pulled")
	}
	return image[:strings.LastIndex(image, ":")] + "@" + digest.(string)
}

func TestKeaTestImageFollowsShippedDefaults(t *testing.T) {
	const mirror = "mirror.example.com/isc"
	tagOf := func(image string) string { return image[strings.LastIndex(image, ":")+1:] }
	cases := []struct {
		component, repo, tag, want string
	}{
		{resources.ComponentDhcp4, "", "", resources.DefaultKeaDhcp4Image},
		{resources.ComponentDhcp6, "", "", resources.DefaultKeaDhcp6Image},
		{resources.ComponentDdns, "", "", resources.DefaultKeaDhcpDdnsImage},
		{resources.ComponentDhcp4, mirror + "/", "", mirror + "/kea-dhcp4:" + tagOf(resources.DefaultKeaDhcp4Image)},
		{resources.ComponentDdns, "", "9.9.9",
			strings.TrimSuffix(resources.DefaultKeaDhcpDdnsImage, tagOf(resources.DefaultKeaDhcpDdnsImage)) + "9.9.9"},
		{resources.ComponentDhcp6, mirror, "9.9.9", mirror + "/kea-dhcp6:9.9.9"},
	}
	for _, tc := range cases {
		if got := keaTestImage(tc.component, tc.repo, tc.tag); got != tc.want {
			t.Errorf("keaTestImage(%q, %q, %q) = %q, want %q", tc.component, tc.repo, tc.tag, got, tc.want)
		}
	}
}

// loadSample reads a CR from config/samples into obj.
func loadSample(t *testing.T, file string, obj interface{}) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "samples", file))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, obj); err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
}

// keaParserAccepts runs `<binary> -t` from image on data and fails the test
// if Kea rejects the configuration or logs an error while checking it.
func keaParserAccepts(t *testing.T, k keaContainers, image, binary string, data []byte) {
	t.Helper()
	dir := t.TempDir()
	cfg := binary + ".conf"
	if err := os.WriteFile(filepath.Join(dir, cfg), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The Kea binaries may carry file capabilities, so the container needs them to exec.
	out, err := exec.Command(k.podman, "run", "--rm",
		"--cap-add=NET_RAW", "--cap-add=NET_BIND_SERVICE", "--cap-add=NET_ADMIN",
		"-v", dir+":/cfg:ro,z", "--entrypoint", "/usr/sbin/"+binary,
		image, "-t", "/cfg/"+cfg).CombinedOutput()
	if err != nil {
		t.Fatalf("%s -t rejected the rendered config: %v\n%s\n--- config ---\n%s", binary, err, out, data)
	}
	if strings.Contains(string(out), "ERROR") {
		t.Fatalf("%s -t logged an error:\n%s\n--- config ---\n%s", binary, out, data)
	}
}
