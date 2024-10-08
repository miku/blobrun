// blobprocd takes blobs via HTTP POST or PUT and saves them to disk.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/adrg/xdg"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/miku/blobproc"
)

var (
	spoolDir         = flag.String("spool", path.Join(xdg.DataHome, "/blobproc/spool"), "")
	listenAddr       = flag.String("addr", "0.0.0.0:8000", "host port to listen on")
	timeout          = flag.Duration("T", 15*time.Second, "server timeout")
	banner           = `{"id": "blobprocd", "about": "Send your PDF payload to %s/spool - a 200 OK status only confirms receipt, not successful postprocessing, which may take more time. Check Location header for spool id."}`
	showVersion      = flag.Bool("version", false, "show version")
	debug            = flag.Bool("debug", false, "switch to log level DEBUG")
	accessLogFile    = flag.String("access-log", "", "server access logfile, none if empty")
	logFile          = flag.String("log", "", "structured log output file, stderr if empty")
	urlMapFile       = flag.String("urlmap", "", "path to sqlite3 file that will record (url, sha1) pairs; if empty nothing is recorded")
	urlMapHttpHeader = flag.String("urlmap-header", blobproc.DefaultURLMapHttpHeader, "HTTP header to use as URL for the URL map db, if available")
)

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Println(blobproc.Version)
		os.Exit(0)
	}
	var (
		logLevel        = slog.LevelInfo
		h               slog.Handler
		accessLogWriter io.Writer
	)
	if *debug {
		logLevel = slog.LevelDebug
	}
	switch {
	case *logFile != "":
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		h = slog.NewJSONHandler(f, &slog.HandlerOptions{Level: logLevel})
	default:
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
	switch {
	case *accessLogFile != "":
		f, err := os.OpenFile(*accessLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		accessLogWriter = f
	default:
		accessLogWriter = io.Discard
	}
	svc := &blobproc.WebSpoolService{
		Dir:              *spoolDir,
		ListenAddr:       *listenAddr,
		URLMapHttpHeader: *urlMapHttpHeader,
	}
	if *urlMapFile != "" {
		urlMap := blobproc.URLMap{Path: *urlMapFile}
		if err := urlMap.EnsureDB(); err != nil {
			log.Fatal(err)
		}
		svc.URLMap = &urlMap
	}
	r := mux.NewRouter()
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := fmt.Fprintf(w, banner+"\n", *listenAddr)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	r.HandleFunc("/spool", svc.BlobHandler).Methods("POST", "PUT")
	r.HandleFunc("/spool", svc.SpoolListHandler).Methods("GET")
	r.HandleFunc("/spool/{id}", svc.SpoolStatusHandler).Methods("GET")
	loggedRouter := handlers.LoggingHandler(accessLogWriter, r)
	srv := &http.Server{
		Handler:      loggedRouter,
		Addr:         *listenAddr,
		WriteTimeout: *timeout,
		ReadTimeout:  *timeout,
	}
	slog.Info("starting server at", "hostport", srv.Addr, "spool", *spoolDir)
	log.Fatal(srv.ListenAndServe())
}
