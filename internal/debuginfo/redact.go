package debuginfo

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// redactConfig hides what a config can hold that's private: URLs, HTTP
// headers and bodies, typed text, commands and app arguments. Images and
// fonts are replaced by their sizes. The rest, such as labels, colours and
// keybinds, is kept.
func redactConfig(b []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("not valid JSON, left out: %w", err)
	}
	if top, ok := v.(map[string]any); ok {
		for _, k := range []string{"images", "fonts"} {
			if m, ok := top[k].(map[string]any); ok {
				for id, data := range m {
					s, _ := data.(string)
					m[id] = fmt.Sprintf("<%d bytes>", len(s)*3/4)
				}
			}
		}
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(redact(v))
	return out.Bytes(), err
}

const hidden = "<redacted>"

func redact(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			switch k {
			case "url", "body", "text":
				if s, ok := x.(string); ok && s != "" {
					v[k] = hidden
				}
			case "command":
				if l, ok := x.([]any); ok {
					for i := range l {
						l[i] = hidden
					}
				}
			case "headers", "args":
				if m, ok := x.(map[string]any); ok {
					for h := range m {
						m[h] = hidden
					}
				}
			default:
				v[k] = redact(x)
			}
		}
	case []any:
		for i := range v {
			v[i] = redact(v[i])
		}
	}
	return v
}
