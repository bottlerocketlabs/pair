package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/microsoftarchive/ttlcache"
	"github.com/ory/graceful"
)

func index(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`
	/         - GET index document
	/s/<path> - PUT create file [up to 10kb content for ~2 min]
	/s/<path> - GET fetch file [within 2 min of creation]
	/p/<path> - PUT stream content to reciever once listening
	/p/<path> - GET stream content from sender once sending
	/metrics  - GET metrics
`))
}

type receiver struct {
	receiverChan chan http.ResponseWriter
	finishedChan chan bool
	ctx          context.Context
	cancel       context.CancelFunc
}

type server struct {
	log           *log.Logger
	files         *ttlcache.Cache
	pipeReceivers map[string]*receiver
}

func (s *server) putHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("error reading body of request: %s", err), http.StatusBadRequest)
		return
	}
	if len(body) > 10240 {
		http.Error(w, fmt.Sprintf("body of request is over 10240 bytes: %d", len(body)), http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		http.Error(w, fmt.Sprintf("body of request is 0 bytes"), http.StatusBadRequest)
		return
	}
	s.files.Set(r.URL.Path, string(body))
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("CREATED\n"))
}

func (s *server) getHandler(w http.ResponseWriter, r *http.Request) {
	content, exists := s.files.Get(r.URL.Path)
	if !exists {
		http.Error(w, fmt.Sprintf("404 page not found"), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}

func (s *server) basePipeHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if _, ok := s.pipeReceivers[path]; !ok {
		s.pipeReceivers[path] = &receiver{
			receiverChan: make(chan http.ResponseWriter),
			finishedChan: make(chan bool),
		}
	}
	pr := s.pipeReceivers[path]

	// TODO: should block collision (e.g. GET the same path twice)
	// TODO: should close if either sender or receiver closes
	switch r.Method {
	case http.MethodGet:
		go func() { pr.receiverChan <- w }()
		// Wait for finish
		<-pr.finishedChan
	case http.MethodPut:
		receiver := <-pr.receiverChan
		// TODO: Hard code: content-type
		receiver.Header().Add("Content-Type", "application/octet-stream")
		_, err := io.Copy(receiver, r.Body)
		if err != nil {
			s.log.Printf("hit error during io.Copy: %s", err)
		}
		pr.finishedChan <- true
		delete(s.pipeReceivers, path)
	default:
		http.Error(w, fmt.Sprintf("unexpected method used: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}
}

func (s *server) baseContentHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getHandler(w, r)
	case http.MethodPut:
		s.putHandler(w, r)
	default:
		http.Error(w, fmt.Sprintf("unexpected method used: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}
}

func (s *server) metrics(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("file_count: %d\n", s.files.Count())))
	w.Write([]byte(fmt.Sprintf("pipe_count: %d\n", len(s.pipeReceivers))))
}

type readerCtx struct {
	ctx context.Context
	r   io.Reader
}

func (r *readerCtx) Read(p []byte) (n int, err error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}

// NewCancellableReader will cancel a read if the context is cancelled
func NewCancellableReader(ctx context.Context, r io.Reader) io.Reader {
	return &readerCtx{
		ctx: ctx,
		r:   r,
	}
}

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

type apacheLogRecord struct {
	http.ResponseWriter

	ip                    string
	time                  time.Time
	method, uri, protocol string
	status                int
	responseBytes         int64
	elapsedTime           time.Duration
}

func (r *apacheLogRecord) Log(out io.Writer) {
	apacheFormatPattern := "%s - - [%s] \"%s %d %d\" %f\n"
	timeFormatted := r.time.Format("02/Jan/2006 03:04:05")
	requestLine := fmt.Sprintf("%s %s %s", r.method, r.uri, r.protocol)
	fmt.Fprintf(out, apacheFormatPattern, r.ip, timeFormatted, requestLine, r.status, r.responseBytes,
		r.elapsedTime.Seconds())
}

func (r *apacheLogRecord) Write(p []byte) (int, error) {
	written, err := r.ResponseWriter.Write(p)
	r.responseBytes += int64(written)
	return written, err
}

func (r *apacheLogRecord) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (s *server) apacheLogHandler(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if colon := strings.LastIndex(clientIP, ":"); colon != -1 {
			clientIP = clientIP[:colon]
		}

		record := &apacheLogRecord{
			ResponseWriter: w,
			ip:             clientIP,
			time:           time.Time{},
			method:         r.Method,
			uri:            r.RequestURI,
			protocol:       r.Proto,
			status:         http.StatusOK,
			elapsedTime:    time.Duration(0),
		}

		startTime := time.Now()
		h.ServeHTTP(record, r)
		finishTime := time.Now()

		record.time = finishTime.UTC()
		record.elapsedTime = finishTime.Sub(startTime)

		record.Log(s.log.Writer())
	}
	return http.HandlerFunc(fn)
}

func main() {
	port := os.Getenv("PORT")
	verbose := flag.Bool("v", false, "Verbose logging")
	listenInsecure := flag.String("i", ":"+port, "network address and port to listen on (insecure)")
	flag.Parse()
	logFlags := 0
	logOut := ioutil.Discard
	if *verbose {
		logFlags = log.LstdFlags | log.Lshortfile
		logOut = os.Stderr
	}
	logger := log.New(logOut, "[server] ", logFlags)

	s := server{
		log:           logger,
		files:         ttlcache.NewCache(120 * time.Second),
		pipeReceivers: make(map[string]*receiver),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", index)
	mux.HandleFunc("/s/", s.baseContentHandler)
	mux.HandleFunc("/p/", s.basePipeHandler)
	mux.HandleFunc("/metrics", s.metrics)

	srvInsecure := graceful.WithDefaults(&http.Server{
		Addr:    *listenInsecure,
		Handler: s.apacheLogHandler(mux),
	})

	logger.Println("main: Starting the server")
	if err := graceful.Graceful(srvInsecure.ListenAndServe, srvInsecure.Shutdown); err != nil {
		logger.Fatalln("main: Failed to gracefully shutdown insure server")
	}
	logger.Println("main: Server was shutdown gracefully")
}
