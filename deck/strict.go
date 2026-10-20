package deck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ParseStrict is Parse for a config about to be saved: it also refuses
// fields and app args this version doesn't know, so a config made for newer
// software isn't saved with parts that would silently do nothing. A saved
// config is loaded with Parse, which ignores them, so after a downgrade the
// device still shows it.
func ParseStrict(b []byte) (*Config, error) {
	if len(b) > MaxConfigBytes {
		return nil, fmt.Errorf("larger than %d bytes", MaxConfigBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		var v any
		if json.Unmarshal(b, &v) == nil {
			if path := unknownField(v, reflect.TypeOf(c), ""); path != "" {
				return nil, fmt.Errorf("%s: unknown field; it needs newer device software", path)
			}
		}
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("data after the config")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	for name, p := range c.Pages {
		for k, b := range p.Keys {
			if err := b.Action.CheckArgs(); err != nil {
				return nil, fmt.Errorf("page %q key %d: %w", name, k, err)
			}
		}
	}
	return &c, nil
}

// unknownField is the path, such as pages.main.keys.3.font2, of the first
// field in v (decoded JSON) that type t has no field for, or "".
func unknownField(v any, t reflect.Type, path string) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	join := func(k string) string {
		if path == "" {
			return k
		}
		return path + "." + k
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, _ := v.(map[string]any)
		for _, k := range slices.Sorted(maps.Keys(obj)) {
			f, ok := fieldByJSON(t, k)
			if !ok {
				return join(k)
			}
			if p := unknownField(obj[k], f.Type, join(k)); p != "" {
				return p
			}
		}
	case reflect.Map:
		obj, _ := v.(map[string]any)
		for _, k := range slices.Sorted(maps.Keys(obj)) {
			if p := unknownField(obj[k], t.Elem(), join(k)); p != "" {
				return p
			}
		}
	case reflect.Slice:
		arr, _ := v.([]any)
		for i, e := range arr {
			if p := unknownField(e, t.Elem(), join(strconv.Itoa(i))); p != "" {
				return p
			}
		}
	}
	return ""
}

// fieldByJSON finds struct t's field named k in JSON, matching case as
// encoding/json does.
func fieldByJSON(t reflect.Type, k string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		if name := jsonName(f); name != "" && strings.EqualFold(name, k) {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "-" || !f.IsExported() {
		return ""
	}
	if name == "" {
		return f.Name
	}
	return name
}

// AppArgs are the args each app's ops take.
var AppArgs = map[string][]string{
	AppDiscord: {"id", "guild", "step"},
	AppOBS:     {"id"},
	AppMedia:   {"id", "show", "cycle"},
}

// CheckArgs refuses args an app in AppArgs doesn't take. Other apps' args
// are their host's to check.
func (a Action) CheckArgs() error {
	known, ok := AppArgs[a.App]
	if a.Type != ActionApp || !ok || len(a.Args) == 0 {
		return nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(a.Args, &args); err != nil {
		return errors.New("app args must be an object")
	}
	for _, k := range slices.Sorted(maps.Keys(args)) {
		if !slices.Contains(known, k) {
			return fmt.Errorf("%s %s: unknown arg %q", a.App, a.Op, k)
		}
	}
	return nil
}
