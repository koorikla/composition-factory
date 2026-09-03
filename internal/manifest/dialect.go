package manifest

import (
	"regexp"
)

var (
	// PreludeRe matches the standard composite prelude in Go-template body.
	PreludeRe = regexp.MustCompile(`(?s)^\s*(\{\{-\s*\$spec\s*:=\s*\.observed\.composite\.resource\.spec\s*-\}\}\s*\{\{-\s*\$xr\s*:=\s*\.observed\.composite\.resource\.metadata\.name\s*-\}\}\s*\{\{-\s*\$xrMeta\s*:=\s*\.observed\.composite\.resource\.metadata\s*-\}\}\s*)`)

	// DefineBlockRe matches define helper blocks.
	DefineBlockRe = regexp.MustCompile(`(?s)\{\{-\s*define\s+"([^"]+)"\s*-\}\}(.*?)\{\{-\s*end\s*-\}\}`)

	// SetResourceNameRe matches the Crossplane composition resource naming helper.
	SetResourceNameRe = regexp.MustCompile(`\{\{\s*setResourceNameAnnotation\s+"([^"]+)"\s*\}\}`)

	// DirectWireRe matches un-guarded parameter wires: key: {{ $spec.param }}
	DirectWireRe = regexp.MustCompile(`^(\s*)([a-zA-Z0-9_.-]+):\s*\{\{\s*\$spec\.([a-zA-Z0-9_.-]+)\s*\}\}$`)

	// GuardedWireRe matches guarded optional parameter wires:
	// {{- if hasKey $spec "param" }}\n  key: {{ $spec.param }}\n{{- end }}
	GuardedWireRe = regexp.MustCompile(`(?m)^(\s*)\{\{-\s*if\s+hasKey\s+\$spec\s+"([^"]+)"\s*\}\}\n(\s*)([a-zA-Z0-9_.-]+):\s*\{\{\s*\$spec\.[a-zA-Z0-9_.-]+\s*\}\}\n\s*\{\{-\s*end\s*\}\}`)

	// StatusWireRe matches status-atProvider wires:
	// {{- if hasKey (dig "resources" "res" "resource" "status" "atProvider" dict $.observed) "field" }}\n  key: {{ (index $.observed.resources "res").resource.status.atProvider.field }}\n{{- end }}
	// Also matches legacy 11-term guard: {{- if and (hasKey $.observed.resources "res") ... }}
	StatusWireRe = regexp.MustCompile(`(?s)^(\s*)\{\{-\s*if\s+(?:hasKey\s+\(dig\s+"resources"\s+"([^"]+)"\s+"resource"\s+"status"\s+"atProvider"\s+dict\s+\$\.observed\)\s+"[^"]+"|and\s+\([^)]*hasKey\s+\$\.observed\.resources\s+"([^"]+)"[^)]*\).*?)\s*\}\}\n(\s*)([a-zA-Z0-9_.-]+):\s*\{\{\s*\(index\s+\$\.observed\.resources\s+"[^"]+"\)\.resource\.status\.atProvider\.([a-zA-Z0-9_.-]+)\s*\}\}\n\s*\{\{-\s*end\s*\}\}`)

	// LiteralFieldRe matches key: 'val', key: 123, key: true
	LiteralFieldRe = regexp.MustCompile(`^(\s*)([a-zA-Z0-9_.-]+):\s*('([^']*)'|"([^"]*)"|true|false|-?\d+(\.\d+)?|\[.*?\]|\{.*?\})$`)
)
