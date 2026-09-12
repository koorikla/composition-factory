package k8s_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type deploymentManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				InitContainers []struct {
					Name         string   `yaml:"name"`
					Image        string   `yaml:"image"`
					Command      []string `yaml:"command"`
					VolumeMounts []struct {
						Name      string `yaml:"name"`
						MountPath string `yaml:"mountPath"`
					} `yaml:"volumeMounts"`
				} `yaml:"initContainers"`
				Containers []struct {
					Name         string   `yaml:"name"`
					Image        string   `yaml:"image"`
					Command      []string `yaml:"command"`
					VolumeMounts []struct {
						Name      string `yaml:"name"`
						MountPath string `yaml:"mountPath"`
					} `yaml:"volumeMounts"`
				} `yaml:"containers"`
				Volumes []struct {
					Name     string `yaml:"name"`
					EmptyDir *struct {
					} `yaml:"emptyDir"`
				} `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func loadDeploymentManifest(t *testing.T) deploymentManifest {
	t.Helper()
	manifestPath := filepath.Join(".", "deployment.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read deployment.yaml: %v", err)
	}

	var dep deploymentManifest
	if err := yaml.Unmarshal(data, &dep); err != nil {
		t.Fatalf("failed to parse deployment.yaml: %v", err)
	}
	return dep
}

func TestDeploymentNoBlockingProviderAddInit(t *testing.T) {
	dep := loadDeploymentManifest(t)

	for _, initC := range dep.Spec.Template.Spec.InitContainers {
		for _, cmd := range initC.Command {
			if strings.Contains(cmd, "cf provider add") {
				t.Errorf("initContainer %q contains blocking 'cf provider add' command: %q", initC.Name, cmd)
			}
		}
		for _, vm := range initC.VolumeMounts {
			if vm.MountPath == "/home/cf/.cache/compositionfactory" {
				t.Errorf("initContainer %q mounts cache directory at %s; should not participate in provider caching", initC.Name, vm.MountPath)
			}
		}
	}
}

func TestDeploymentNoEphemeralCacheEmptyDir(t *testing.T) {
	dep := loadDeploymentManifest(t)

	for _, vol := range dep.Spec.Template.Spec.Volumes {
		if vol.Name == "cache" && vol.EmptyDir != nil {
			t.Errorf("volume %q is an ephemeral emptyDir: {} which masks pre-baked image schemas and discards downloaded schemas across pod restarts", vol.Name)
		}
	}

	for _, c := range dep.Spec.Template.Spec.Containers {
		for _, vm := range c.VolumeMounts {
			if vm.Name == "cache" && vm.MountPath == "/home/cf/.cache/compositionfactory" {
				t.Errorf("container %q mounts ephemeral emptyDir cache volume at %s, masking pre-baked image schemas", c.Name, vm.MountPath)
			}
		}
	}
}
