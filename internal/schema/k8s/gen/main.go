// Command gen (re)generates the vendored Kubernetes OpenAPI subset that
// internal/schema/k8s embeds. It is a maintainer tool, not a runtime
// dependency: cf itself NEVER fetches Kubernetes schemas over the network —
// the vendored files are the only schema source for native kinds, and this
// command is the only thing that ever writes them.
//
// What it does, per API group (apps/v1, batch/v1, core v1):
//
//  1. Read the upstream OpenAPI v3 document for the group, either from
//     --src <dir> (a directory holding the upstream files under their
//     upstream names) or, by default, fetched from the pinned Kubernetes
//     tag on raw.githubusercontent.com. The pin is the single k8sVersion
//     constant below; changing the vendored Kubernetes version means
//     changing that constant and re-running this command.
//  2. Compute the transitive $ref closure of the group's root kinds (the
//     native kinds cf composes: Deployment, StatefulSet, DaemonSet, Service,
//     ConfigMap, Secret, ServiceAccount, Job, CronJob). Each upstream group
//     document is self-contained — every $ref it uses resolves inside its
//     own components.schemas — so the closure never crosses files.
//  3. Write internal/schema/k8s/openapi_<group>.json holding ONLY the
//     closure's schemas, each carried as raw JSON exactly as upstream
//     published it (whitespace normalized by re-encoding; content
//     untouched), plus the version and source URL it came from, so the
//     vendored file is auditable against upstream byte content with jq.
//
// Everything lossy — $ref resolution, allOf flattening, int-or-string
// normalization — happens at load time in internal/schema/k8s, in tested Go,
// never here: the vendored artifact stays as close to upstream as pruning
// allows.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// k8sVersion is the pinned upstream Kubernetes release the vendored subset
// is extracted from. internal/schema/k8s refuses to load a vendored file
// whose recorded k8sVersion disagrees with its own pin, so bumping this
// constant without regenerating (or vice versa) fails loudly at first use.
const k8sVersion = "v1.34.1"

const baseURL = "https://raw.githubusercontent.com/kubernetes/kubernetes/" + k8sVersion + "/api/openapi-spec/v3/"

// groups maps each upstream OpenAPI group document to the vendored file it
// produces and the root kinds whose $ref closure is kept.
var groups = []struct {
	upstream string   // upstream file name under api/openapi-spec/v3/
	out      string   // vendored file name under internal/schema/k8s/
	roots    []string // components.schemas names whose closure is vendored
}{
	{
		upstream: "apis__apps__v1_openapi.json",
		out:      "openapi_apps_v1.json",
		roots: []string{
			"io.k8s.api.apps.v1.Deployment",
			"io.k8s.api.apps.v1.StatefulSet",
			"io.k8s.api.apps.v1.DaemonSet",
		},
	},
	{
		upstream: "apis__batch__v1_openapi.json",
		out:      "openapi_batch_v1.json",
		roots: []string{
			"io.k8s.api.batch.v1.Job",
			"io.k8s.api.batch.v1.CronJob",
		},
	},
	{
		upstream: "api__v1_openapi.json",
		out:      "openapi_core_v1.json",
		roots: []string{
			"io.k8s.api.core.v1.Service",
			"io.k8s.api.core.v1.ConfigMap",
			"io.k8s.api.core.v1.Secret",
			"io.k8s.api.core.v1.ServiceAccount",
			"io.k8s.api.core.v1.PersistentVolumeClaim",
		},
	},
	{
		upstream: "apis__networking.k8s.io__v1_openapi.json",
		out:      "openapi_networking_v1.json",
		roots: []string{
			"io.k8s.api.networking.v1.Ingress",
			"io.k8s.api.networking.v1.NetworkPolicy",
		},
	},
	{
		upstream: "apis__autoscaling__v2_openapi.json",
		out:      "openapi_autoscaling_v2.json",
		roots: []string{
			"io.k8s.api.autoscaling.v2.HorizontalPodAutoscaler",
		},
	},
	{
		upstream: "apis__policy__v1_openapi.json",
		out:      "openapi_policy_v1.json",
		roots: []string{
			"io.k8s.api.policy.v1.PodDisruptionBudget",
		},
	},
	{
		upstream: "apis__rbac.authorization.k8s.io__v1_openapi.json",
		out:      "openapi_rbac_v1.json",
		roots: []string{
			"io.k8s.api.rbac.v1.Role",
			"io.k8s.api.rbac.v1.RoleBinding",
		},
	},
}

