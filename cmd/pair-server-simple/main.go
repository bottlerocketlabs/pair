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
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
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

	s := handlers.NewServer(logger, 120*time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("/", handlers.Index)
	mux.HandleFunc("/s/", s.BaseContentHandler)
	mux.HandleFunc("/p/", s.BasePipeHandler)
	mux.HandleFunc("/metrics", s.Metrics)

	srvInsecure := graceful.WithDefaults(&http.Server{
		Addr:    *listenInsecure,
		Handler: s.ApacheLogHandler(mux),
	})

	logger.Println("main: Starting the server")
	if err := graceful.Graceful(srvInsecure.ListenAndServe, srvInsecure.Shutdown); err != nil {
		logger.Fatalln("main: Failed to gracefully shutdown server")
	}
	logger.Println("main: Server was shutdown gracefully")
}
