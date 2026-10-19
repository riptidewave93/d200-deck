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
	"net/netip"
	"runtime/debug"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/serial"
	"github.com/riptidewave93/d200-deck/netid"
)

// version is set at build time.
var version = "dev"

//go:embed update.html
var page string

// osRelease is where the OS version is read from.
var osRelease = osrelease.Path

func main() {
	addr := flag.String("addr", ":80", "listen address")
	flag.Parse()
	debug.SetMemoryLimit(8 << 20)
	log.Printf("deck-update %s", version)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler(mdnsName()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	log.Fatal(srv.ListenAndServe())
}

// mdnsName is the device's d200-<serial>.local name, or "" without a serial.
func mdnsName() string {
	s, err := serial.Read()
	if err == nil {
		var id netid.ID
		if id, err = netid.For(s); err == nil {
			return id.MDNS
		}
	}
	log.Printf("no mDNS name: %v", err)
	return ""
}

// handler serves the pages to requests for the device's IP address or name.
func handler(name string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/update", http.StatusFound)
	})
	mux.HandleFunc("GET /update", showUpdate)
	return sameHost(mux, name)
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

// sameHost only answers requests addressed to the device by its IP address or
// its mDNS name. A web page can't use DNS rebinding to reach the device
// through a hostname it controls.
func sameHost(next http.Handler, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		named := name != "" && strings.EqualFold(strings.TrimSuffix(host, "."), name)
		if !named && (local == nil || !sameAddr(host, local.String())) {
			http.Error(w, "use the device's name or IP address", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameAddr reports whether host (from a Host header, e.g. "[fd00::1]") is the
// IP address of local (host:port).
func sameAddr(host, local string) bool {
	l, _, _ := net.SplitHostPort(local)
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	b, err2 := netip.ParseAddr(l)
	return err == nil && err2 == nil && a.Unmap().WithZone("") == b.Unmap().WithZone("")
}
