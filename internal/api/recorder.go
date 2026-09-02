package api

import (
	"bytes"
	"net/http"
)

// ResponseRecorder is a minimal http.ResponseWriter that buffers a handler's entire
// response instead of sending it.
type ResponseRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

// NewRecorder creates a new ResponseRecorder initialized with 200 OK.
func NewRecorder() *ResponseRecorder {
	return &ResponseRecorder{header: make(http.Header), status: http.StatusOK}
}

func (r *ResponseRecorder) Header() http.Header { return r.header }

func (r *ResponseRecorder) WriteHeader(status int) { r.status = status }

func (r *ResponseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *ResponseRecorder) Status() int { return r.status }

func (r *ResponseRecorder) Body() []byte { return r.body.Bytes() }
