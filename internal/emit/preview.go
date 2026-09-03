package emit

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	sprig "github.com/Masterminds/sprig/v3"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

const (
	maxIncludeDepth       = 20
	maxOutputSize         = 1 << 20 // 1MB
	defaultPreviewTimeout = 5 * time.Second
)

type boundedWriter struct {
	buf bytes.Buffer
	max int
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		return 0, fmt.Errorf("preview output size exceeded maximum limit of %d bytes", w.max)
	}
	return w.buf.Write(p)
}

func (w *boundedWriter) String() string {
	return w.buf.String()
}

// PreviewExpression executes a Go template expression in-process against a
// synthetic context built from the blueprint's parameters, $xr metadata,
// and observed resource fixtures.
func PreviewExpression(b *blueprint.Blueprint, resourceName string, expr string) (string, error) {
	return PreviewExpressionContext(context.Background(), b, resourceName, expr)
}

// PreviewExpressionContext executes a Go template expression with context cancellation and deadline bounds.
func PreviewExpressionContext(ctx context.Context, b *blueprint.Blueprint, resourceName string, expr string) (string, error) {
	if strings.TrimSpace(expr) == "" {
		return "", nil
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultPreviewTimeout)
		defer cancel()
	}

	if b == nil {
		b = &blueprint.Blueprint{}
	}

	if resourceName != "" && b.ResourceNamed(resourceName) == nil {
		return "", fmt.Errorf("resource %q is not declared in blueprint", resourceName)
	}

	xrName := "sample-xr"
	if b.Spec.XRD.Kind != "" {
		xrName = "sample-" + strings.ToLower(b.Spec.XRD.Kind)
	}

	xrSpec := make(map[string]any)
	for name, p := range b.Spec.XRD.Parameters {
		xrSpec[name] = placeholderValue(p)
	}

	xrMeta := map[string]any{
		"name":      xrName,
		"namespace": "default",
		"labels": map[string]any{
			"app.kubernetes.io/name": xrName,
		},
		"annotations": map[string]any{},
		"uid":         "00000000-0000-0000-0000-000000000001",
	}

	observedResources := make(map[string]any)
	for _, r := range b.Spec.Resources {
		observedResources[r.Name] = map[string]any{
			"resource": map[string]any{
				"apiVersion": "example.org/v1alpha1",
				"kind":       r.Kind,
				"metadata": map[string]any{
					"name":      r.Name,
					"namespace": "default",
				},
				"spec": map[string]any{},
				"status": map[string]any{
					"atProvider": map[string]any{
						"id":  r.Name + "-id-12345",
						"arn": "arn:aws:service:region:123456789012:" + r.Name,
						"url": "https://" + r.Name + ".example.com",
					},
					"conditions": []any{
						map[string]any{
							"type":    "Ready",
							"status":  "True",
							"reason":  "Available",
							"message": "Resource is ready",
						},
					},
					"ready": true,
					"state": "AVAILABLE",
				},
			},
		}
	}

	envMap := make(map[string]any)
	for name, k := range b.Spec.Environment {
		envMap[name] = envPlaceholderValue(k)
	}

	data := map[string]any{
		"observed": map[string]any{
			"composite": map[string]any{
				"resource": map[string]any{
					"metadata": xrMeta,
					"spec":     xrSpec,
					"status": map[string]any{
						"conditions": []any{
							map[string]any{"type": "Ready", "status": "True"},
						},
					},
				},
			},
			"resources": observedResources,
		},
		"context": map[string]any{
			"apiextensions.crossplane.io/environment": envMap,
		},
		"spec":     xrSpec,
		"xr":       xrName,
		"xrMeta":   xrMeta,
		"resource": resourceName,
	}

	var lines []string
	tmplNames := make([]string, 0, len(b.Spec.Templates))
	for n := range b.Spec.Templates {
		tmplNames = append(tmplNames, n)
	}
	sort.Strings(tmplNames)
	for _, n := range tmplNames {
		lines = append(lines, blueprint.TemplateBlockLines(n, b.Spec.Templates[n])...)
	}

	lines = append(lines,
		"{{- $spec := .observed.composite.resource.spec -}}",
		"{{- $xr := .observed.composite.resource.metadata.name -}}",
		"{{- $xrMeta := .observed.composite.resource.metadata -}}",
		"{{- $observed := .observed.resources -}}",
		`{{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}`,
		"{{- $i := 0 -}}",
		fmt.Sprintf("{{- $resource := %q -}}", resourceName),
		expr,
	)

	tmplBody := strings.Join(lines, "\n")

	tmpl := template.New("preview").Option("missingkey=error")
	funcs := sprig.TxtFuncMap()
	delete(funcs, "env")
	delete(funcs, "expandenv")

	funcs["until"] = func(count int) ([]int, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if count < 0 {
			return nil, fmt.Errorf("negative count %d", count)
		}
		if count > 10000 {
			return nil, fmt.Errorf("until count %d exceeds maximum limit of 10000", count)
		}
		out := make([]int, count)
		for i := 0; i < count; i++ {
			out[i] = i
		}
		return out, nil
	}

	funcs["untilStep"] = func(start, stop, step int) ([]int, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if step == 0 {
			return nil, fmt.Errorf("untilStep with step 0 would loop indefinitely")
		}
		if (step > 0 && start > stop) || (step < 0 && start < stop) {
			return []int{}, nil
		}
		count := (stop - start) / step
		if count < 0 {
			count = -count
		}
		if count > 10000 {
			return nil, fmt.Errorf("untilStep iterations %d exceed maximum limit of 10000", count)
		}
		var out []int
		if step > 0 {
			for i := start; i < stop; i += step {
				out = append(out, i)
			}
		} else {
			for i := start; i > stop; i += step {
				out = append(out, i)
			}
		}
		return out, nil
	}

	funcs["randomChoice"] = func(args ...any) any {
		if len(args) == 0 {
			return ""
		}
		if len(args) == 1 {
			if slice, ok := args[0].([]any); ok && len(slice) > 0 {
				return slice[0]
			}
			if slice, ok := args[0].([]string); ok && len(slice) > 0 {
				return slice[0]
			}
		}
		return args[0]
	}

	funcs["toYaml"] = func(v any) (string, error) {
		b, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	funcs["fromYaml"] = func(s string) (map[string]any, error) {
		out := map[string]any{}
		err := yaml.Unmarshal([]byte(s), &out)
		return out, err
	}

	funcs["getResourceCondition"] = func(condType string, res any) map[string]any {
		if resMap, ok := res.(map[string]any); ok {
			if status, ok := resMap["status"].(map[string]any); ok {
				if conds, ok := status["conditions"].([]any); ok {
					for _, c := range conds {
						if cm, ok := c.(map[string]any); ok && cm["type"] == condType {
							return cm
						}
					}
				}
			}
		}
		return map[string]any{
			"type":    condType,
			"status":  "True",
			"reason":  "Available",
			"message": "Resource is ready",
		}
	}

	funcs["setResourceNameAnnotation"] = func(name string) string {
		return fmt.Sprintf("crossplane.io/composition-resource-name: %s", name)
	}

	funcs["getComposedResource"] = func(name string, observed any) any {
		if obsMap, ok := observed.(map[string]any); ok {
			if resMap, ok := obsMap["resources"].(map[string]any); ok {
				return resMap[name]
			}
		}
		return nil
	}

	funcs["getCompositeResource"] = func(observed any) any {
		if obsMap, ok := observed.(map[string]any); ok {
			return obsMap["composite"]
		}
		return nil
	}

	funcs["getExtraResources"] = func(name string, extra any) any {
		if extraMap, ok := extra.(map[string]any); ok {
			return extraMap[name]
		}
		return nil
	}

	funcs["getExtraResourcesFromContext"] = func(name string, ctx any) any {
		return nil
	}

	funcs["getCredentialData"] = func(args ...any) any {
		return map[string]any{}
	}

	includeDepth := 0
	funcs["include"] = func(name string, data any) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if includeDepth >= maxIncludeDepth {
			return "", fmt.Errorf("maximum template include depth (%d) exceeded", maxIncludeDepth)
		}
		includeDepth++
		defer func() { includeDepth-- }()
		var buf boundedWriter
		buf.max = maxOutputSize
		err := tmpl.ExecuteTemplate(&buf, name, data)
		return buf.String(), err
	}

	tmpl, err := tmpl.Funcs(funcs).Parse(tmplBody)
	if err != nil {
		return "", err
	}

	type execResult struct {
		rendered string
		err      error
	}

	resCh := make(chan execResult, 1)
	go func() {
		var buf boundedWriter
		buf.max = maxOutputSize
		err := tmpl.Execute(&buf, data)
		resCh <- execResult{rendered: buf.String(), err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resCh:
		return res.rendered, res.err
	}
}
