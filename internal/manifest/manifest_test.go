package manifest

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

func deploymentNodes(t *testing.T) []*schema.Node {
	t.Helper()
	return kindNodes(t, "Deployment")
}

func kindNodes(t *testing.T, kind string) []*schema.Node {
	t.Helper()
	kinds, err := k8s.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range kinds {
		if c.Kind == kind {
			nodes, err := c.FieldTree()
			if err != nil {
				t.Fatal(err)
			}
			return nodes
		}
	}
	t.Fatalf("no %s", kind)
	return nil
}

func starter() map[string]blueprint.Field {
	return map[string]blueprint.Field{
		"spec.replicas":                                               {From: "params.replicas"},
		"spec.selector.matchLabels[app]":                              {Value: "web"},
		"spec.template.metadata.labels[app]":                          {Value: "web"},
		"spec.template.spec.containers[0].name":                       {Value: "web"},
		"spec.template.spec.containers[0].image":                      {Value: "nginx:1.27"},
		"spec.template.spec.containers[0].ports[0].containerPort":     {Value: "80"},
		"spec.template.spec.containers[0].resources.requests[cpu]":    {Value: "100m"},
		"spec.template.spec.containers[0].resources.requests[memory]": {Value: "128Mi"},
		"spec.template.spec.containers[0].env[1].name":                {Value: "B"},
		"spec.template.spec.containers[0].env[0].name":                {Value: "A"},
		"spec.template.spec.containers[0].env[0].value":               {From: "resources.queue.status.atProvider.url"},
		"spec.template.spec.containers[0].command":                    {Value: "sh,-c"},
		"metadata.annotations[app.kubernetes.io/part-of]":             {Value: "shop"},
	}
}

func TestRenderNestsFieldsInManifestShape(t *testing.T) {
	out, err := Render(deploymentNodes(t), starter())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"spec:\n", "  replicas: {from: params.replicas}\n", "    matchLabels:\n      app: web\n",
		"      containers:\n        - command: [sh, -c]\n", "          image: nginx:1.27\n",
		"          ports:\n            - containerPort: 80\n", "            requests:\n              cpu: 100m\n",
		"          env:\n            - name: A\n              value: {from: resources.queue.status.atProvider.url}\n            - name: B\n",
		"metadata:\n  annotations:\n    app.kubernetes.io/part-of: shop\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered manifest lacks %q:\n%s", want, out)
		}
	}
}

func TestParseRoundTripsRender(t *testing.T) {
	nodes := deploymentNodes(t)
	in := starter()
	out, err := Render(nodes, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(nodes, out)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, out)
	}
	if len(got) != len(in) {
		t.Fatalf("round trip changed field count: got %d want %d\n%v", len(got), len(in), got)
	}
	for p, f := range in {
		if got[p] != f {
			t.Errorf("%s: got %+v want %+v", p, got[p], f)
		}
	}
}

func TestParseUnknownKeyReportsPathAndLine(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicas: 2\n  template:\n    spec:\n      containers:\n        - imagee: x\n")
	var me *Error
	if !asError(err, &me) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	if me.Path != "spec.template.spec.containers[0].imagee" || me.Line != 6 {
		t.Errorf("got path %q line %d, want containers[0].imagee at line 6", me.Path, me.Line)
	}
}

func TestParseScalarAtObjectIsAnError(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  selector: '{app: web}'\n")
	var me *Error
	if !asError(err, &me) || me.Path != "spec.selector" || me.Line != 2 {
		t.Fatalf("want error at spec.selector line 2, got %v", err)
	}
	if !strings.Contains(me.Error(), "{raw:") {
		t.Errorf("message should point at the explicit raw form: %s", me.Error())
	}
}

func TestParseExplicitRawAtMapNode(t *testing.T) {
	got, err := Parse(deploymentNodes(t), "spec:\n  selector:\n    matchLabels: {raw: \"{app: web}\"}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got["spec.selector.matchLabels"].Raw != "{app: web}" {
		t.Errorf("got %+v", got)
	}
}

