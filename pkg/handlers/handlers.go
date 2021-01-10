package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"

	"github.com/stuart-warren/pair/pkg/random"
)

func GenSDPURL(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		panic(fmt.Sprintf("badly formed host provided: %s", host))
	}
	randPath, _ := random.String(32)
	u.Path = path.Join("/p", randPath)
	return u.String()
}

func Index(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`
	/         - GET index document
	/<path>   - PUT stream content to reciever(s) once listening
	/<path>   - GET stream content from sender once sending
	/help     - GET help message
`))
}

func Help(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`
	/         - GET index document
	/<path>   - PUT stream content to reciever(s) once listening
	/<path>   - GET stream content from sender once sending
	/help     - GET help message
`))
}

func (s *server) Metrics(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("no metrics\n")))
}

type server struct {
	log                     *log.Logger
	pathToEstablished       pathToEstablished
	pathToUnestablishedPipe map[string]unestablishedPipe
	reservedPaths           map[string]http.HandlerFunc
}

func NewServer(logger *log.Logger) server {
	return server{
		log: logger,
		pathToEstablished: pathToEstablished{
			paths: make(map[string]interface{}),
		},
		pathToUnestablishedPipe: make(map[string]unestablishedPipe),
		reservedPaths: map[string]http.HandlerFunc{
			"/":     Index,
			"/help": Help,
			"/favicon.ico": func(rw http.ResponseWriter, r *http.Request) {
				rw.WriteHeader(http.StatusNoContent)
				return
			},
			"/robots.txt": func(rw http.ResponseWriter, r *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
				return
			},
		},
	}
}

func (s *server) BaseHandler(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Path
	reservedPathHandler, isReservedPath := s.reservedPaths[reqPath]

	switch r.Method {
	case http.MethodGet:
		if isReservedPath {
			reservedPathHandler(w, r)
			return
		}
		s.handleReciever(w, r)
		return
	case http.MethodPost:
		fallthrough
	case http.MethodPut:
		if isReservedPath {
			http.Error(w, "cannot send data to that path", http.StatusBadRequest)
			return
		}
		s.handleSender(w, r)
		return
	default:
		http.Error(w, fmt.Sprintf("unexpected method used: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}
}
