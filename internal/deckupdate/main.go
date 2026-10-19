// Package deckupdate is deckd update: it serves the device's update page over
// USB Ethernet and installs update bundles uploaded to it, once confirmed with
// a key press on the device (see internal/ota).
package deckupdate

import (
	_ "embed"
	"flag"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/riptidewave93/d200-deck/internal/debuginfo"
	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/ota"
	"github.com/riptidewave93/d200-deck/internal/serial"
	"github.com/riptidewave93/d200-deck/netid"
)

// Version is deckd's version, set by its main.
var Version = "dev"

//go:embed update.html
var page string

//go:embed debug.html
var debugPage string

//go:embed style.css
var style string

// osRelease is where the OS version is read from.
var osRelease = osrelease.Path

// modelPath names the device, e.g. Ulanzi D200.
var modelPath = "/proc/device-tree/model"

// Main runs deckd update with the given flags.
func Main(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	addr := fs.String("addr", ":80", "listen address")
	dir := fs.String("dir", "/data/update", "where update bundles are received")
	state := fs.String("state", "/data/deckd/update.json", "what the update page keeps across reboots")
	config := fs.String("config", "/data/deckd/config.json", "deckd's config, for the debug info")
	fs.Parse(args)
	debug.SetMemoryLimit(12 << 20)
	log.Printf("deckd %s", Version)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler(mdnsName(), newUpdater(*dir, *state), debuginfo.Device(Version, *config)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	log.Fatal(srv.ListenAndServe())
}

func newUpdater(dir, state string) *updater {
	info, err := osrelease.Read(osRelease)
	if err != nil {
		log.Print(err)
	}
	cmdline, _ := os.ReadFile("/proc/cmdline")
	u := &updater{
		dir: dir, statePath: state, installed: info,
		slot:   ota.RunningSlot(string(cmdline)),
		env:    ota.FWEnv{},
		screen: func() (screen, error) { return dialDeckd(ota.UISocket) },
		reboot: func() error { return exec.Command("reboot").Run() },
		freeSpace: func(dir string) (int64, error) {
			var st unix.Statfs_t
			err := unix.Statfs(dir, &st)
			return int64(st.Bavail) * int64(st.Bsize), err
		},
		healthy: func() bool { return deckdHealthy(ota.ReadyFile, 90*time.Second, 10*time.Second) },
	}
	u.install = func(m *ota.Manifest, progress func(string, int, int)) ([]ota.Result, error) {
		in := &ota.Installer{
			Dir:      dir,
			OpenNAND: func(name string) (ota.NANDCloser, error) { return ota.OpenMTD(name) },
			UBI:      ota.UBI0{},
			Env:      ota.FWEnv{},
			Progress: progress,
		}
		return in.Install(m, u.slot)
	}
	for _, d := range []string{dir, filepath.Dir(state)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Print(err)
		}
	}
	u.load()
	if u.slot == "" {
		log.Print("no d200.slot on the kernel command line: updates are off")
		u.st.State = stateUnsupported
		return u
	}
	u.start()
	return u
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
func handler(name string, u *updater, dbg *debuginfo.Collector) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/update", http.StatusFound)
	})
	mux.HandleFunc("GET /update", showUpdate)
	mux.HandleFunc("GET /update/status", u.showStatus)
	mux.HandleFunc("PUT /update/bundle", u.upload)
	mux.HandleFunc("GET /debug", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		strings.NewReplacer("{style}", style, "{summary}", html.EscapeString(dbg.Summary())).WriteString(w, debugPage)
	})
	mux.HandleFunc("GET /debug/bundle", func(w http.ResponseWriter, r *http.Request) {
		info, _ := osrelease.Read(osRelease)
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="deck-debug-%s.tar.gz"`, orUnknown(info.Build)))
		if err := dbg.Bundle(w); err != nil {
			log.Printf("debug info: %v", err)
		}
	})
	return sameHost(mux, name)
}

func orUnknown(s string) string {
	if s == "" || strings.ContainsAny(s, `"/\`) {
		return "unknown"
	}
	return s
}

func showUpdate(w http.ResponseWriter, r *http.Request) {
	info, err := osrelease.Read(osRelease)
	if err != nil {
		log.Print(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	model, _ := os.ReadFile(modelPath)
	strings.NewReplacer(
		"{style}", style,
		"{model}", show(strings.TrimSpace(strings.TrimRight(string(model), "\x00"))),
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
