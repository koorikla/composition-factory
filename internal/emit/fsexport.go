package emit

import (
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// templateCMCap is the per-ConfigMap budget for template bytes (key + value).
// The Kubernetes API server limit is 1 MiB per object; 900 KiB leaves headroom
// for names, labels and metadata.
var templateCMCap = 900 * 1024

// TemplateFile is one file of the FileSystem-source template tree.
type TemplateFile struct {
	Name string
	Body []byte
}

// inlineTemplateBody renders the whole template body at column zero — the
// exact text Composition inlines under `template: |`.
func inlineTemplateBody(b *blueprint.Blueprint, crds []schema.CRD) ([]byte, error) {
	d := NewDoc()
	if err := writeTemplateBody(d, 0, b, crds, b.Spec.XRD.Scope == "Namespaced"); err != nil {
		return nil, err
	}
	return d.Bytes(), nil
}

// TemplateFiles splits the template body into individual document files:
// 000-context.yaml (user defines + context assignments) followed by one
// numbered file per resource (e.g. 001-main-queue.yaml, including the leading
// `---` separator). Concatenating these in lexical order reproduces the inline
// body exactly.
func TemplateFiles(b *blueprint.Blueprint, crds []schema.CRD) ([]TemplateFile, error) {
	wantNamespaced := b.Spec.XRD.Scope == "Namespaced"
	files := make([]TemplateFile, len(b.Spec.Resources)+1)

	d0 := NewDoc()
	writeTemplatePreamble(d0, 0, b)
	files[0] = TemplateFile{
		Name: "000-context.yaml",
		Body: d0.Bytes(),
	}

	for i, r := range b.Spec.Resources {
		dr := NewDoc()
		if err := writeResourceTemplate(dr, 0, r, b, crds, wantNamespaced); err != nil {
			return nil, err
		}
		files[i+1] = TemplateFile{
			Name: fmt.Sprintf("%03d-%s.yaml", i+1, r.Name),
			Body: dr.Bytes(),
		}
	}
	return files, nil
}

// templatesDirPath is where the DeploymentRuntimeConfig mounts the template
// projected volume and where the Composition's fileSystem source reads them.
func templatesDirPath(b *blueprint.Blueprint) string {
	return "/templates/" + b.Spec.XRD.Plural + "." + b.Spec.XRD.Group
}

// packTemplateCMs assigns files to ConfigMaps sequentially under the size
// budget.
func packTemplateCMs(files []TemplateFile) ([][]TemplateFile, error) {
	var groups [][]TemplateFile
	var cur []TemplateFile
	size := 0
	for _, f := range files {
		itemSize := len(f.Name) + len(f.Body)
		if itemSize > templateCMCap {
			return nil, fmt.Errorf("single template file %q (%d bytes) exceeds the %d-byte ConfigMap budget",
				f.Name, itemSize, templateCMCap)
		}
		if len(cur) > 0 && size+itemSize > templateCMCap {
			groups = append(groups, cur)
			cur, size = nil, 0
		}
		cur = append(cur, f)
		size += itemSize
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups, nil
}

func templateCMName(b *blueprint.Blueprint, i int) string {
	return b.Spec.XRD.Plural + "." + b.Spec.XRD.Group + "-templates-" + fmt.Sprint(i)
}

// RuntimeDoc renders the runtime/<plural>.<group>.yaml file containing
// the ConfigMap(s) and the DeploymentRuntimeConfig.
func RuntimeDoc(b *blueprint.Blueprint, crds []schema.CRD) ([]byte, error) {
	files, err := TemplateFiles(b, crds)
	if err != nil {
		return nil, err
	}
	groups, err := packTemplateCMs(files)
	if err != nil {
		return nil, err
	}

	d := NewDoc()
	header(d, blueprintSource(b))
	d.Comment("ConfigMap(s) carrying the FileSystem template files, followed by")
	d.Comment("the DeploymentRuntimeConfig that mounts them into the function pod.")

	for i, g := range groups {
		if i > 0 {
			d.Line(0, "---")
		}
		d.Line(0, "apiVersion: v1")
		d.Line(0, "kind: ConfigMap")
		d.Line(0, "metadata:")
		d.Line(1, "name: %s", templateCMName(b, i))
		d.Line(1, "namespace: crossplane-system")
		d.Line(0, "data:")
		for _, f := range g {
			d.Line(1, "%s: |", f.Name)
			lines := strings.Split(string(f.Body), "\n")
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			for _, line := range lines {
				if line == "" {
					d.Line(0, "")
				} else {
					d.Line(2, "%s", line)
				}
			}
		}
	}

	d.Line(0, "")
	d.Line(0, "---")
	d.Line(0, "apiVersion: pkg.crossplane.io/v1beta1")
	d.Line(0, "kind: DeploymentRuntimeConfig")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s", blueprint.TemplatingFunctionName)
	d.Line(0, "spec:")
	d.Line(1, "deploymentTemplate:")
	d.Line(2, "spec:")
	d.Line(3, "selector: {}")
	d.Line(3, "template:")
	d.Line(4, "spec:")
	d.Line(5, "containers:")
	d.Line(5, "- name: package-runtime")
	d.Line(6, "volumeMounts:")
	d.Line(6, "- name: templates")
	d.Line(7, "mountPath: %s", templatesDirPath(b))
	d.Line(7, "readOnly: true")
	d.Line(5, "volumes:")
	d.Line(5, "- name: templates")
	d.Line(6, "projected:")
	d.Line(7, "sources:")
	for i := range groups {
		d.Line(7, "- configMap:")
		d.Line(9, "name: %s", templateCMName(b, i))
	}

	return d.Bytes(), nil
}
