package emit

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

// fsBlueprint is testBlueprint in FileSystem template-source mode, plus a
// user template so the context file has a define block to carry.
func fsBlueprint() *blueprint.Blueprint {
	b := testBlueprint()
	b.Spec.Templates = map[string]string{"cf.tags": "team: platform\nxr: {{ .xr | quote }}\n"}
	b.Spec.Resources[0].Fields["tags"] = blueprint.Field{Template: "cf.tags"}
	b.Spec.Emit = &blueprint.Emit{TemplateSource: blueprint.TemplateSourceFileSystem}
	return b
}

const fsName = "xqueues.platform.sparky.ee"

// fsCRDs provides CRD definitions with both maxMessageSize and tags.
func fsCRDs(t *testing.T) []schema.CRD {
	t.Helper()
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  maxMessageSize: {type: integer}
                  tags:
                    type: object
                    additionalProperties: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                properties:
                  region: {type: string}
                  maxMessageSize: {type: integer}
                  tags:
                    type: object
                    additionalProperties: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

func outputByPath(t *testing.T, outs []Output, rel string) Output {
	t.Helper()
	for _, o := range outs {
		if filepath.ToSlash(o.Path) == rel {
			return o
		}
	}
	var paths []string
	for _, o := range outs {
		paths = append(paths, o.Path)
	}
	t.Fatalf("no output at %q; have %v", rel, paths)
	return Output{}
}

func TestFileSystemOutputPaths(t *testing.T) {
	outs, err := Generate(fsBlueprint(), fsCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var got []string
	for _, o := range outs {
		got = append(got, filepath.ToSlash(o.Path))
	}
	want := []string{
		"out/compositions/" + fsName + ".yaml",
		"out/functions.yaml",
		"out/runtime/" + fsName + ".yaml",
		"out/templates/" + fsName + "/000-context.yaml",
		"out/templates/" + fsName + "/001-main-queue.yaml",
		"out/xrds/" + fsName + ".yaml",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("output paths (-want +got):\n%s", diff)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("outputs must stay path-sorted (positional diffs between runs): %v", got)
	}
}

func TestFileSystemCompositionStep(t *testing.T) {
	outs, err := Generate(fsBlueprint(), fsCRDs(t), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	comp := string(outputByPath(t, outs, "compositions/"+fsName+".yaml").Body)
	for _, want := range []string{
		"      source: FileSystem\n",
		"      options: [\"missingkey=error\"]\n",
		"      fileSystem:\n        dirPath: /templates/" + fsName + "\n",
	} {
		if !strings.Contains(comp, want) {
			t.Errorf("composition missing %q\n---\n%s", want, comp)
		}
	}
	for _, bad := range []string{"inline:", "template: |", "source: Inline", "{{"} {
		if strings.Contains(comp, bad) {
			t.Errorf("FileSystem composition must not carry %q\n---\n%s", bad, comp)
		}
	}
	// the step is still followed by the effective pipeline's after-steps
	if !strings.Contains(comp, "  - step: auto-ready\n") {
		t.Errorf("auto-ready step missing after the templating step\n---\n%s", comp)
	}
}

func TestFileSystemFunctionsCarryRuntimeConfigRef(t *testing.T) {
	fns, err := Functions(fsBlueprint())
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	docs := strings.Split(string(fns), "\n---\n")
	if len(docs) != 2 {
		t.Fatalf("functions.yaml has %d docs, want 2:\n%s", len(docs), fns)
	}
	if !strings.Contains(docs[0], "name: function-go-templating") ||
		!strings.Contains(docs[0], "runtimeConfigRef:") ||
		!strings.Contains(docs[0], "name: function-go-templating") {
		t.Errorf("go-templating Function must reference the DeploymentRuntimeConfig\n---\n%s", docs[0])
	}
	if strings.Contains(docs[1], "runtimeConfigRef") {
		t.Errorf("only the templating function mounts templates; auto-ready must not gain a ref\n---\n%s", docs[1])
	}

	inline, err := Functions(testBlueprint())
	if err != nil {
		t.Fatalf("Functions (inline): %v", err)
	}
	if strings.Contains(string(inline), "runtimeConfigRef") {
		t.Errorf("inline mode must stay byte-identical to before: no runtimeConfigRef\n---\n%s", inline)
	}
}

// inlineTemplateBody extracts the go-templating step's block-scalar body
// from an Inline-mode Composition, de-indented: the lines under
// `template: |` up to the next pipeline step, minus the block's 10-space
// indent (blank lines stay blank).
func inlineTemplateBodyExtract(t *testing.T, comp string) string {
	t.Helper()
	lines := strings.Split(comp, "\n")
	start := -1
	for i, l := range lines {
		if l == "        template: |" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("no `template: |` in composition:\n%s", comp)
	}
	var body []string
	for _, l := range lines[start:] {
		if l == "" {
			body = append(body, "")
			continue
		}
		if !strings.HasPrefix(l, "          ") {
			break
		}
		body = append(body, l[10:])
	}
	return strings.TrimRight(strings.Join(body, "\n"), "\n") + "\n"
}

// TestFileSystemFilesEqualInlineTemplate is the byte-equivalence proof: the
// template files, concatenated in lexical order, ARE the inline template —
// so whatever the inline composition renders, the mounted folder renders
// too (function-go-templating parses the folder as one template, file after
// file, exactly this concatenation plus a harmless separator).
func TestFileSystemFilesEqualInlineTemplate(t *testing.T) {
	inlineBP := fsBlueprint()
	inlineBP.Spec.Emit = nil
	inlineComp, err := Composition(inlineBP, fsCRDs(t))
	if err != nil {
		t.Fatalf("Composition (inline): %v", err)
	}
	want := inlineTemplateBodyExtract(t, string(inlineComp))

	outs, err := Generate(fsBlueprint(), fsCRDs(t), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var files []Output
	for _, o := range outs {
		if strings.HasPrefix(filepath.ToSlash(o.Path), "templates/") {
			files = append(files, o)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	var got bytes.Buffer
	for _, f := range files {
		got.Write(f.Body)
	}
	if diff := cmp.Diff(want, got.String()); diff != "" {
		t.Errorf("template files != inline template body (-inline +files):\n%s", diff)
	}
	// each file is a whole, LF-terminated document fragment of its own
	for _, f := range files {
		if !bytes.HasSuffix(f.Body, []byte("\n")) || bytes.HasSuffix(f.Body, []byte("\n\n")) {
			t.Errorf("%s must end with exactly one newline", f.Path)
		}
	}
	ctx := string(outputByPath(t, outs, "templates/"+fsName+"/000-context.yaml").Body)
	if !strings.HasPrefix(ctx, "{{- define \"cf.tags\" }}\n") || !strings.Contains(ctx, "{{- $spec := ") {
		t.Errorf("context file must carry the define blocks and the $spec/$xr assignments:\n%s", ctx)
	}
	res := string(outputByPath(t, outs, "templates/"+fsName+"/001-main-queue.yaml").Body)
	if !strings.HasPrefix(res, "---\napiVersion: sqs.aws.m.upbound.io/v1beta1\nkind: Queue\n") {
		t.Errorf("resource file must be that resource's document, separator first:\n%s", res)
	}
}

// runtimeDocs splits the runtime file into its parsed documents.
func runtimeDocs(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var docs []map[string]any
	for _, raw := range bytes.Split(body, []byte("\n---\n")) {
		var m map[string]any
		if err := yaml.Unmarshal(raw, &m); err != nil {
			t.Fatalf("runtime doc does not parse: %v\n---\n%s", err, raw)
		}
		docs = append(docs, m)
	}
	return docs
}

func configMapData(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()
	data, _ := doc["data"].(map[string]any)
	out := map[string]string{}
	for k, v := range data {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("ConfigMap data %q is %T, want string", k, v)
		}
		out[k] = s
	}
	return out
}

func TestFileSystemRuntimeShape(t *testing.T) {
	outs, err := Generate(fsBlueprint(), fsCRDs(t), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rt := outputByPath(t, outs, "runtime/"+fsName+".yaml").Body
	s := string(rt)
	for _, want := range []string{
		"# Generated by compositionfactory. Do not edit.\n",
		"kind: ConfigMap\n",
		"  name: " + fsName + "-templates-0\n",
		"  namespace: crossplane-system\n",
		"  000-context.yaml: |\n",
		"  001-main-queue.yaml: |\n",
		"kind: DeploymentRuntimeConfig\n",
		"  name: function-go-templating\n",
		"      selector: {}\n",
		"          - name: package-runtime\n",
		"              mountPath: /templates/" + fsName + "\n",
		"              readOnly: true\n",
		"            projected:\n",
		"                  name: " + fsName + "-templates-0\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("runtime file missing %q\n---\n%s", want, s)
		}
	}

	docs := runtimeDocs(t, rt)
	if len(docs) != 2 || docs[0]["kind"] != "ConfigMap" || docs[1]["kind"] != "DeploymentRuntimeConfig" {
		t.Fatalf("runtime file must be [ConfigMap, DeploymentRuntimeConfig], got %d docs", len(docs))
	}
	// the ConfigMap keys ARE the template files: the mount must reproduce
	// them byte for byte, so the block scalars must round-trip exactly
	data := configMapData(t, docs[0])
	for _, o := range outs {
		p := filepath.ToSlash(o.Path)
		if !strings.HasPrefix(p, "templates/") {
			continue
		}
		key := filepath.Base(p)
		if data[key] != string(o.Body) {
			t.Errorf("ConfigMap data[%s] != file body (-file +data):\n%s", key, cmp.Diff(string(o.Body), data[key]))
		}
		delete(data, key)
	}
	if len(data) != 0 {
		t.Errorf("ConfigMap carries keys that are not template files: %v", data)
	}
}

// bigBlueprint carries n resources each rendering a value of about size
// bytes, so the template files overflow one ConfigMap's budget.
func bigBlueprint(n, size int) *blueprint.Blueprint {
	b := fsBlueprint()
	b.Spec.Resources = nil
	for i := 0; i < n; i++ {
		b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
			Name: fmt.Sprintf("queue-%d", i), Kind: "Queue",
			Fields: map[string]blueprint.Field{
				"region": {Value: strings.Repeat("x", size)},
			},
		})
	}
	return b
}

func TestFileSystemPacksConfigMapsUnderBudget(t *testing.T) {
	// three ~400 KiB files against a 900 KiB budget: [context+0+1] then [2]
	outs, err := Generate(bigBlueprint(3, 400<<10), fsCRDs(t), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	docs := runtimeDocs(t, outputByPath(t, outs, "runtime/"+fsName+".yaml").Body)
	if len(docs) != 3 {
		t.Fatalf("want 2 ConfigMaps + 1 DeploymentRuntimeConfig, got %d docs", len(docs))
	}
	seen := map[string]int{}
	for i, d := range docs[:2] {
		if d["kind"] != "ConfigMap" {
			t.Fatalf("doc %d kind %v, want ConfigMap", i, d["kind"])
		}
		meta := d["metadata"].(map[string]any)
		if meta["name"] != fmt.Sprintf("%s-templates-%d", fsName, i) {
			t.Errorf("ConfigMap %d name %v", i, meta["name"])
		}
		total := 0
		for k, v := range configMapData(t, d) {
			seen[k]++
			total += len(k) + len(v)
		}
		if total > 900<<10 {
			t.Errorf("ConfigMap %d carries %d bytes, over the 900 KiB budget", i, total)
		}
	}
	for _, key := range []string{"000-context.yaml", "001-queue-0.yaml", "002-queue-1.yaml", "003-queue-2.yaml"} {
		if seen[key] != 1 {
			t.Errorf("template %s appears in %d ConfigMaps, want exactly 1", key, seen[key])
		}
	}
	// the one projected volume lists every ConfigMap, in order
	drc, err := yaml.Marshal(docs[2])
	if err != nil {
		t.Fatal(err)
	}
	s := string(drc)
	if i0, i1 := strings.Index(s, fsName+"-templates-0"), strings.Index(s, fsName+"-templates-1"); i0 < 0 || i1 < 0 || i1 < i0 {
		t.Errorf("DeploymentRuntimeConfig must project both ConfigMaps in order:\n%s", s)
	}
}

func TestFileSystemRefusesOversizedTemplateFile(t *testing.T) {
	_, err := Generate(bigBlueprint(1, 1<<20), fsCRDs(t), "")
	if err == nil {
		t.Fatal("a single template file over the ConfigMap budget must be refused")
	}
	for _, want := range []string{"queue-0", "ConfigMap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestInlineModeUnchangedByEmitKey: an explicit templateSource: Inline is
// byte-identical to an absent spec.emit — the goldens already pin the
// absent case; this pins that "Inline" is not a third shape.
func TestInlineModeUnchangedByEmitKey(t *testing.T) {
	absent, err := Generate(testBlueprint(), fsCRDs(t), "")
	if err != nil {
		t.Fatal(err)
	}
	b := testBlueprint()
	b.Spec.Emit = &blueprint.Emit{TemplateSource: blueprint.TemplateSourceInline}
	explicit, err := Generate(b, fsCRDs(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(absent, explicit); diff != "" {
		t.Errorf("templateSource: Inline differs from absent (-absent +explicit):\n%s", diff)
	}
}

func TestFileSystemGuardedResourcesSelfContained(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xmulti"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.0.0"}},
			XRD: blueprint.XRD{
				Group: "platform.example.org", Kind: "XMulti", Plural: "xmultis",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"enableQueue":  {Type: "boolean", Required: true, Default: "true"},
					"enableAudit":  {Type: "boolean", Required: true, Default: "false"},
					"replicaCount": {Type: "integer", Required: true, Default: "2"},
				},
			},
			Emit: &blueprint.Emit{TemplateSource: blueprint.TemplateSourceFileSystem},
			Resources: []blueprint.Resource{
				{
					Name:     "primary-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.0.0",
					When:     "params.enableQueue",
					Fields: map[string]blueprint.Field{
						"region": {Value: "us-east-1"},
					},
				},
				{
					Name:     "audit-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.0.0",
					When:     "params.enableAudit",
					Fields: map[string]blueprint.Field{
						"region": {Value: "us-west-2"},
					},
				},
				{
					Name:     "worker-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.0.0",
					ForEach:  "params.replicaCount",
					Fields: map[string]blueprint.Field{
						"region": {Value: "eu-west-1"},
					},
				},
			},
		},
	}
	crds := fsCRDs(t)
	files, err := TemplateFiles(b, crds)
	if err != nil {
		t.Fatalf("TemplateFiles failed: %v", err)
	}

	if len(files) != 4 {
		t.Fatalf("expected 4 template files (context + 3 resources), got %d", len(files))
	}

	// 000-context.yaml must NOT contain any {{- if or {{- end tags
	ctxBody := string(files[0].Body)
	if strings.Contains(ctxBody, "{{- if") || strings.Contains(ctxBody, "{{- end") {
		t.Errorf("context file must not contain resource when guards:\n%s", ctxBody)
	}

	// Each resource file must be self-contained and closed
	for i, f := range files[1:] {
		body := string(f.Body)
		openCount := strings.Count(body, "{{- if") + strings.Count(body, "{{- range")
		closeCount := strings.Count(body, "{{- end")
		if openCount != closeCount {
			t.Errorf("file %s has unbalanced template control blocks (%d opens, %d ends):\n%s", f.Name, openCount, closeCount, body)
		}
		if i == 0 { // primary-queue with When
			if !strings.HasPrefix(body, "{{- if $spec.enableQueue }}\n---\n") {
				t.Errorf("primary-queue must start with its own when guard:\n%s", body)
			}
			if !strings.HasSuffix(body, "{{- end }}\n") {
				t.Errorf("primary-queue must end with its closing {{- end }}:\n%s", body)
			}
		}
		if i == 1 { // audit-queue with When
			if !strings.HasPrefix(body, "{{- if $spec.enableAudit }}\n---\n") {
				t.Errorf("audit-queue must start with its own when guard:\n%s", body)
			}
			if !strings.HasSuffix(body, "{{- end }}\n") {
				t.Errorf("audit-queue must end with its closing {{- end }}:\n%s", body)
			}
		}
		if i == 2 { // worker-queue with ForEach
			if !strings.HasPrefix(body, "{{- range $i := until (int $spec.replicaCount) }}\n---\n") {
				t.Errorf("worker-queue must start with its own forEach range:\n%s", body)
			}
			if !strings.HasSuffix(body, "{{- end }}\n") {
				t.Errorf("worker-queue must end with its closing {{- end }}:\n%s", body)
			}
		}
	}
}
