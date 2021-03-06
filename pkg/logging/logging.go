package logging

import "net/http"

type LogRecord struct {
	http.ResponseWriter
	ResponseBytes int64
	Status        int
	ContentType   string
}

func (r *LogRecord) Write(p []byte) (int, error) {
	written, err := r.ResponseWriter.Write(p)
	r.ResponseBytes += int64(written)
	r.ContentType = r.Header().Get("Content-Type")
	return written, err
}

func (r *LogRecord) WriteHeader(status int) {
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
}
