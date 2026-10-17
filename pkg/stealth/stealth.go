package stealth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

var (
	URLRE       = regexp.MustCompile(`(?i)^https?://`)
	NickRE      = regexp.MustCompile(`(?i)l?kbelludoo\w*|lohmoreira|l?lrn\b|l?maumau\w*`)
	WebNickRE   = regexp.MustCompile(`(?i)l?kbelludoo\w*|lohmoreira|l?lrn\b|l?maumau\w*`)
	EmailRE     = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	IPRE        = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	SessionRE   = regexp.MustCompile(`\([^)]*@[^)]*\)`)
	VpsSubnetRE = regexp.MustCompile(`\b(?:129\.153|141\.148|193\.122)\.\d{1,3}\.\d{1,3}\b`)
)

type Rule struct {
	RE   *regexp.Regexp
	With string
}

type Scrub struct {
	Rules    []Rule
	KeyRules []Rule
}

func Hub() Scrub {
	return Scrub{
		Rules: []Rule{
			{EmailRE, "usuario@privado.com"},
			{IPRE, "x.x.x.x"},
			{NickRE, "treinador"},
		},
		KeyRules: []Rule{{NickRE, "treinador"}},
	}
}

func HubDeep() Scrub {
	return Scrub{
		Rules: []Rule{
			{SessionRE, "(Sessão Autenticada)"},
			{EmailRE, "protegido"},
			{VpsSubnetRE, "127.0.0.1"},
			{NickRE, "anonimo"},
		},
	}
}

func Individual() Scrub {
	return Scrub{
		Rules: []Rule{
			{SessionRE, "(Sessão Autenticada)"},
			{EmailRE, "protegido"},
			{IPRE, "127.0.0.1"},
			{NickRE, "treinador"},
		},
		KeyRules: []Rule{{NickRE, "treinador"}},
	}
}

func Web() Scrub {
	return Scrub{
		Rules: []Rule{
			{EmailRE, "usuario@privado.com"},
			{IPRE, "127.0.0.1"},
			{WebNickRE, "treinador"},
		},
		KeyRules: []Rule{{WebNickRE, "treinador"}},
	}
}

func Generic() Scrub {
	return Scrub{
		Rules: []Rule{
			{EmailRE, "usuario@privado.com"},
			{IPRE, "x.x.x.x"},
			{NickRE, "treinador"},
		},
	}
}

func (s Scrub) String(str string) string {
	if URLRE.MatchString(str) {
		return str
	}
	out := str
	for _, r := range s.Rules {
		out = r.RE.ReplaceAllLiteralString(out, r.With)
	}
	return out
}

func (s Scrub) Apply(v any) any {
	switch t := v.(type) {
	case string:
		return s.String(t)
	case []any:
		for i := range t {
			t[i] = s.Apply(t[i])
		}
		return t
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		n := 0
		for _, rawKey := range keys {
			key := rawKey
			if !URLRE.MatchString(rawKey) {
				for _, r := range s.KeyRules {
					key = r.RE.ReplaceAllLiteralString(key, r.With)
				}
			}
			for {
				if _, exists := out[key]; !exists {
					break
				}
				n++
				key = fmt.Sprintf("%s_%d", key, n)
			}
			out[key] = s.Apply(t[rawKey])
		}
		return out
	default:
		return v
	}
}

func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func AsMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func AsArray(v any) []any {
	a, _ := v.([]any)
	return a
}

func Truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case json.Number:
		f, err := t.Float64()
		return err == nil && f != 0
	case float64:
		return t != 0
	default:
		return true
	}
}
