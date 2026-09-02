package emit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// The FileSystem export: the same Composition, with the go-template body
// shipped as a folder of files instead of one inline block scalar — one
// object per file for helm-chart-style readability, packed into ConfigMaps
// (split under the ~1MiB object limit) that a DeploymentRuntimeConfig
// mounts into the function pod.
//
// Safety rests on one fact from function-go-templating's own source
// (template.go readTemplates): the FileSystem source walks dirPath in
// lexical order, concatenates every file with "\n---\n", and parses the
// result as ONE template. So splitting the inline body exactly at its
// top-level "---" separator lines and numbering the files keeps the parsed
// template byte-identical to the inline form.

// templateCMCap is the per-ConfigMap budget for template bytes. ConfigMaps
// cap at ~1MiB total; this leaves headroom for names, keys and metadata.
// A var, not a const, so the splitting logic is testable without megabytes
// of fixture.
var templateCMCap = 700 * 1024

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

// TemplateFiles splits the template body at its top-level "---" lines:
// 00-head (defines + context preamble) then one numbered file per resource,
// in blueprint order. Rejoining the files with "\n---\n" — what the
// function's FileSystem source does — reproduces the inline body exactly.
func TemplateFiles(b *blueprint.Blueprint, crds []schema.CRD) ([]TemplateFile, error) {
	body, err := inlineTemplateBody(b, crds)
	if err != nil {
		return nil, err
	}
	var chunks [][]string
	cur := []string{}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	for _, line := range lines {
		if line == "---" {
			chunks = append(chunks, cur)
			cur = []string{}
			continue
		}
		cur = append(cur, line)
	}
	chunks = append(chunks, cur)
	if len(chunks) != len(b.Spec.Resources)+1 {
		// one "---" per resource is a structural invariant of
		// writeTemplateBody; failing loudly beats silently misnaming files
		return nil, fmt.Errorf("template body has %d documents for %d resources; "+
			"the per-resource split invariant is broken", len(chunks), len(b.Spec.Resources))
	}
	files := make([]TemplateFile, len(chunks))
	for i, c := range chunks {
		name := "00-head.yaml.tmpl"
		if i > 0 {
			name = fmt.Sprintf("%02d-%s.yaml.tmpl", i, b.Spec.Resources[i-1].Name)
		}
		files[i] = TemplateFile{Name: name, Body: []byte(strings.Join(c, "\n") + "\n")}
	}
	return files, nil
}

// templatesDirPath is where the DeploymentRuntimeConfig mounts the template
// ConfigMaps and where the Composition's fileSystem source reads them.
func templatesDirPath(b *blueprint.Blueprint) string {
	return "/templates/" + b.Spec.XRD.Plural + "." + b.Spec.XRD.Group
}

