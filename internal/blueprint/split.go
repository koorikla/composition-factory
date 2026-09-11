package blueprint

import (
	"bytes"
)

// isSeparator checks if the line starting at sepStart is a column-zero YAML document separator:
// "---" followed only by optional trailing whitespace (' ', '\t', '\r') until '\n' or EOF.
// It returns whether it is a separator and the offset of the line's end ('\n' or len(in)).
func isSeparator(in []byte, sepStart int) (bool, int) {
	if sepStart+3 > len(in) || in[sepStart] != '-' || in[sepStart+1] != '-' || in[sepStart+2] != '-' {
		return false, 0
	}
	i := sepStart + 3
	isSep := true
	for i < len(in) && in[i] != '\n' {
		b := in[i]
		if b != ' ' && b != '\t' && b != '\r' {
			isSep = false
		}
		i++
	}
	return isSep, i
}

// SplitDocs splits a multi-document YAML stream on column-zero "---" document separators.
// It handles leading "---", trailing "---", Windows (CRLF) line endings, and ignores empty documents.
func SplitDocs(in []byte) [][]byte {
	var docs [][]byte
	docStart := 0

	// Check if document stream begins with a separator at column 0.
	searchPos := 0
	if ok, lineEnd := isSeparator(in, 0); ok {
		if lineEnd < len(in) {
			docStart = lineEnd + 1
			searchPos = lineEnd
		} else {
			docStart = len(in)
			searchPos = len(in)
		}
	}

	sepPattern := []byte("\n---")
	for searchPos < len(in) {
		idx := bytes.Index(in[searchPos:], sepPattern)
		if idx == -1 {
			break
		}
		newlinePos := searchPos + idx
		sepStart := newlinePos + 1
		ok, lineEnd := isSeparator(in, sepStart)
		if !ok {
			searchPos = sepStart
			continue
		}

		rawDoc := in[docStart:sepStart]
		docBytes := bytes.TrimSpace(rawDoc)
		if bytes.IndexByte(docBytes, '\r') != -1 {
			docBytes = bytes.ReplaceAll(docBytes, []byte("\r\n"), []byte("\n"))
			docBytes = bytes.TrimRight(docBytes, "\r")
		}
		if len(docBytes) > 0 {
			cp := make([]byte, len(docBytes))
			copy(cp, docBytes)
			docs = append(docs, cp)
		}

		if lineEnd < len(in) {
			docStart = lineEnd + 1
			searchPos = lineEnd
		} else {
			docStart = len(in)
			searchPos = len(in)
		}
	}

	if docStart < len(in) {
		rawDoc := in[docStart:]
		docBytes := bytes.TrimSpace(rawDoc)
		if bytes.IndexByte(docBytes, '\r') != -1 {
			docBytes = bytes.ReplaceAll(docBytes, []byte("\r\n"), []byte("\n"))
			docBytes = bytes.TrimRight(docBytes, "\r")
		}
		if len(docBytes) > 0 {
			cp := make([]byte, len(docBytes))
			copy(cp, docBytes)
			docs = append(docs, cp)
		}
	}

	return docs
}
