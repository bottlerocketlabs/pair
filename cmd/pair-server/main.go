package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/microsoftarchive/ttlcache"
	"github.com/ory/graceful"
	"golang.org/x/crypto/acme/autocert"
)

func index(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`
	/         - GET index document
	/s/<path> - PUT create file [up to 10kb content for ~2 min]
	/s/<path> - GET fetch file [within 2 min of creation]
	/metrics  - GET metrics
`))
}

type server struct {
	log    *log.Logger
	domain string
	files  *ttlcache.Cache
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

func (s *server) baseContentHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getHandler(w, r)
	case http.MethodPut:
		s.putHandler(w, r)
	default:
		http.Error(w, fmt.Sprintf("unexpected method used: %s", r.Method), http.StatusMethodNotAllowed)
	}
}

func (s *server) metrics(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("count: %d\n", s.files.Count())))
}

func (s *server) getSelfSignedOrLetsEncryptCert(certManager *autocert.Manager) func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		dirCache, ok := certManager.Cache.(autocert.DirCache)
		if !ok {
			dirCache = "certs"
		}

		keyFile := filepath.Join(string(dirCache), hello.ServerName+"-key.pem")
		crtFile := filepath.Join(string(dirCache), hello.ServerName+".pem")
		certificate, err := tls.LoadX509KeyPair(crtFile, keyFile)
		if err != nil {
			s.log.Printf("%s\nFalling back to Letsencrypt\n", err)
			return certManager.GetCertificate(hello)
		}
		s.log.Println("Loaded selfsigned certificate.")
		return &certificate, err
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
	verbose := flag.Bool("v", false, "Verbose logging")
	domain := flag.String("domain", "localhost.dev", "domain to use on certificate")
	listen := flag.String("l", ":443", "network address and port to listen on (TLS)")
	listenInsecure := flag.String("i", ":80", "network address and port to listen on (insecure)")
	flag.Parse()
	logFlags := 0
	logOut := ioutil.Discard
	if *verbose {
		logFlags = log.LstdFlags | log.Lshortfile
		logOut = os.Stderr
	}
	logger := log.New(logOut, "[server] ", logFlags)

	s := server{
		log:    logger,
		domain: *domain,
		files:  ttlcache.NewCache(120 * time.Second),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", index)
	mux.HandleFunc("/s/", s.baseContentHandler)
	mux.HandleFunc("/metrics", s.metrics)

	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(*domain, "localhost"),
		Cache:      autocert.DirCache("certs"),
	}
	tlsConfig := certManager.TLSConfig()
	tlsConfig.GetCertificate = s.getSelfSignedOrLetsEncryptCert(&certManager)

	srv := graceful.WithDefaults(&http.Server{
		Addr:      *listen,
		Handler:   s.apacheLogHandler(mux),
		TLSConfig: tlsConfig,
	})
	srvInsecure := graceful.WithDefaults(&http.Server{
		Addr:    *listenInsecure,
		Handler: s.apacheLogHandler(certManager.HTTPHandler(nil)),
	})

	logger.Println("main: Starting the server")
	var wg sync.WaitGroup
	go func() {
		wg.Add(1)
		defer wg.Done()
		if err := graceful.Graceful(srvInsecure.ListenAndServe, srvInsecure.Shutdown); err != nil {
			logger.Fatalln("main: Failed to gracefully shutdown insure server")
		}
	}()

	wg.Add(1)
	if err := graceful.Graceful(func() error {
		return srv.ListenAndServeTLS("", "")
	}, srv.Shutdown); err != nil {
		logger.Fatalln("main: Failed to gracefully shutdown server")
	}
	wg.Done()
	logger.Println("main: Server was shutdown gracefully")
	wg.Wait()
}
