// deck-update serves the D200's update page over USB Ethernet. Installing
// updates isn't supported yet; the page only shows the installed version.
package main

import (
	_ "embed"
	"flag"
	"html"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/internal/osrelease"
)

//go:embed update.html
var page string

// osRelease is where the OS version is read from.
var osRelease = osrelease.Path

func main() {
	addr := flag.String("addr", ":80", "listen address")
	flag.Parse()
	debug.SetMemoryLimit(8 << 20)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	log.Fatal(srv.ListenAndServe())
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/update", http.StatusFound)
	})
	mux.HandleFunc("GET /update", showUpdate)
	return sameHost(mux)
}

func showUpdate(w http.ResponseWriter, r *http.Request) {
	info, err := osrelease.Read(osRelease)
	if err != nil {
		log.Print(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	strings.NewReplacer(
		"{version}", show(info.Version),
		"{flavour}", show(info.Flavour),
		"{build}", show(info.Build),
	).WriteString(w, page)
}

func show(s string) string {
	if s == "" {
		return "unknown"
	}
	return html.EscapeString(s)
}

// sameHost only answers requests addressed to the device by its IP address.
// A web page can't use DNS rebinding to reach the device through a hostname
// it controls.
func sameHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if local == nil || host != hostOf(local.String()) {
			http.Error(w, "use the device's IP address", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostOf(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	return h
}
