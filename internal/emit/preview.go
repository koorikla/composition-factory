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
	if err := ctx.Err(); err != nil {
		return "", err
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
		xrSpec[name] = previewPlaceholderValue(p)
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

	funcs["fromYaml"] = func(s string) (any, error) {
		var out any
		err := yaml.Unmarshal([]byte(s), &out)
		return out, err
	}

	funcs["getResourceCondition"] = func(a, b any) map[string]any {
		var condType string
		var res any
		if s, ok := a.(string); ok {
			condType = s
			res = b
		} else if s, ok := b.(string); ok {
			condType = s
			res = a
		} else {
			return nil
		}

		if resMap, ok := res.(map[string]any); ok {
			if inner, ok := resMap["resource"].(map[string]any); ok {
				resMap = inner
			}
			if status, ok := resMap["status"].(map[string]any); ok {
				if conds, ok := status["conditions"].([]any); ok {
					for _, c := range conds {
						if cm, ok := c.(map[string]any); ok && (cm["type"] == condType || cm["Type"] == condType) {
							out := make(map[string]any, len(cm)*2)
							for k, v := range cm {
								out[k] = v
								if len(k) > 0 {
									titleK := strings.ToUpper(k[:1]) + k[1:]
									out[titleK] = v
								}
							}
							return out
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
			"Type":    condType,
			"Status":  "True",
			"Reason":  "Available",
			"Message": "Resource is ready",
		}
	}

	funcs["setResourceNameAnnotation"] = func(name string) string {
		return fmt.Sprintf("crossplane.io/composition-resource-name: %s", name)
	}

	funcs["getComposedResource"] = func(a, b any) any {
		var req any
		var name string
		if s, ok := a.(string); ok {
			name = s
			req = b
		} else if s, ok := b.(string); ok {
			name = s
			req = a
		} else {
			return nil
		}

		reqMap, ok := req.(map[string]any)
		if !ok {
			return nil
		}

		extract := func(entry any) any {
			if entryMap, ok := entry.(map[string]any); ok {
				if res, ok := entryMap["resource"].(map[string]any); ok {
					return res
				}
				return entryMap
			}
			return entry
		}

		if obs, ok := reqMap["observed"].(map[string]any); ok {
			if resMap, ok := obs["resources"].(map[string]any); ok {
				if entry, ok := resMap[name]; ok {
					return extract(entry)
				}
				return nil
			}
		}
		if resMap, ok := reqMap["resources"].(map[string]any); ok {
			if entry, ok := resMap[name]; ok {
				return extract(entry)
			}
			return nil
		}
		if entry, ok := reqMap[name]; ok {
			return extract(entry)
		}
		return nil
	}

	funcs["getCompositeResource"] = func(req any) any {
		reqMap, ok := req.(map[string]any)
		if !ok {
			return nil
		}
		if obs, ok := reqMap["observed"].(map[string]any); ok {
			if comp, ok := obs["composite"].(map[string]any); ok {
				if res, ok := comp["resource"].(map[string]any); ok {
					return res
				}
				return comp
			}
		}
		if comp, ok := reqMap["composite"].(map[string]any); ok {
			if res, ok := comp["resource"].(map[string]any); ok {
				return res
			}
			return comp
		}
		if res, ok := reqMap["resource"].(map[string]any); ok {
			return res
		}
		return nil
	}

	funcs["getExtraResources"] = func(a, b any) any {
		var req any
		var name string
		if s, ok := a.(string); ok {
			name = s
			req = b
		} else if s, ok := b.(string); ok {
			name = s
			req = a
		} else {
			return nil
		}
		if reqMap, ok := req.(map[string]any); ok {
			if extra, ok := reqMap["extraResources"].(map[string]any); ok {
				if items, ok := extra[name]; ok {
					return items
				}
			}
			if extra, ok := reqMap["extra"].(map[string]any); ok {
				if items, ok := extra[name]; ok {
					return items
				}
			}
			return reqMap[name]
		}
		return nil
	}

	funcs["getExtraResourcesFromContext"] = func(args ...any) any {
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
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return res.rendered, res.err
	}
}