func TestParseWrapperWithTwoKeysIsNotAWrapper(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicas: {from: params.a, value: 2}\n")
	if err == nil {
		t.Fatal("want error for a two-key wrapper")
	}
}

func TestParseNullLeavesUnset(t *testing.T) {
	got, err := Parse(deploymentNodes(t), "spec:\n  replicas:\n  paused: false\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["spec.replicas"]; ok {
		t.Error("null scalar must not create a field")
	}
	if got["spec.paused"].Value != "false" {
		t.Errorf("got %+v", got)
	}
}

func TestRenderEmptyFieldsIsEmptyDocument(t *testing.T) {
	out, err := Render(deploymentNodes(t), nil)
	if err != nil || strings.TrimSpace(out) != "{}" {
		t.Fatalf("got %q err %v", out, err)
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

// roundTrip renders fields and parses the text back, failing on any loss.
func roundTrip(t *testing.T, nodes []*schema.Node, in map[string]blueprint.Field) string {
	t.Helper()
	out, err := Render(nodes, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(nodes, out)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, out)
	}
	if len(got) != len(in) {
		t.Fatalf("round trip changed field count: got %d want %d\n%s\n%v", len(got), len(in), out, got)
	}
	for p, f := range in {
		if got[p] != f {
			t.Errorf("%s: got %+v want %+v\n%s", p, got[p], f, out)
		}
	}
	return out
}

// A map entry or object member whose key happens to be a wrapper key must
// not read back as a wrapper for its parent: Render writes the literal as
// {value: x} so Parse sees an entry.
func TestRenderSingleWrapperKeyedEntryStaysAnEntry(t *testing.T) {
	nodes := deploymentNodes(t)
	out := roundTrip(t, nodes, map[string]blueprint.Field{
		"metadata.annotations[from]":                    {Value: "x"},
		"spec.template.spec.containers[0].env[0].value": {Value: "only"},
	})
	for _, want := range []string{"    from: {value: x}\n", "            - value: {value: only}\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered manifest lacks %q:\n%s", want, out)
		}
	}
}

// A literal at a branch position (a whole map, object or list) is written as
// an explicit wrapper, since a bare scalar there is refused by Parse.
func TestRenderLiteralAtBranchIsAnExplicitWrapper(t *testing.T) {
	nodes := deploymentNodes(t)
	out := roundTrip(t, nodes, map[string]blueprint.Field{
		"spec.selector.matchLabels":              {Value: "whole"},
		"spec.template.spec.containers[1].env":   {Raw: "[{name: A, value: '1'}]"},
		"spec.template.spec.containers[1].image": {Value: "second"},
	})
	if !strings.Contains(out, "    matchLabels: {value: whole}\n") {
		t.Errorf("whole-map literal not wrapped:\n%s", out)
	}
	if !strings.Contains(out, "        - {}\n        - env: {raw:") {
		t.Errorf("gap element and raw list not rendered as expected:\n%s", out)
	}
}

// A numeric bracket under a map is a key, not an index (a ConfigMap's
// data[0] is the key "0"); keys that would read as another YAML type are
// quoted.
func TestNumericMapKeyIsAKeyNotAnIndex(t *testing.T) {
	nodes := deploymentNodes(t)
	out := roundTrip(t, nodes, map[string]blueprint.Field{
		"metadata.labels[0]":    {Value: "zero"},
		"metadata.labels[true]": {Value: "t"},
	})
	if !strings.Contains(out, "  labels:\n    \"0\": zero\n    \"true\": t\n") {
		t.Errorf("map keys not rendered as quoted strings:\n%s", out)
	}
}

// An integer literal at an IntOrString position reads unquoted, the way
// the emitter writes it; a name there stays a plain string.
func TestRenderIntOrStringIntegerIsUnquoted(t *testing.T) {
	nodes := kindNodes(t, "Service")
	out := roundTrip(t, nodes, map[string]blueprint.Field{
		"spec.ports[0].port":       {Value: "80"},
		"spec.ports[0].targetPort": {Value: "8080"},
		"spec.ports[1].port":       {Value: "443"},
		"spec.ports[1].targetPort": {Value: "https"},
	})
	if !strings.Contains(out, "      targetPort: 8080\n") || !strings.Contains(out, "      targetPort: https\n") {
		t.Errorf("unexpected targetPort rendering:\n%s", out)
	}
}

