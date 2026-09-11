# CF-245 — README local-binary quickstart wrote-output example omits the providerconfigs/ line cf gen actually prints

## Severity & Scope
- **Severity**: P3
- **Scale**: engine
- **Touch set**: `README.md`

## Background & Problem
In `README.md:176-183`, the "Quickstart (Local Binary)" step 3 shows illustrative output for `bin/cf gen testdata/xqueue.cf.yaml -o out`:
```
# wrote out/compositions/xqueues.platform.sparky.ee.yaml
# wrote out/functions.yaml
# wrote out/xrds/xqueues.platform.sparky.ee.yaml
```
Running the command actually prints:
```
wrote out/compositions/xqueues.platform.sparky.ee.yaml
wrote out/functions.yaml
wrote out/providerconfigs/aws.yaml
wrote out/xrds/xqueues.platform.sparky.ee.yaml
```
The documentation omitted the ProviderConfig output line.

## Expected Behavior & Contract
Update `README.md` to include `# wrote out/providerconfigs/aws.yaml` in the step 3 output example.
