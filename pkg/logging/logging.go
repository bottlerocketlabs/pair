package logging

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// logReqInfo describes info about HTTP request
type logReqInfo struct {
	// GET etc.
	method  string
	uri     string
	referer string
	ipaddr  string
	// response code, like 200, 404
	code int
	// number of bytes of the response sent
	size int64
	// how long did it take to
	duration  time.Duration
	userAgent string
}

// ConnectLogRecord is a simpler log format to record server connections
type ConnectLogRecord struct {
	IP                    string
	Time                  time.Time
	Method, URI, Protocol string
}

func (c *ConnectLogRecord) Log(out io.Writer) {
	apacheFormatPattern := "%s - - [%s] %q Connected\n"
	timeFormatted := c.Time.Format("02/Jan/2006 03:04:05")
	requestLine := fmt.Sprintf("%s %s %s", c.Method, c.URI, c.Protocol)
	fmt.Fprintf(out, apacheFormatPattern, c.IP, timeFormatted, requestLine)
}

// ApacheLogRecord is a complex log format to match the apache request logs
type ApacheLogRecord struct {
	http.ResponseWriter

	IP                    string
	Time                  time.Time
	Method, URI, Protocol string
	Status                int
	ResponseBytes         int64
	ElapsedTime           time.Duration
}

func (r *ApacheLogRecord) Log(out io.Writer) {
	apacheFormatPattern := "%s - - [%s] \"%s %d %d\" %f\n"
	timeFormatted := r.Time.Format("02/Jan/2006 03:04:05")
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URI, r.Protocol)
	fmt.Fprintf(out, apacheFormatPattern, r.IP, timeFormatted, requestLine, r.Status, r.ResponseBytes,
		r.ElapsedTime.Seconds())
}

func (r *ApacheLogRecord) Write(p []byte) (int, error) {
	written, err := r.ResponseWriter.Write(p)
	r.ResponseBytes += int64(written)
	return written, err
}

func (r *ApacheLogRecord) WriteHeader(status int) {
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
}
