package handlers

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/microsoftarchive/ttlcache"
	"github.com/sirupsen/logrus"
	"github.com/stuart-warren/pair/pkg/contextio"
	"github.com/stuart-warren/pair/pkg/env"
	"github.com/stuart-warren/pair/pkg/logging"
	"github.com/stuart-warren/pair/pkg/random"
	"golang.org/x/crypto/acme/autocert"
)

func Index(w http.ResponseWriter, r *http.Request) {
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

func GenSDPURL(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		panic(fmt.Sprintf("badly formed host provided: %s", host))
	}
	randPath, _ := random.String(32)
	u.Path = path.Join("/p", randPath)
	return u.String()
}

type receiver struct {
	receiverChan    chan http.ResponseWriter
	finishedChan    chan bool
	readerConnected bool
	writerConnected bool
	ctx             context.Context
	cancel          context.CancelFunc
}

func newReciever(r *http.Request) *receiver {
	ctx, cancel := context.WithCancel(r.Context())
	return &receiver{
		receiverChan: make(chan http.ResponseWriter),
		finishedChan: make(chan bool),
		ctx:          ctx,
		cancel:       cancel,
	}
}

func NewServer(logger *log.Logger, fileTTL time.Duration) server {
	return server{
		log:           logger,
		files:         ttlcache.NewCache(fileTTL),
		pipeReceivers: make(map[string]*receiver),
	}
}

type server struct {
	log   *log.Logger
	files *ttlcache.Cache

	rwm           sync.RWMutex
	pipeReceivers map[string]*receiver
}

func (s *server) getReciever(path string) (*receiver, bool) {
	s.rwm.RLock()
	defer s.rwm.RUnlock()
	r, ok := s.pipeReceivers[path]
	return r, ok
}

func (s *server) setReciever(path string, r *receiver) {
	s.rwm.Lock()
	defer s.rwm.Unlock()
	s.pipeReceivers[path] = r
}

func (s *server) markReaderConnected(path string) error {
	s.rwm.Lock()
	defer s.rwm.Unlock()
	r, ok := s.pipeReceivers[path]
	if !ok {
		return fmt.Errorf("unexpected path")
	}
	if r.readerConnected {
		return fmt.Errorf("already connected")
	}
	r.readerConnected = true
	return nil
}

func (s *server) markWriterConnected(path string) error {
	s.rwm.Lock()
	defer s.rwm.Unlock()
	r, ok := s.pipeReceivers[path]
	if !ok {
		return fmt.Errorf("unexpected path")
	}
	if r.writerConnected {
		return fmt.Errorf("already connected")
	}
	r.writerConnected = true
	return nil
}

func (s *server) killReciever(path string) {
	s.rwm.Lock()
	defer s.rwm.Unlock()
	r, ok := s.pipeReceivers[path]
	if !ok {
		return
	}
	r.finishedChan <- true
	r.cancel()
	delete(s.pipeReceivers, path)
	s.log.Printf("disconnecting path %s", path)
}

func (s *server) BasePipeHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if _, ok := s.getReciever(path); !ok {
		s.setReciever(path, newReciever(r))
	}
	go func() {
		<-r.Context().Done()
		s.killReciever(path)
	}()

	pr, _ := s.getReciever(path)
	w = contextio.NewResponseWriter(pr.ctx, w)

	switch r.Method {
	case http.MethodGet:
		err := s.markReaderConnected(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("unexpected reader: %s", err), http.StatusConflict)
			return
		}
		go func() { pr.receiverChan <- w }()
		// Wait for finish
		<-pr.finishedChan
	case http.MethodPut:
		err := s.markWriterConnected(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("unexpected writer: %s", err), http.StatusConflict)
			return
		}
		receiver := <-pr.receiverChan
		contentType := env.FirstNonBlank(
			r.Header.Get("Content-Type"),
			mime.TypeByExtension(filepath.Ext(r.URL.Path)),
			"application/octet-stream",
		)
		receiver.Header().Add("Content-Type", contentType)
		_, err = io.Copy(receiver, contextio.NewReader(pr.ctx, r.Body))
		if err != nil {
			s.log.Printf("hit error during io.Copy: %s", err)
		}
	default:
		http.Error(w, fmt.Sprintf("unexpected method used: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}
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

func (s *server) BaseContentHandler(w http.ResponseWriter, r *http.Request) {
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

func (s *server) Metrics(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("file_count: %d\n", s.files.Count())))
	w.Write([]byte(fmt.Sprintf("pipe_count: %d\n", len(s.pipeReceivers))))
}

func justIP(hostPort string) string {
	ip := hostPort
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}
	return ip
}

