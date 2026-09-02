package manifest

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseComposition parses a Crossplane Composition YAML document into a structured manifest
// with exact byte ranges into the underlying raw buffer.
func ParseComposition(raw []byte) (*CompositionManifest, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("failed to parse composition YAML: %w", err)
	}

	manifest := &CompositionManifest{
		Raw:       raw,
		Resources: make([]*ParsedResource, 0),
	}

	if len(root.Content) == 0 {
		return manifest, nil
	}

	topNode := root.Content[0]
	if topNode.Kind != yaml.MappingNode {
		return manifest, nil
	}

	// Extract metadata.name and spec.compositeTypeRef
	for i := 0; i < len(topNode.Content); i += 2 {
		k := topNode.Content[i].Value
		v := topNode.Content[i+1]
		if k == "metadata" && v.Kind == yaml.MappingNode {
			for j := 0; j < len(v.Content); j += 2 {
				if v.Content[j].Value == "name" {
					manifest.Name = v.Content[j+1].Value
				}
			}
		}
		if k == "spec" && v.Kind == yaml.MappingNode {
			for j := 0; j < len(v.Content); j += 2 {
				if v.Content[j].Value == "compositeTypeRef" && v.Content[j+1].Kind == yaml.MappingNode {
					ctr := v.Content[j+1]
					for m := 0; m < len(ctr.Content); m += 2 {
						if ctr.Content[m].Value == "apiVersion" {
							parts := strings.Split(ctr.Content[m+1].Value, "/")
							manifest.Group = parts[0]
						}
						if ctr.Content[m].Value == "kind" {
							manifest.XRDKind = ctr.Content[m+1].Value
						}
					}
				}
			}
		}
	}

	// Find template string and its offset in raw
	tmplText, tmplOffset, found := extractTemplate(raw, topNode)
	if !found {
		return manifest, nil
	}

	manifest.TemplateSpan = Span{
		Start: tmplOffset,
		End:   tmplOffset + len(tmplText),
		Raw:   tmplText,
	}

	// Parse template body
	parseTemplateBody(manifest, tmplText, tmplOffset)

	return manifest, nil
}

func extractTemplate(raw []byte, topNode *yaml.Node) (string, int, bool) {
	// Locate template in raw by looking for "template: |" or walking the pipeline nodes
	idx := bytes.Index(raw, []byte("template: |"))
	if idx == -1 {
		idx = bytes.Index(raw, []byte("template: >"))
	}
	if idx == -1 {
		return "", 0, false
	}

	// Move past "template: |\n" or "template: |\r\n"
	lineEnd := bytes.IndexByte(raw[idx:], '\n')
	if lineEnd == -1 {
		return "", 0, false
	}
	contentStart := idx + lineEnd + 1

	// Determine indentation of the template block from the first non-empty line
	tmplBytes := raw[contentStart:]
	return string(tmplBytes), contentStart, true
}

var (
	docSplitRe = regexp.MustCompile(`(?m)^\s*---(\s*$)`)
	apiKindRe  = regexp.MustCompile(`(?m)^(\s*)apiVersion:\s*([^\n]+)\n\s*kind:\s*([^\n]+)`)
)

func parseTemplateBody(m *CompositionManifest, tmpl string, baseOffset int) {
	offset := 0

	// 1. Match prelude
	if loc := PreludeRe.FindStringIndex(tmpl); loc != nil && loc[0] == 0 {
		m.PreludeSpan = Span{
			Start: baseOffset,
			End:   baseOffset + loc[1],
			Raw:   tmpl[:loc[1]],
		}
		offset = loc[1]
	}

	// 2. Match define blocks
	defMatches := DefineBlockRe.FindAllStringIndex(tmpl[offset:], -1)
	for _, match := range defMatches {
		s := offset + match[0]
		e := offset + match[1]
		m.Defines = append(m.Defines, Span{
			Start: baseOffset + s,
			End:   baseOffset + e,
			Raw:   tmpl[s:e],
		})
	}
	if len(defMatches) > 0 {
		last := defMatches[len(defMatches)-1]
		offset += last[1]
	}

	// 3. Match resource documents split by "---"
	body := tmpl[offset:]
	docIndices := docSplitRe.FindAllStringIndex(body, -1)
	if len(docIndices) == 0 {
		return
	}

	for i, dIdx := range docIndices {
		docStart := offset + dIdx[0]
		var docEnd int
		if i+1 < len(docIndices) {
			docEnd = offset + docIndices[i+1][0]
		} else {
			docEnd = len(tmpl)
		}

		docText := tmpl[docStart:docEnd]
		res := parseResourceDoc(docText, baseOffset+docStart)
		if res != nil {
			m.Resources = append(m.Resources, res)
		}
	}
}

func parseResourceDoc(docText string, baseOffset int) *ParsedResource {
	res := &ParsedResource{
		Span: Span{
			Start: baseOffset,
			End:   baseOffset + len(docText),
			Raw:   docText,
		},
		Fields: make(map[string]*ParsedField),
	}

	// Extract resource name
	nameMatches := SetResourceNameRe.FindStringSubmatch(docText)
	if len(nameMatches) > 1 {
		res.Name = nameMatches[1]
	}

	// Extract apiVersion & kind
	akMatches := apiKindRe.FindStringSubmatch(docText)
	if len(akMatches) > 3 {
		res.APIVersion = strings.TrimSpace(akMatches[2])
		res.Kind = strings.TrimSpace(akMatches[3])
	}

	// Parse fields
	lines := strings.Split(docText, "\n")
	lineOffset := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" || trimmed == "---" {
			lineOffset += len(line) + 1
			continue
		}

		if wireMatch := DirectWireRe.FindStringSubmatch(line); len(wireMatch) > 3 {
			k := wireMatch[2]
			res.Fields[k] = &ParsedField{
				Key:    k,
				Indent: wireMatch[1],
				Form:   FormParameterWire,
				Value:  wireMatch[3],
				Span: Span{
					Start: baseOffset + lineOffset,
					End:   baseOffset + lineOffset + len(line),
					Raw:   line,
				},
			}
		} else if litMatch := LiteralFieldRe.FindStringSubmatch(line); len(litMatch) > 3 {
			k := litMatch[2]
			res.Fields[k] = &ParsedField{
				Key:    k,
				Indent: litMatch[1],
				Form:   FormLiteral,
				Value:  litMatch[3],
				Span: Span{
					Start: baseOffset + lineOffset,
					End:   baseOffset + lineOffset + len(line),
					Raw:   line,
				},
			}
		}
		lineOffset += len(line) + 1
	}

	// Check for guarded wires
	guardedMatches := GuardedWireRe.FindAllStringSubmatchIndex(docText, -1)
	for _, gm := range guardedMatches {
		gText := docText[gm[0]:gm[1]]
		param := docText[gm[4]:gm[5]]
		key := docText[gm[8]:gm[9]]
		indent := docText[gm[6]:gm[7]]
		res.Fields[key] = &ParsedField{
			Key:    key,
			Indent: indent,
			Form:   FormGuardedWire,
			Value:  param,
			Guard:  fmt.Sprintf("hasKey $spec %q", param),
			Span: Span{
				Start: baseOffset + gm[0],
				End:   baseOffset + gm[1],
				Raw:   gText,
			},
		}
	}

	return res
}
