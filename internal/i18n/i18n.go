// Package i18n picks the language the add-on's own pages are rendered in.
//
// Home Assistant translates an add-on's configuration page itself, from the
// files in the add-on's translations directory, but it hands the add-on's own
// Web UI nothing to go on: the Supervisor forwards the ingress user's identity
// and not the language they set in Home Assistant. What does arrive is the
// browser's Accept-Language, which is the same signal every other website uses
// and is right for almost everyone. The add-on option exists for the rest:
// somebody running an English browser who wants the nursery page in Dutch has
// no other way to say so.
package i18n

import (
	"sort"
	"strconv"
	"strings"
)

// Language is a language this add-on's pages are written in.
type Language string

const (
	// English is the language every page is authored in, and the fallback for
	// anything else a browser asks for.
	English Language = "en"
	// Dutch is the second translation.
	Dutch Language = "nl"

	// Auto is the configured value that follows the browser.
	Auto = "auto"
)

// Supported lists every language a page can be rendered in, English first.
var Supported = []Language{English, Dutch}

// Default is the language used when nothing better is known.
const Default = English

// Options lists the accepted values of the language setting, for a flag's help
// text and for the add-on's schema.
func Options() []string {
	options := make([]string, 0, len(Supported)+1)
	options = append(options, Auto)
	for _, language := range Supported {
		options = append(options, string(language))
	}
	return options
}

// ParseOption reads the configured language. An empty value and "auto" both
// mean "follow the browser", which is reported as an empty language.
func ParseOption(value string) (Language, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == Auto {
		return "", true
	}
	for _, language := range Supported {
		if value == string(language) {
			return language, true
		}
	}
	return "", false
}

// Match negotiates a language from an Accept-Language header. Anything it
// cannot read, and any language this add-on is not written in, yields Default:
// a page in a language the reader did not ask for is still a working page, and
// an unreadable header must never be an error.
func Match(header string) Language {
	type candidate struct {
		language Language
		quality  float64
		position int
	}
	var candidates []candidate
	for position, part := range strings.Split(header, ",") {
		tag, parameters, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		// "nl-BE" is Dutch. Only the primary subtag decides, because that is
		// the granularity these translations have.
		if base, _, found := strings.Cut(tag, "-"); found {
			tag = base
		}
		language, known := supported(tag)
		if !known {
			continue
		}
		candidates = append(candidates, candidate{language, quality(parameters), position})
	}
	// A stable sort on quality alone would keep the header's order for ties,
	// which is exactly what the specification asks for.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].quality > candidates[j].quality
	})
	for _, item := range candidates {
		if item.quality > 0 {
			return item.language
		}
	}
	return Default
}

// supported reports the language for a primary subtag. "*" is deliberately not
// matched: a wildcard says the reader has no preference, and the fallback below
// already covers that.
func supported(tag string) (Language, bool) {
	for _, language := range Supported {
		if tag == string(language) {
			return language, true
		}
	}
	return "", false
}

// quality reads the q parameter of one Accept-Language entry. A missing or
// malformed q is 1, as the specification says, so a header written by hand
// still expresses a preference.
func quality(parameters string) float64 {
	for _, parameter := range strings.Split(parameters, ";") {
		name, value, found := strings.Cut(parameter, "=")
		if !found || strings.ToLower(strings.TrimSpace(name)) != "q" {
			continue
		}
		weight, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || weight < 0 || weight > 1 {
			return 1
		}
		return weight
	}
	return 1
}
