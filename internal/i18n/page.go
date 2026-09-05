package i18n

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// A page is authored once, in English, with every piece of text it shows
// written as a %%token%%. Rendering substitutes one language's catalog into it,
// and hands the same catalog to the page's own script as a JSON dictionary, so
// a string the script composes at runtime — a badge, a failure reason, a
// message — is translated by the same entry as the markup around it. Each
// language is rendered once at start; nothing is substituted per request.
//
// A string the script fills in carries {placeholders}, which the page's own
// helper replaces. Keeping the whole sentence in the catalog is what lets a
// translation put the values where its own grammar wants them.

// Catalog is one language's strings, by key.
type Catalog map[string]string

// DictionaryToken is where a page's script receives its own copy of the
// catalog, as JSON. It is not a catalog key, so no translation can shadow it.
const DictionaryToken = "%%strings%%"

// Text returns one string in one language, falling back to the default
// language and then to the key itself. It is for messages a handler writes
// rather than the page, which cannot use the rendered dictionary.
func Text(catalogs map[Language]Catalog, language Language, key string) string {
	if catalog, ok := catalogs[language]; ok {
		if value, ok := catalog[key]; ok {
			return value
		}
	}
	if catalog, ok := catalogs[Default]; ok {
		if value, ok := catalog[key]; ok {
			return value
		}
	}
	return key
}

// MustRenderPages renders every supported language or panics. Its inputs are
// compile-time constants, so a failure is a malformed binary rather than
// anything a request could cause, and failing at start is how it gets noticed
// instead of reaching someone as a raw token on the page.
func MustRenderPages(what, raw string, catalogs map[Language]Catalog) map[Language]string {
	rendered, err := RenderPages(raw, catalogs)
	if err != nil {
		panic(what + ": " + err.Error())
	}
	return rendered
}

// RenderPages renders raw once per supported language.
func RenderPages(raw string, catalogs map[Language]Catalog) (map[Language]string, error) {
	reference, ok := catalogs[Default]
	if !ok {
		return nil, fmt.Errorf("no catalog for the default language %q", Default)
	}
	rendered := make(map[Language]string, len(catalogs))
	for _, language := range Supported {
		catalog, ok := catalogs[language]
		if !ok {
			return nil, fmt.Errorf("no catalog for %q", language)
		}
		if err := sameKeys(reference, catalog, language); err != nil {
			return nil, err
		}
		page, err := renderPage(raw, catalog)
		if err != nil {
			return nil, fmt.Errorf("render %q: %w", language, err)
		}
		rendered[language] = page
	}
	return rendered, nil
}

// renderPage substitutes one catalog into the authored page.
func renderPage(raw string, catalog Catalog) (string, error) {
	replacements := make([]string, 0, 2*len(catalog)+2)
	for key, value := range catalog {
		if err := substitutable(key, value); err != nil {
			return "", err
		}
		replacements = append(replacements, "%%"+key+"%%", value)
	}
	// Go's JSON encoder escapes <, > and & to their unicode form, so the
	// dictionary cannot close the script element it sits in, whatever a
	// translation says.
	dictionary, err := json.Marshal(catalog)
	if err != nil {
		return "", err
	}
	replacements = append(replacements, DictionaryToken, string(dictionary))

	page := strings.NewReplacer(replacements...).Replace(raw)
	if index := strings.Index(page, "%%"); index >= 0 {
		end := index + 40
		if end > len(page) {
			end = len(page)
		}
		return "", fmt.Errorf("unreplaced token near %q", page[index:end])
	}
	return page, nil
}

// substitutable refuses a translated string that could break out of the markup
// it is substituted into. A page is assembled by substitution rather than by an
// HTML template, so this stands in for escaping — and every string here is a
// label or a sentence, which never legitimately needs any of these characters.
func substitutable(key, value string) error {
	if value == "" {
		return fmt.Errorf("the translation of %q is empty", key)
	}
	if index := strings.IndexAny(value, "<>&\"\\"); index >= 0 {
		return fmt.Errorf("the translation of %q contains %q, which cannot be substituted into a page",
			key, value[index:index+1])
	}
	return nil
}

// sameKeys reports the keys one catalog is missing, or has that no page uses.
func sameKeys(reference, catalog Catalog, language Language) error {
	var missing, unknown []string
	for key := range reference {
		if _, ok := catalog[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range catalog {
		if _, ok := reference[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	if len(missing) > 0 {
		return fmt.Errorf("the %q catalog is missing: %s", language, strings.Join(missing, ", "))
	}
	if len(unknown) > 0 {
		return fmt.Errorf("the %q catalog has strings the page does not use: %s", language, strings.Join(unknown, ", "))
	}
	return nil
}
