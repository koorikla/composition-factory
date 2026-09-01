// End-to-end HTTP coverage for native Kubernetes kinds: the index the
// server serves them from is built the way cmd/cf/serve.go builds it
// (provider CRDs plus k8s.Kinds() under the "k8s" label), so these tests
// exercise the same wiring production uses rather than a native-only toy.
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// nativeTestBlueprintYAML extends the standard test blueprint with a native
// Deployment whose container image comes from a parameter — the acceptance
// shape, over HTTP.
const nativeTestBlueprintYAML = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xwebapp
spec:
  sources:
    - provider: ghcr.io/x/provider-aws-sqs:v2.7.0
  xrd:
    group: platform.sparky.ee
    kind: XWebApp
    plural: xwebapps
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      image: {type: string, required: true}
  resources:
    - name: main-queue
      kind: Queue
      provider: ghcr.io/x/provider-aws-sqs:v2.7.0
      fields:
        region: {value: eu-north-1}
    - name: web
      kind: Deployment
      provider: k8s
      fields:
        spec.template.spec.containers[0].name: {value: web}
        spec.template.spec.containers[0].image: {from: params.image}
`

// nativeTestHandler builds the server over an index that includes the
// vendored native kinds — the exact byProvider shape cmd/cf/serve.go's
// startup assembles — and the native blueprint above.
func nativeTestHandler(t *testing.T) http.Handler {
	t.Helper()
	byProvider := testFixtureCRDs(t)
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	byProvider[blueprint.NativeProvider] = native
	idx, err := index.Build(byProvider)
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}

	o := testServerOptions(t)
	o.Index = idx

	path := filepath.Join(t.TempDir(), "xwebapp.cf.yaml")
	if err := os.WriteFile(path, []byte(nativeTestBlueprintYAML), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}
	if _, err := blueprint.Load(path); err != nil {
		t.Fatalf("native blueprint fixture does not validate: %v", err)
	}
	o.Blueprint = path

	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestKindsListsNativeKindsUnderTheK8sProvider(t *testing.T) {
	h := nativeTestHandler(t)
	var got struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds?q=deployment", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(got.Kinds) != 1 {
		t.Fatalf("kinds = %+v, want exactly the native Deployment", got.Kinds)
	}
	k := got.Kinds[0]
	if k.Provider != blueprint.NativeProvider {
		t.Errorf("Provider = %q, want %q — label, don't hide", k.Provider, blueprint.NativeProvider)
	}
	if k.APIVersion != "apps/v1" {
		t.Errorf("APIVersion = %q, want apps/v1", k.APIVersion)
	}
	if k.Fields == 0 {
		t.Error("Fields = 0; the native settable tree must be counted")
	}
}

// A native kind's detail: identity plus an EMPTY envelope (the composed
// object has no Crossplane wrapper), and its fields route serves the nested
// pod-template hierarchy. The core group's bare "v1" apiVersion is also
// exercised — its path segment carries no slash at all.
func TestNativeKindDetailAndFields(t *testing.T) {
	h := nativeTestHandler(t)

	var detail struct {
		Kind     index.Kind    `json:"kind"`
		Envelope []index.Field `json:"envelope"`
	}
	if code := getJSON(t, h, "/api/kinds/apps%2Fv1/Deployment", &detail); code != 200 {
		t.Fatalf("GET kind detail: status %d", code)
	}
	if len(detail.Envelope) != 0 {
		t.Errorf("Deployment envelope = %+v, want empty — a native object has no Crossplane envelope", detail.Envelope)
	}

	var fields struct {
		Fields []index.Field `json:"fields"`
		Total  int           `json:"total"`
	}
	if code := getJSON(t, h, "/api/kinds/apps%2Fv1/Deployment/fields?prefix=spec.template.spec.containers%5B0%5D", &fields); code != 200 {
		t.Fatalf("GET fields: status %d", code)
	}
	var sawImage bool
	for _, f := range fields.Fields {
		if f.Path == "spec.template.spec.containers[0].image" {
			sawImage = true
			if f.Type != "string" {
				t.Errorf("image field type = %q, want string", f.Type)
			}
		}
		if !strings.HasPrefix(f.Path, "spec.template.spec.containers[0]") {
			t.Errorf("prefix filter leaked %q", f.Path)
		}
	}
	if !sawImage {
		t.Errorf("fields under containers[0] do not include image; got %d fields", len(fields.Fields))
	}

	var core struct {
		Kind     index.Kind    `json:"kind"`
		Envelope []index.Field `json:"envelope"`
	}
	if code := getJSON(t, h, "/api/kinds/v1/ConfigMap", &core); code != 200 {
		t.Fatalf("GET core kind detail: status %d", code)
	}
	if core.Kind.APIVersion != "v1" {
		t.Errorf("ConfigMap APIVersion = %q, want bare v1", core.Kind.APIVersion)
	}
}

// POST /api/generate over the native blueprint: the emitted Composition
// carries the Deployment as the object itself. providerConfigRef must appear
// exactly once — the managed Queue's — and never under the Deployment.
func TestGenerateEmitsNativeResourceWithoutEnvelope(t *testing.T) {
	h := nativeTestHandler(t)
	rec := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	var comp string
	for _, want := range []string{"apiVersion: apps/v1", "kind: Deployment"} {
		if !strings.Contains(body, want) {
			t.Errorf("generate response missing %q", want)
		}
	}
	comp = body
	if got := strings.Count(comp, "providerConfigRef"); got != 1 {
		t.Errorf("providerConfigRef appears %d times, want exactly 1 (the Queue's; never on the native Deployment)", got)
	}
	if got := strings.Count(comp, "forProvider"); got != 1 {
		t.Errorf("forProvider appears %d times, want exactly 1 (the Queue's)", got)
	}
}