// upstreamDoc mirrors only what the generator reads from an upstream
// OpenAPI v3 document. Schemas stay raw so the vendored output carries each
// schema's upstream content verbatim rather than a Go round-trip of it.
type upstreamDoc struct {
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

// vendoredFile is the on-disk shape of one vendored group file. It must
// stay in sync with the identically named struct in internal/schema/k8s.
//
// Source is always the canonical pinned upstream URL, even when the bytes
// were read from --src: the vendored file's provenance is the upstream
// document, and --src is only a transport detail for machines where this
// process cannot dial out. SourceSHA256 is the digest of the complete
// upstream document (not the pruned subset), so `curl <source> | shasum -a
// 256` verifies that what was pruned really was the pinned upstream file.
type vendoredFile struct {
	K8sVersion   string                     `json:"k8sVersion"`
	Source       string                     `json:"source"`
	SourceSHA256 string                     `json:"sourceSha256"`
	Schemas      map[string]json.RawMessage `json:"schemas"`
}

func main() {
	src := flag.String("src", "", "directory holding the upstream OpenAPI files under their upstream names; empty means fetch from the pinned tag")
	out := flag.String("out", "internal/schema/k8s", "directory to write the vendored files into")
	flag.Parse()

	if err := run(*src, *out); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}

func run(src, out string) error {
	for _, g := range groups {
		raw, err := readUpstream(src, g.upstream)
		if err != nil {
			return err
		}
		var doc upstreamDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s: %w", g.upstream, err)
		}

		kept, err := closure(doc.Components.Schemas, g.roots)
		if err != nil {
			return fmt.Errorf("%s: %w", g.upstream, err)
		}

		body, err := json.MarshalIndent(vendoredFile{
			K8sVersion:   k8sVersion,
			Source:       baseURL + g.upstream,
			SourceSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)),
			Schemas:      kept,
		}, "", " ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", g.out, err)
		}
		path := filepath.Join(out, g.out)
		if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s: %d of %d schemas, %d bytes\n", path, len(kept), len(doc.Components.Schemas), len(body)+1)
	}
	return nil
}

// readUpstream returns the upstream document's bytes: fetched from the
// pinned tag by default, or read from --src, which must hold byte-identical
// copies of the pinned upstream files under their upstream names (the
// recorded sourceSha256 is what makes a divergent copy detectable later).
func readUpstream(src, name string) ([]byte, error) {
	if src != "" {
		return os.ReadFile(filepath.Join(src, name))
	}
	url := baseURL + name
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	return raw, nil
}

// closure returns the subset of schemas reachable from roots by following
// $ref, roots included. A root missing from the upstream document is an
// error: silently vendoring 8 of 9 kinds is exactly the kind of quiet
// wrongness the rest of this project refuses.
func closure(schemas map[string]json.RawMessage, roots []string) (map[string]json.RawMessage, error) {
	kept := make(map[string]json.RawMessage)
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, done := kept[name]; done {
			continue
		}
		raw, ok := schemas[name]
		if !ok {
			return nil, fmt.Errorf("schema %q is referenced but not present upstream", name)
		}
		kept[name] = raw

		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("schema %q: %w", name, err)
		}
		refs := collectRefs(decoded, nil)
		sort.Strings(refs) // deterministic traversal, deterministic errors
		queue = append(queue, refs...)
	}
	return kept, nil
}

// collectRefs gathers every "$ref" schema name in v, depth-first.
func collectRefs(v any, refs []string) []string {
	switch t := v.(type) {
	case map[string]any:
		for k, elem := range t {
			if k == "$ref" {
				if s, ok := elem.(string); ok {
					refs = append(refs, strings.TrimPrefix(s, "#/components/schemas/"))
					continue
				}
			}
			refs = collectRefs(elem, refs)
		}
	case []any:
		for _, elem := range t {
			refs = collectRefs(elem, refs)
		}
	}
	return refs
}
