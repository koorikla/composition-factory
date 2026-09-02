package blueprint

import (
	"bytes"
)

// SplitDocs splits a multi-document YAML stream on column-zero "---" document separators.
// It handles leading "---", trailing "---", Windows (CRLF) line endings, and ignores empty documents.
func SplitDocs(in []byte) [][]byte {
	var docs [][]byte
	lines := bytes.Split(in, []byte("\n"))
	var current bytes.Buffer

	for _, line := range lines {
		trimmed := bytes.TrimRight(line, "\r")
		// Document separator must be at column 0 (no leading whitespace)
		if bytes.HasPrefix(trimmed, []byte("---")) && len(bytes.TrimSpace(trimmed)) == 3 {
			if docBytes := bytes.TrimSpace(current.Bytes()); len(docBytes) > 0 {
				cp := make([]byte, len(docBytes))
				copy(cp, docBytes)
				docs = append(docs, cp)
			}
			current.Reset()
			continue
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.Write(trimmed)
	}

	if docBytes := bytes.TrimSpace(current.Bytes()); len(docBytes) > 0 {
		cp := make([]byte, len(docBytes))
		copy(cp, docBytes)
		docs = append(docs, cp)
	}
	return docs
}
