package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ory/graceful"
	"github.com/stuart-warren/pair/pkg/handlers"
	"golang.org/x/crypto/acme/autocert"
)

func main() {
	verbose := flag.Bool("v", false, "Verbose logging")
	domain := flag.String("domain", "localhost.dev", "additional domain to use on certificate")
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

	s := handlers.NewServer(logger, 120*time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/", handlers.Index)
	mux.HandleFunc("/s/", s.BaseContentHandler)
	mux.HandleFunc("/p/", s.BasePipeHandler)
	mux.HandleFunc("/metrics", s.Metrics)

	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(*domain, "localhost"),
		Cache:      autocert.DirCache("certs"),
	}
	tlsConfig := certManager.TLSConfig()
	tlsConfig.GetCertificate = s.GetSelfSignedOrLetsEncryptCert(&certManager)

	srv := graceful.WithDefaults(&http.Server{
		Addr:      *listen,
		Handler:   s.ApacheLogHandler(mux),
		TLSConfig: tlsConfig,
	})

	srvInsecure := graceful.WithDefaults(&http.Server{
		Addr:    *listenInsecure,
		Handler: s.ApacheLogHandler(certManager.HTTPHandler(nil)),
	})

	logger.Println("main: Starting the server")
	go func() {
		if err := graceful.Graceful(srvInsecure.ListenAndServe, srvInsecure.Shutdown); err != nil {
			logger.Fatalln("main: Failed to gracefully shutdown insure server")
		}
	}()
	if err := graceful.Graceful(func() error { return srv.ListenAndServeTLS("", "") }, srv.Shutdown); err != nil {
		logger.Fatalln("main: Failed to gracefully shutdown server")
	}
	logger.Println("main: Server was shutdown gracefully")
}