// packTemplateCMs assigns files to ConfigMaps sequentially under the size
// cap. Each ConfigMap mounts under its own numbered subdir, so the
// function's lexical WalkDir sees files in the original order across the
// whole split.
func packTemplateCMs(files []TemplateFile) [][]TemplateFile {
	var groups [][]TemplateFile
	var cur []TemplateFile
	size := 0
	for _, f := range files {
		if len(cur) > 0 && size+len(f.Body) > templateCMCap {
			groups = append(groups, cur)
			cur, size = nil, 0
		}
		cur = append(cur, f)
		size += len(f.Body)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

func templateCMName(b *blueprint.Blueprint, i int) string {
	return b.Spec.XRD.Plural + "." + b.Spec.XRD.Group + "-templates-" + fmt.Sprint(i)
}

// templateConfigMap renders one ConfigMap of template files.
func templateConfigMap(b *blueprint.Blueprint, i int, files []TemplateFile) []byte {
	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Comment("Template files for the FileSystem-source Composition; mounted by")
	d.Comment("the DeploymentRuntimeConfig in runtimeconfigs/. Apply to the")
	d.Comment("namespace the crossplane function pods run in (crossplane-system).")
	d.Line(0, "apiVersion: v1")
	d.Line(0, "kind: ConfigMap")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s", templateCMName(b, i))
	d.Line(1, "namespace: crossplane-system")
	d.Line(0, "data:")
	for _, f := range files {
		d.Line(1, "%s: |", f.Name)
		for _, line := range strings.Split(strings.TrimSuffix(string(f.Body), "\n"), "\n") {
			if line == "" {
				d.Line(0, "")
				continue
			}
			d.Line(2, "%s", line)
		}
	}
	return d.Bytes()
}

// runtimeConfig renders the DeploymentRuntimeConfig that mounts every
// template ConfigMap into the go-templating function pod, each under its
// ordered subdir of the templates dir.
func runtimeConfig(b *blueprint.Blueprint, groups int) []byte {
	dir := templatesDirPath(b)
	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Comment("Mounts the template ConfigMaps into the %s pod;", blueprint.TemplatingFunctionName)
	d.Comment("functions.yaml pins this via spec.runtimeConfigRef.")
	d.Line(0, "apiVersion: pkg.crossplane.io/v1beta1")
	d.Line(0, "kind: DeploymentRuntimeConfig")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s", runtimeConfigName(b))
	d.Line(0, "spec:")
	d.Line(1, "deploymentTemplate:")
	d.Line(2, "spec:")
	d.Line(3, "selector: {}")
	d.Line(3, "template:")
	d.Line(4, "spec:")
	d.Line(5, "containers:")
	d.Line(5, "- name: package-runtime")
	d.Line(6, "volumeMounts:")
	for i := 0; i < groups; i++ {
		d.Line(6, "- name: templates-%d", i)
		d.Line(7, "mountPath: %s/%d", dir, i)
		d.Line(7, "readOnly: true")
	}
	d.Line(5, "volumes:")
	for i := 0; i < groups; i++ {
		d.Line(5, "- name: templates-%d", i)
		d.Line(6, "configMap:")
		d.Line(7, "name: %s", templateCMName(b, i))
	}
	return d.Bytes()
}

func runtimeConfigName(b *blueprint.Blueprint) string {
	return b.Spec.XRD.Plural + "." + b.Spec.XRD.Group + "-templates"
}

// GenerateFS renders the FileSystem-source variant of the whole tree: the
// same XRD and providerconfigs, a Composition whose templating step reads
// from a mounted folder, the folder itself (one object per file), the
// ConfigMaps carrying it, and the DeploymentRuntimeConfig that mounts them.
// The render check keeps using the inline variant — `crossplane composition
// render` has no pod to mount ConfigMaps into.
func GenerateFS(b *blueprint.Blueprint, crds []schema.CRD, outDir string) ([]Output, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	name := b.Spec.XRD.Plural + "." + b.Spec.XRD.Group

	xrd, err := XRD(b)
	if err != nil {
		return nil, err
	}
	comp, err := CompositionFileSystem(b, crds, templatesDirPath(b))
	if err != nil {
		return nil, err
	}
	fns, err := functionsDoc(b, runtimeConfigName(b))
	if err != nil {
		return nil, err
	}
	pcs, err := ProviderConfigs(b, crds)
	if err != nil {
		return nil, err
	}
	files, err := TemplateFiles(b, crds)
	if err != nil {
		return nil, err
	}
	groups := packTemplateCMs(files)

	var out []Output
	out = append(out,
		Output{Path: filepath.Join(outDir, "compositions", name+".yaml"), Body: comp},
		Output{Path: filepath.Join(outDir, "functions.yaml"), Body: fns},
		Output{Path: filepath.Join(outDir, "xrds", name+".yaml"), Body: xrd},
		Output{Path: filepath.Join(outDir, "runtimeconfigs", name+".yaml"), Body: runtimeConfig(b, len(groups))},
	)
	for fam, body := range pcs {
		out = append(out, Output{Path: filepath.Join(outDir, "providerconfigs", fam+".yaml"), Body: body})
	}
	for _, f := range files {
		out = append(out, Output{Path: filepath.Join(outDir, "templates", name, f.Name), Body: f.Body})
	}
	for i, g := range groups {
		out = append(out, Output{
			Path: filepath.Join(outDir, "configmaps", fmt.Sprintf("%s-templates-%d.yaml", name, i)),
			Body: templateConfigMap(b, i, g),
		})
	}
	sortOutputs(out)
	return out, nil
}

func sortOutputs(out []Output) {
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Path < out[j-1].Path; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
}