func TestRenderTypedScalarsAndLookalikeStrings(t *testing.T) {
	nodes := deploymentNodes(t)
	out := roundTrip(t, nodes, map[string]blueprint.Field{
		"spec.paused":                                      {Value: "true"},
		"spec.template.spec.containers[0].name":            {Value: "80"},
		"spec.template.spec.containers[0].image":           {Value: "true"},
		"spec.template.spec.containers[0].imagePullPolicy": {Value: ""},
	})
	for _, want := range []string{"  paused: true\n", "          name: \"80\"\n", "        - image: \"true\"\n", "          imagePullPolicy: \"\"\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered manifest lacks %q:\n%s", want, out)
		}
	}
}

func TestRenderRefusesAWholeValueBesideItsMembers(t *testing.T) {
	_, err := Render(deploymentNodes(t), map[string]blueprint.Field{
		"spec.selector":                  {Raw: "{matchLabels: {app: web}}"},
		"spec.selector.matchLabels[app]": {Value: "web"},
	})
	var me *Error
	if !asError(err, &me) || me.Path != "spec.selector.matchLabels[app]" {
		t.Fatalf("want a conflict error naming the member path, got %v", err)
	}
}

func TestParseDuplicateKeyIsAnError(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicas: 1\n  paused: true\n  replicas: 2\n")
	var me *Error
	if !asError(err, &me) || me.Path != "spec.replicas" || me.Line != 4 {
		t.Fatalf("want duplicate error at spec.replicas line 4, got %v", err)
	}
}

func TestParseScalarTypeMismatchReportsPathAndLine(t *testing.T) {
	for _, tc := range []struct {
		yaml, path string
		line       int
	}{
		{"spec:\n  replicas: two\n", "spec.replicas", 2},
		{"spec:\n  paused: yes\n", "spec.paused", 2},
		{"spec:\n  template:\n    spec:\n      containers:\n        - name: web\n          ports:\n            - containerPort: 80a\n", "spec.template.spec.containers[0].ports[0].containerPort", 7},
	} {
		_, err := Parse(deploymentNodes(t), tc.yaml)
		var me *Error
		if !asError(err, &me) || me.Path != tc.path || me.Line != tc.line {
			t.Errorf("%q: want error at %s line %d, got %v", tc.yaml, tc.path, tc.line, err)
		}
	}
	// A wrapper is not type-checked here: {value: two} at an integer leaf
	// is the CRD validation's call, the same as through the fields API.
	if _, err := Parse(deploymentNodes(t), "spec:\n  replicas: {value: two}\n"); err != nil {
		t.Errorf("wrapper must pass Parse untyped, got %v", err)
	}
}

func TestParseUnknownKeySuggestsTheClosestSibling(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicaz: 3\n")
	if err == nil || !strings.Contains(err.Error(), `did you mean "replicas"`) {
		t.Fatalf("want a sibling hint, got %v", err)
	}
}

func TestParseWrapperWithNullIsAnError(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicas: {from: }\n")
	var me *Error
	if !asError(err, &me) || me.Path != "spec.replicas" || me.Line != 2 {
		t.Fatalf("want error at spec.replicas line 2, got %v", err)
	}
}

func TestParseSyntaxErrorCarriesLine(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicas: 1\n bad: [\n")
	var me *Error
	if !asError(err, &me) || me.Line == 0 {
		t.Fatalf("want a syntax error with a line, got %v", err)
	}
}

func TestParseWholeDocumentForms(t *testing.T) {
	nodes := deploymentNodes(t)
	for _, text := range []string{"", "{}\n", "# nothing yet\n", "---\n"} {
		got, err := Parse(nodes, text)
		if err != nil || len(got) != 0 {
			t.Errorf("%q: got %v err %v, want no fields", text, got, err)
		}
	}
	if _, err := Parse(nodes, "- a\n"); err == nil {
		t.Error("a top-level list must be refused")
	}
}
