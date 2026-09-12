package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func seedDeployment(t *testing.T, h http.Handler) {
	t.Helper()
	res := blueprint.Resource{Name: "web", Kind: "Deployment", Provider: "k8s", Fields: map[string]blueprint.Field{
		"spec.replicas":                          {Value: "2"},
		"spec.selector.matchLabels[app]":         {Value: "web"},
		"spec.template.metadata.labels[app]":     {Value: "web"},
		"spec.template.spec.containers[0].name":  {Value: "web"},
		"spec.template.spec.containers[0].image": {Value: "nginx:1.27"},
	}}
	body, _ := json.Marshal(res)
	if rec := do(t, h, "POST", "/api/blueprint/resources", string(body)); rec.Code != 200 {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body)
	}
}

func TestGetResourceManifestRendersNestedYAML(t *testing.T) {
	h := testHandler(t)
	seedDeployment(t, h)
	rec := do(t, h, "GET", "/api/blueprint/resources/web/manifest", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.YAML, "      containers:\n        - image: nginx:1.27\n") {
		t.Errorf("unexpected yaml:\n%s", out.YAML)
	}
}

func TestPutResourceManifestReplacesFields(t *testing.T) {
	h, path := testHandlerWithPath(t)
	seedDeployment(t, h)
	y := "spec:\n  replicas: 3\n  selector:\n    matchLabels:\n      app: web\n  template:\n    metadata:\n      labels:\n        app: web\n    spec:\n      containers:\n        - name: web\n          image: nginx:1.27\n          ports:\n            - containerPort: 80\n            - containerPort: 9090\n"
	body, _ := json.Marshal(map[string]string{"yaml": y})
	rec := do(t, h, "PUT", "/api/blueprint/resources/web/manifest", string(body))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	f := b.ResourceNamed("web").Fields
	if f["spec.replicas"].Value != "3" || f["spec.template.spec.containers[0].ports[1].containerPort"].Value != "9090" {
		t.Errorf("fields not replaced: %+v", f)
	}
}

func TestPutResourceManifestUnknownKeyIs400WithPathAndLine(t *testing.T) {
	h := testHandler(t)
	seedDeployment(t, h)
	body, _ := json.Marshal(map[string]string{"yaml": "spec:\n  replicas: 3\n  replicaz: 4\n"})
	rec := do(t, h, "PUT", "/api/blueprint/resources/web/manifest", string(body))
	if rec.Code != 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var e struct {
		Error string `json:"error"`
		Path  string `json:"path"`
		Line  int    `json:"line"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Path != "spec.replicaz" || e.Line != 3 || !strings.Contains(e.Error, "unknown field") {
		t.Errorf("got %+v", e)
	}
}

func TestPutResourceManifestUnknownResourceIs404(t *testing.T) {
	h := testHandler(t)
	body, _ := json.Marshal(map[string]string{"yaml": "spec: {}\n"})
	if rec := do(t, h, "PUT", "/api/blueprint/resources/nope/manifest", string(body)); rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
}

// A wrapper's literal is not type-checked by the manifest parser (it has no
// line to blame for a {value: ...} the fields API would accept just the
// same), so a bad integer inside one is exactly the CRD validation's catch:
// the same validation PUT /api/blueprint/resources/{name} runs.
func TestPutResourceManifestRunsCRDValidation(t *testing.T) {
	h := testHandler(t)
	body, _ := json.Marshal(map[string]string{"yaml": "region: eu-north-1\nmaxMessageSize: {value: notanumber}\n"})
	rec := do(t, h, "PUT", "/api/blueprint/resources/main-queue/manifest", string(body))
	if rec.Code != 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "not a valid integer") {
		t.Errorf("error should come from CRD validation: %s", rec.Body)
	}
}

func TestGetResourceManifestUnknownResourceIs404(t *testing.T) {
	h := testHandler(t)
	if rec := do(t, h, "GET", "/api/blueprint/resources/nope/manifest", ""); rec.Code != 404 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

// A syntax error has a line but no field path: the body still carries
// both keys so the editor can rely on their presence, with path "".
func TestPutResourceManifestSyntaxErrorBodyShape(t *testing.T) {
	h := testHandler(t)
	seedDeployment(t, h)
	body, _ := json.Marshal(map[string]string{"yaml": "spec: [\n"})
	rec := do(t, h, "PUT", "/api/blueprint/resources/web/manifest", string(body))
	if rec.Code != 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var e map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	msg, _ := e["error"].(string)
	path, hasPath := e["path"]
	line, _ := e["line"].(float64)
	if !strings.HasPrefix(msg, "line 1: ") || !hasPath || path != "" || line != 1 {
		t.Errorf("got %s", rec.Body)
	}
}
