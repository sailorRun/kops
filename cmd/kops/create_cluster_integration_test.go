/*
Copyright 2016 The Kubernetes Authors.

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

package main

import (
	"bytes"
	"github.com/golang/glog"
	"io/ioutil"
	"k8s.io/kops/cmd/kops/util"
	"k8s.io/kops/pkg/apis/kops"
	"k8s.io/kops/pkg/diff"
	k8sapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"path"
	"strings"
	"testing"
	"time"
)

var MagicTimestamp = unversioned.Time{Time: time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)}

// TestCreateClusterMinimal runs kops create cluster minimal.example.com --zones us-test-1a
func TestCreateClusterMinimal(t *testing.T) {
	runCreateClusterIntegrationTest(t, "../../tests/integration/create_cluster/minimal", "v1alpha1")
	runCreateClusterIntegrationTest(t, "../../tests/integration/create_cluster/minimal", "v1alpha2")
}

// TestCreateClusterHA runs kops create cluster ha.example.com --zones us-test-1a,us-test-1b,us-test-1c --master-zones us-test-1a,us-test-1b,us-test-1c
func TestCreateClusterHA(t *testing.T) {
	runCreateClusterIntegrationTest(t, "../../tests/integration/create_cluster/ha", "v1alpha1")
	runCreateClusterIntegrationTest(t, "../../tests/integration/create_cluster/ha", "v1alpha2")
}

// TestCreateClusterPrivate runs kops create cluster private.example.com --zones us-test-1a --master-zones us-test-1a
func TestCreateClusterPrivate(t *testing.T) {
	runCreateClusterIntegrationTest(t, "../../tests/integration/create_cluster/private", "v1alpha1")
	runCreateClusterIntegrationTest(t, "../../tests/integration/create_cluster/private", "v1alpha2")
}

func runCreateClusterIntegrationTest(t *testing.T, srcDir string, version string) {
	var stdout bytes.Buffer

	optionsYAML := "options.yaml"
	expectedClusterPath := "expected-" + version + ".yaml"

	factoryOptions := &util.FactoryOptions{}
	factoryOptions.RegistryPath = "memfs://tests"

	h := NewIntegrationTestHarness(t)
	defer h.Close()

	h.SetupMockAWS()

	publicKeyPath := path.Join(h.TempDir, "id_rsa.pub")
	privateKeyPath := path.Join(h.TempDir, "id_rsa")
	{
		if err := MakeSSHKeyPair(publicKeyPath, privateKeyPath); err != nil {
			t.Fatalf("error making SSH keypair: %v", err)
		}
	}

	factory := util.NewFactory(factoryOptions)

	{
		optionsBytes, err := ioutil.ReadFile(path.Join(srcDir, optionsYAML))
		if err != nil {
			t.Fatalf("error reading options file: %v", err)
		}

		options := &CreateClusterOptions{}
		options.InitDefaults()

		err = kops.ParseRawYaml(optionsBytes, options)
		if err != nil {
			t.Fatalf("error parsing options: %v", err)
		}

		// No preview
		options.Target = ""

		// Use the public key we produced
		options.SSHPublicKey = publicKeyPath

		err = RunCreateCluster(factory, &stdout, options)
		if err != nil {
			t.Fatalf("error running create cluster: %v", err)
		}
	}

	clientset, err := factory.Clientset()
	if err != nil {
		t.Fatalf("error getting clientset: %v", err)
	}

	// Compare cluster
	clusters, err := clientset.Clusters().List(k8sapi.ListOptions{})
	if err != nil {
		t.Fatalf("error listing clusters: %v", err)
	}

	if len(clusters.Items) != 1 {
		t.Fatalf("expected one cluster, found %d", len(clusters.Items))
	}

	var yamlAll []string

	for _, cluster := range clusters.Items {
		cluster.ObjectMeta.CreationTimestamp = MagicTimestamp
		actualYAMLBytes, err := kops.ToVersionedYamlWithVersion(&cluster, version)
		if err != nil {
			t.Fatalf("unexpected error serializing cluster: %v", err)
		}
		actualYAML := strings.TrimSpace(string(actualYAMLBytes))

		yamlAll = append(yamlAll, actualYAML)
	}

	// Compare instance groups

	instanceGroups, err := clientset.InstanceGroups(clusters.Items[0].ObjectMeta.Name).List(k8sapi.ListOptions{})
	if err != nil {
		t.Fatalf("error listing instance groups: %v", err)
	}

	for _, ig := range instanceGroups.Items {
		ig.ObjectMeta.CreationTimestamp = MagicTimestamp

		actualYAMLBytes, err := kops.ToVersionedYamlWithVersion(&ig, version)
		if err != nil {
			t.Fatalf("unexpected error serializing InstanceGroup: %v", err)
		}

		actualYAML := strings.TrimSpace(string(actualYAMLBytes))

		yamlAll = append(yamlAll, actualYAML)
	}

	expectedYAMLBytes, err := ioutil.ReadFile(path.Join(srcDir, expectedClusterPath))
	if err != nil {
		t.Fatalf("unexpected error reading expected YAML: %v", err)
	}

	expectedYAML := strings.TrimSpace(string(expectedYAMLBytes))

	actualYAML := strings.Join(yamlAll, "\n\n---\n\n")
	if actualYAML != expectedYAML {
		glog.Infof("Actual YAML:\n%s\n", actualYAML)

		diffString := diff.FormatDiff(expectedYAML, actualYAML)
		t.Logf("diff:\n%s\n", diffString)

		t.Fatalf("YAML differed from expected")
	}

}
