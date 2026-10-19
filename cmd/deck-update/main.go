// deck-update serves the D200's update page over USB Ethernet and installs
// update bundles uploaded to it, once confirmed with a key press on the
// D200 (see internal/ota).
package main

import (
	_ "embed"
	"flag"
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

	"github.com/riptidewave93/d200-deck/internal/osrelease"
	"github.com/riptidewave93/d200-deck/internal/ota"
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
	dir := flag.String("dir", "/data/update", "where update bundles are received")
	state := flag.String("state", "/data/deck-update/state.json", "what deck-update keeps across reboots")
	flag.Parse()
	debug.SetMemoryLimit(12 << 20)
	log.Printf("deck-update %s", version)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler(mdnsName(), newUpdater(*dir, *state)),
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
func handler(name string, u *updater) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/update", http.StatusFound)
	})
	mux.HandleFunc("GET /update", showUpdate)
	mux.HandleFunc("GET /update/status", u.showStatus)
	mux.HandleFunc("PUT /update/bundle", u.upload)
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
