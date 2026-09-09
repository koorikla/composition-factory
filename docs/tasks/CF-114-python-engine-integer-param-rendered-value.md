# CF-114 — The python engine renders an integer parameter wired into a string map as `"8080.0"` instead of `"8080"`

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 (engine scale: emits wrong output silently; wrong value reaches the cluster without error) |
| **Closes** | `CF-114 — *(engine)* The python engine renders an integer parameter wired into a string map as PORT: "8080.0"; go-templating and kcl render "8080".` |
| **Worktree** | `.worktrees/CF-114` on branch `CF-114-python-engine-integer-param-rendered-value` |
| **May write** | `internal/emit/python.go`, `internal/emit/quoting_test.go` |
| **Merges after** | nothing |

## Symptom

When an integer parameter is wired into a string field or string map (such as `PORT` in ConfigMap data in `k8s-workload.cf.yaml`), the python engine (`--engine python`) renders it as `PORT: "8080.0"`. Go-templating and KCL engines render `"8080"`.

The Kubernetes API server accepts strings like `"8080.0"` for ConfigMap keys and environment variables without error, so the wrong string value reaches the cluster silently and breaks applications expecting integer port numbers.

## Evidence

In `internal/emit/python.go:31`:
```python
oxr = MessageToDict(req.observed.composite.resource)
```
In protobuf python, `MessageToDict` converts protobuf `struct_pb2.Value` instances into Python dicts. In protobuf, all numbers in a `Struct` are stored as IEEE 754 64-bit floating point numbers (`number_value`). Consequently, every number from the composite resource arrives in Python as a `float` (e.g., `8080.0`).

In `python.go:230`:
```go
if s.targetType == "string" && s.sourceType != "" && s.sourceType != "string" {
    return fmt.Sprintf("str(%s)", expr)
}
```
Emitted Python code:
```python
"PORT": str(spec.get("port"))
```
In Python, `str(8080.0)` evaluates to `"8080.0"`.

Existing guard `TestK8sWorkloadConfigMapPortQuotedAcrossEngines` in `internal/emit/quoting_test.go:281` checks:
```go
if !strings.Contains(s, `"PORT": str(spec.get("port"))`) { ... }
```
which asserted the buggy emitted syntax rather than correct integer-string formatting or rendered value.

## Location

- `internal/emit/python.go:29` — script header where helpers like `_present` are declared.
- `internal/emit/python.go:229-232` — integer-to-string parameter mapping.
- `internal/emit/quoting_test.go:274-285` — test asserting python string emission.

## Acceptance test

Write this test **first**, verbatim in `internal/emit/quoting_test.go`, and watch it fail before changing production code.

```go
func TestCF114PythonEngineIntegerParamStringFormatting(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}

	b, err := blueprint.Load("../examples/k8s-workload.cf.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}

	out, err := Composition(b, native)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)

	// An integer parameter wired into a string target must not be converted via plain str(val)
	// because MessageToDict delivers numbers as floats (8080.0 -> "8080.0").
	if strings.Contains(s, `"PORT": str(spec.get("port"))`) {
		t.Errorf("Python engine emitted uncoerced str(spec.get(\"port\")), which formats float as 8080.0:\n%s", s)
	}
	if !strings.Contains(s, `_str(spec.get("port"))`) && !strings.Contains(s, `str(int(spec.get("port")))`) {
		t.Errorf("Python engine expected integer-safe string conversion for port, got:\n%s", s)
	}
}
```

**Fails today with:**

```
--- FAIL: TestCF114PythonEngineIntegerParamStringFormatting (0.01s)
    quoting_test.go:305: Python engine emitted uncoerced str(spec.get("port")), which formats float as 8080.0
FAIL
```

## Contract

- When generating Python function scripts, a parameter or status field mapped to a string target must format whole numbers without fractional `.0`.
- The helper `_str(v)` or integer conversion must convert `8080.0` (and `8080`) to `"8080"` while properly handling `None` (returning `None` so `_present` drops missing optional parameters).
- `TestK8sWorkloadConfigMapPortQuotedAcrossEngines` and `TestCF114PythonEngineIntegerParamStringFormatting` must both pass cleanly.
- Go-templating and KCL engines remain unaffected.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- KCL or Go-template engine changes.

## Handover

Branch `CF-114-python-engine-integer-param-rendered-value`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
