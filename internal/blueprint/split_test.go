package blueprint

import (
	"bytes"
	"reflect"
	"testing"
)

func TestSplitDocs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single doc without separators",
			in:   "foo: bar\nbaz: qux\n",
			want: []string{"foo: bar\nbaz: qux"},
		},
		{
			name: "leading separator",
			in:   "---\nfoo: bar\n",
			want: []string{"foo: bar"},
		},
		{
			name: "trailing separator",
			in:   "foo: bar\n---\n",
			want: []string{"foo: bar"},
		},
		{
			name: "multiple docs with standard separators",
			in:   "doc: 1\n---\ndoc: 2\n---\ndoc: 3",
			want: []string{"doc: 1", "doc: 2", "doc: 3"},
		},
		{
			name: "windows CRLF separators",
			in:   "---\r\ndoc: 1\r\n---\r\ndoc: 2\r\n---\r\n",
			want: []string{"doc: 1", "doc: 2"},
		},
		{
			name: "consecutive separators and empty docs",
			in:   "---\n\n---\ndoc: 1\n---\n---\ndoc: 2\n---\n",
			want: []string{"doc: 1", "doc: 2"},
		},
		{
			name: "comments and inline content",
			in:   "# leading comment\ndoc: 1\n# middle comment\nfield: value\n---\n# doc 2\ndoc: 2\n",
			want: []string{"# leading comment\ndoc: 1\n# middle comment\nfield: value", "# doc 2\ndoc: 2"},
		},
		{
			name: "trailing separator without newline",
			in:   "doc: 1\n---",
			want: []string{"doc: 1"},
		},
		{
			name: "separator with trailing spaces and tabs",
			in:   "doc: 1\n---   \ndoc: 2\n---\t\ndoc: 3\n",
			want: []string{"doc: 1", "doc: 2", "doc: 3"},
		},
		{
			name: "lines with dashes that are not separators",
			in:   "doc: 1\n----not-sep\nkey: ---value\n  ---\ndoc: 1-end\n---\ndoc: 2\n",
			want: []string{"doc: 1\n----not-sep\nkey: ---value\n  ---\ndoc: 1-end", "doc: 2"},
		},
		{
			name: "windows CRLF multi-line documents",
			in:   "---\r\ndoc: 1\r\nkey: val1\r\n---\r\ndoc: 2\r\nkey: val2\r\n",
			want: []string{"doc: 1\nkey: val1", "doc: 2\nkey: val2"},
		},
		{
			name: "back to back leading separators",
			in:   "---\n---\ndoc: 1\n",
			want: []string{"doc: 1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitDocs([]byte(tc.in))
			var gotStr []string
			for _, g := range got {
				gotStr = append(gotStr, string(g))
			}
			if !reflect.DeepEqual(gotStr, tc.want) {
				t.Errorf("SplitDocs(%q) = %q, want %q", tc.in, gotStr, tc.want)
			}
		})
	}
}

func TestSplitDocs_StreamAllocations(t *testing.T) {
	// Construct a multi-document stream with 30,000 lines across 5 documents.
	const numDocs = 5
	const entriesPerDoc = 2000
	var buf bytes.Buffer
	for d := 0; d < numDocs; d++ {
		if d > 0 {
			buf.WriteString("---\n")
		}
		for l := 0; l < entriesPerDoc; l++ {
			buf.WriteString("kind: CustomResourceDefinition\nmetadata:\n  name: test.example.org\n")
		}
	}
	stream := buf.Bytes()

	// Verify correctness first
	docs := SplitDocs(stream)
	if len(docs) != numDocs {
		t.Fatalf("expected %d docs, got %d", numDocs, len(docs))
	}

	// An offset-scanning implementation should only allocate once per document
	// (for the copied document bytes) plus small slice overhead for the docs slice,
	// rather than thousands of allocations for line-splitting and buffer growth.
	allocs := testing.AllocsPerRun(10, func() {
		_ = SplitDocs(stream)
	})

	// With numDocs=5, scanning offsets directly requires <= 10 allocations.
	// The naive bytes.Split(in, "\n") implementation requires > 60 allocations
	// due to line-slice headers and bytes.Buffer growth.
	if allocs > 10 {
		t.Errorf("SplitDocs allocated %v times on large stream, want <= 10 (detected line-splitting or buffer growth)", allocs)
	}
}

func BenchmarkSplitDocs(b *testing.B) {
	const numDocs = 5
	const entriesPerDoc = 2000
	var buf bytes.Buffer
	for d := 0; d < numDocs; d++ {
		if d > 0 {
			buf.WriteString("---\n")
		}
		for l := 0; l < entriesPerDoc; l++ {
			buf.WriteString("kind: CustomResourceDefinition\nmetadata:\n  name: test.example.org\n")
		}
	}
	stream := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = SplitDocs(stream)
	}
}

func BenchmarkSplitDocs_7MB(b *testing.B) {
	const numDocs = 10
	const linesPerDoc = 14000
	var buf bytes.Buffer
	for d := 0; d < numDocs; d++ {
		if d > 0 {
			buf.WriteString("---\n")
		}
		for l := 0; l < linesPerDoc; l++ {
			buf.WriteString("apiVersion: apiextensions.k8s.io/v1\n")
		}
	}
	stream := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = SplitDocs(stream)
	}
}
