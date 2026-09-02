package manifest

import (
	"bytes"
	"fmt"
	"sort"
)

// Edit represents a byte replacement splice on the raw document.
type Edit struct {
	Start      int
	End        int
	NewContent string
}

// Apply applies a sequence of splice edits to the raw manifest bytes.
// If edits is empty, it returns the exact raw byte-for-byte content.
func Apply(m *CompositionManifest, edits ...Edit) ([]byte, error) {
	if len(edits) == 0 {
		buf := make([]byte, len(m.Raw))
		copy(buf, m.Raw)
		return buf, nil
	}

	// Validate and sort edits ascending by Start offset
	sorted := make([]Edit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})

	var out bytes.Buffer
	cursor := 0

	for i, e := range sorted {
		if e.Start < 0 || e.End > len(m.Raw) || e.Start > e.End {
			return nil, fmt.Errorf("invalid edit range [%d, %d) for buffer of size %d", e.Start, e.End, len(m.Raw))
		}
		if e.Start < cursor {
			return nil, fmt.Errorf("overlapping edits at byte %d (edit %d)", e.Start, i)
		}

		out.Write(m.Raw[cursor:e.Start])
		out.WriteString(e.NewContent)
		cursor = e.End
	}

	if cursor < len(m.Raw) {
		out.Write(m.Raw[cursor:])
	}

	return out.Bytes(), nil
}

// SetFieldLiteral creates an edit replacing or inserting a literal field.
func SetFieldLiteral(res *ParsedResource, key, value string) (Edit, error) {
	if f, ok := res.Fields[key]; ok {
		return Edit{
			Start:      f.Span.Start,
			End:        f.Span.End,
			NewContent: fmt.Sprintf("%s%s: '%s'", f.Indent, key, value),
		}, nil
	}
	// Fallback: append field to resource
	return Edit{
		Start:      res.Span.End,
		End:        res.Span.End,
		NewContent: fmt.Sprintf("\n    %s: '%s'", key, value),
	}, nil
}

// SetFieldWire creates an edit replacing or inserting a parameter wire.
func SetFieldWire(res *ParsedResource, key, param string, optional bool) (Edit, error) {
	if f, ok := res.Fields[key]; ok {
		if optional {
			return Edit{
				Start: f.Span.Start,
				End:   f.Span.End,
				NewContent: fmt.Sprintf("%s{{- if hasKey $spec %q }}\n%s%s: {{ $spec.%s }}\n%s{{- end }}",
					f.Indent, param, f.Indent, key, param, f.Indent),
			}, nil
		}
		return Edit{
			Start:      f.Span.Start,
			End:        f.Span.End,
			NewContent: fmt.Sprintf("%s%s: {{ $spec.%s }}", f.Indent, key, param),
		}, nil
	}
	return Edit{
		Start:      res.Span.End,
		End:        res.Span.End,
		NewContent: fmt.Sprintf("\n    %s: {{ $spec.%s }}", key, param),
	}, nil
}

// DeleteResource creates an edit removing a resource document.
func DeleteResource(res *ParsedResource) Edit {
	return Edit{
		Start:      res.Span.Start,
		End:        res.Span.End,
		NewContent: "",
	}
}
