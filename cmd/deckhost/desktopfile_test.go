package main

import (
	"slices"
	"strings"
	"testing"
)

func TestParseDesktopEntry(t *testing.T) {
	e, err := parseDesktopEntry(strings.NewReader(`# a comment
[Desktop Entry]
Type=Application
Name=Example
Exec=/usr/bin/flatpak run --branch=stable --command=ex org.example.App --new-window %U
TryExec=flatpak
Path=/tmp/x\sy

[Desktop Action new]
Exec=ignored
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/flatpak", "run", "--branch=stable", "--command=ex", "org.example.App", "--new-window"}
	if !slices.Equal(e.Exec, want) || e.TryExec != "flatpak" || e.Path != "/tmp/x y" || e.Hidden {
		t.Errorf("%+v", e)
	}

	for _, bad := range []string{
		"[Desktop Entry]\nType=Link\nURL=https://example.com\n",
		"[Desktop Entry]\nType=Application\n",
		"[Desktop Entry]\nType=Application\nExec=\"unterminated\n",
	} {
		if _, err := parseDesktopEntry(strings.NewReader(bad)); err == nil {
			t.Errorf("parsed %q", bad)
		}
	}
	if e, _ := parseDesktopEntry(strings.NewReader("[Desktop Entry]\nExec=a\nHidden=true\n")); !e.Hidden {
		t.Error("not hidden")
	}
}

func TestSplitExec(t *testing.T) {
	for in, want := range map[string][]string{
		`code --new-window %F`:               {"code", "--new-window"},
		`"/opt/My App/app" --x`:              {"/opt/My App/app", "--x"},
		`sh -c "echo \"hi\" \$HOME \\ done"`: {"sh", "-c", `echo "hi" $HOME \ done`},
		`app --file=%f 100%%`:                {"app", "--file=", "100%"},
		`app ""`:                             {"app", ""},
		`  app   %i  %c   `:                  {"app"},
	} {
		if got, err := splitExec(in); err != nil || !slices.Equal(got, want) {
			t.Errorf("splitExec(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
