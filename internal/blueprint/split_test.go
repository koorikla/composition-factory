package blueprint

import (
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
