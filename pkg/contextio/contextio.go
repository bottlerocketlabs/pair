package contextio

import (
	"context"
	"io"
	"net/http"
)

type responsewriter struct {
	ctx context.Context
	w   http.ResponseWriter
}

type writer struct {
	ctx context.Context
	w   io.Writer
}

type rwcopier struct {
	responsewriter
}

type copier struct {
	writer
}

func (rw *responsewriter) Header() http.Header {
	return rw.w.Header()
}

func (rw *responsewriter) WriteHeader(statusCode int) {
	rw.w.WriteHeader(statusCode)
}

// NewResponseWriter wraps an http.ResponseWriter to handle context cancellation.
//
// Context state is checked BEFORE every Write.
//
// The returned ResponseWriter also implements io.ReaderFrom to allow io.Copy to select
// the best strategy while still checking the context state before every chunk transfer.
func NewResponseWriter(ctx context.Context, w http.ResponseWriter) http.ResponseWriter {
	if w, ok := w.(*rwcopier); ok && ctx == w.ctx {
		return w
	}
	return &rwcopier{responsewriter{ctx: ctx, w: w}}
}

// NewWriter wraps an io.Writer to handle context cancellation.
//
// Context state is checked BEFORE every Write.
//
// The returned Writer also implements io.ReaderFrom to allow io.Copy to select
// the best strategy while still checking the context state before every chunk transfer.
func NewWriter(ctx context.Context, w io.Writer) io.Writer {
	if w, ok := w.(*copier); ok && ctx == w.ctx {
		return w
	}
	return &copier{writer{ctx: ctx, w: w}}
}

// Write implements io.Writer, but with context awareness.
func (rw *responsewriter) Write(p []byte) (n int, err error) {
	return NewWriter(rw.ctx, rw.w).Write(p)
}

// Write implements io.Writer, but with context awareness.
func (w *writer) Write(p []byte) (n int, err error) {
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
		return w.w.Write(p)
	}
}

type reader struct {
	ctx context.Context
	r   io.Reader
}

// NewReader wraps an io.Reader to handle context cancellation.
//
// Context state is checked BEFORE every Read.
func NewReader(ctx context.Context, r io.Reader) io.Reader {
	if r, ok := r.(*reader); ok && ctx == r.ctx {
		return r
	}
	return &reader{ctx: ctx, r: r}
}

func (r *reader) Read(p []byte) (n int, err error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}

// ReadFrom implements interface io.ReaderFrom, but with context awareness.
//
// This should allow efficient copying allowing writer or reader to define the chunk size.
func (w *copier) ReadFrom(r io.Reader) (n int64, err error) {
	if _, ok := w.w.(io.ReaderFrom); ok {
		// Let the original Writer decide the chunk size.
		return io.Copy(w.writer.w, &reader{ctx: w.ctx, r: r})
	}
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
		// The original Writer is not a ReaderFrom.
		// Let the Reader decide the chunk size.
		return io.Copy(&w.writer, r)
	}
}
