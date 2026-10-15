// Package i18n translates the UI. Messages live in locales/<lang>.toml:
// Ukrainian is the default language and English the other one.
package i18n

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/BurntSushi/toml"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"

	"github.com/sqprrr/ExtendedTimetable/locales"
)

// Default is the language of visitors who have not chosen one.
const Default = "uk"

// Languages lists the supported languages, the default first.
var Languages = []string{"uk", "en"}

// IsSupported reports whether lang is one of Languages.
func IsSupported(lang string) bool { return slices.Contains(Languages, lang) }

// Message is a translatable message: an ID from the locale files and the
// data its template uses. A "Count" entry also picks the plural form.
type Message struct {
	ID   string
	Data map[string]any
}

// M builds a Message from an ID and key/value pairs of template data:
// M("err.too_long", "Count", 200).
func M(id string, kv ...any) Message {
	m := Message{ID: id}
	if len(kv) > 0 {
		m.Data = make(map[string]any, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			m.Data[fmt.Sprint(kv[i])] = kv[i+1]
		}
	}
	return m
}

// Localizer translates messages into one language.
type Localizer struct {
	lang string
	l    *goi18n.Localizer
	// memo holds the translations made so far, by memoKey: a page asks
	// for the same ones on every request, and go-i18n takes long over each.
	memo     sync.Map
	memoSize atomic.Int64
}

// maxMemo bounds the translations a Localizer keeps; most are of fixed
// messages, but the data of the others (names, counts) varies.
const maxMemo = 20000

var localizers = sync.OnceValue(func() map[string]*Localizer {
	bundle := goi18n.NewBundle(language.Ukrainian)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	for _, lang := range Languages {
		// The files are embedded, so a broken one is a programming error
		// that every test run catches.
		if _, err := bundle.LoadMessageFileFS(locales.FS, lang+".toml"); err != nil {
			panic(fmt.Sprintf("i18n: load %s.toml: %v", lang, err))
		}
	}
	out := map[string]*Localizer{}
	for _, lang := range Languages {
		// A message missing in one language falls back to the other.
		out[lang] = &Localizer{lang: lang, l: goi18n.NewLocalizer(bundle, lang, "en", Default)}
	}
	return out
})

// For returns the localizer of lang, or of Default when lang is unsupported.
func For(lang string) *Localizer {
	if l, ok := localizers()[lang]; ok {
		return l
	}
	return localizers()[Default]
}

// English translates m into English, for logs and the JSON API.
func English(m Message) string { return For("en").Msg(m) }

// Lang is the localizer's language code.
func (l *Localizer) Lang() string { return l.lang }

// T translates the message id with key/value pairs of template data.
func (l *Localizer) T(id string, kv ...any) string { return l.Msg(M(id, kv...)) }

// Msg translates m. Data values that are Messages themselves (a field name
// inside an error) are translated first. A message that does not exist comes
// back as its ID.
func (l *Localizer) Msg(m Message) string {
	cfg := &goi18n.LocalizeConfig{MessageID: m.ID}
	var data map[string]any
	if m.Data != nil {
		data = make(map[string]any, len(m.Data))
		for k, v := range m.Data {
			if inner, ok := v.(Message); ok {
				v = l.Msg(inner)
			}
			data[k] = v
		}
		cfg.TemplateData = data
		if c, ok := data["Count"]; ok {
			cfg.PluralCount = c
		}
	}
	key, ok := memoKey(m.ID, data)
	if ok {
		if s, ok := l.memo.Load(key); ok {
			return s.(string)
		}
	}
	s, err := l.l.Localize(cfg)
	if err != nil {
		slog.Error("i18n: translate", "lang", l.lang, "id", m.ID, "err", err)
		return m.ID
	}
	if ok && l.memoSize.Load() < maxMemo {
		if _, loaded := l.memo.LoadOrStore(key, s); !loaded {
			l.memoSize.Add(1)
		}
	}
	return s
}

// memoKey identifies a translation: the message and its data, each value
// with its type, since the plural form of 1 and of 1.5 or "1" may differ.
// ok is false for data of other types than strings and numbers, which are
// not memoized.
func memoKey(id string, data map[string]any) (key string, ok bool) {
	if len(data) == 0 {
		return id, true
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(id)
	for _, k := range keys {
		switch v := data[k].(type) {
		case string, int, int64, float64:
			fmt.Fprintf(&b, "\x00%s\x00%T\x00%v", k, v, v)
		default:
			return "", false
		}
	}
	return b.String(), true
}

type ctxKey struct{}

// WithLang returns a context carrying the request's language.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, lang)
}

// FromContext returns the localizer of the request's language.
func FromContext(ctx context.Context) *Localizer {
	lang, _ := ctx.Value(ctxKey{}).(string)
	return For(lang)
}