func firstIP(commaSepList string) string {
	ips := strings.Split(commaSepList, ", ")
	return justIP(ips[0])
}

func (s *server) LogrusLogHandler(h http.Handler) http.Handler {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(s.log.Writer())
	fn := func(w http.ResponseWriter, r *http.Request) {

		clientIP := justIP(r.RemoteAddr)
		forwardedIP := firstIP(r.Header.Get("X-Forwarded-For"))
		record := &logging.LogRecord{
			ResponseWriter: w,
			Status:         http.StatusOK,
		}
		messagePattern := "time=%s method=%s ip=%q fwd=%q proto=%s uri=%q state=%s"

		startTime := time.Now()
		connectLog := logger.WithFields(logrus.Fields{
			"ip":           clientIP,
			"fwd":          forwardedIP,
			"timestamp":    int(startTime.UTC().UnixNano() / int64(time.Millisecond)),
			"method":       r.Method,
			"uri":          r.RequestURI,
			"proto":        r.Proto,
			"state":        "connected",
			"referer":      r.Referer(),
			"user-agent":   r.UserAgent(),
			"content-type": r.Header.Get("Content-Type"),
		})
		connectLog.Info(fmt.Sprintf(messagePattern, startTime.UTC().Format(time.RFC3339), r.Method, clientIP, forwardedIP, r.Proto, r.RequestURI, "connected"))

		h.ServeHTTP(record, r)
		finishTime := time.Now()
		connectLog.WithFields(logrus.Fields{
			"status":       record.Status,
			"timestamp":    int(finishTime.UTC().UnixNano() / int64(time.Millisecond)),
			"size":         record.ResponseBytes,
			"duration":     finishTime.Sub(startTime).Milliseconds(),
			"state":        "disconnected",
			"content-type": record.ContentType,
		}).Info(fmt.Sprintf(messagePattern, finishTime.UTC().Format(time.RFC3339), r.Method, clientIP, forwardedIP, r.Proto, r.RequestURI, "disconnected"))

	}
	return http.HandlerFunc(fn)
}

func (s *server) ApacheLogHandler(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if colon := strings.LastIndex(clientIP, ":"); colon != -1 {
			clientIP = clientIP[:colon]
		}

		record := &logging.ApacheLogRecord{
			LogRecord: &logging.LogRecord{
				ResponseWriter: w,
				Status:         http.StatusOK,
			},
			IP:          clientIP,
			Time:        time.Time{},
			Method:      r.Method,
			URI:         r.RequestURI,
			Protocol:    r.Proto,
			ElapsedTime: time.Duration(0),
		}

		startTime := time.Now()
		connect := &logging.ConnectLogRecord{
			IP:       clientIP,
			Time:     startTime,
			Method:   r.Method,
			URI:      r.RequestURI,
			Protocol: r.Proto,
		}
		connect.Log(s.log.Writer())
		h.ServeHTTP(record, r)
		finishTime := time.Now()

		record.Time = finishTime.UTC()
		record.ElapsedTime = finishTime.Sub(startTime)

		record.Log(s.log.Writer())
	}
	return http.HandlerFunc(fn)
}

func (s *server) GetSelfSignedOrLetsEncryptCert(certManager *autocert.Manager) func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
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
