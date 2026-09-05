package i18n

import (
	"strings"
	"testing"
)

const samplePage = `<html lang="%%htmlLang%%"><body><h1>%%greeting%%</h1>
<script>var T = %%strings%%;</script></body></html>`

func sampleCatalogs() map[Language]Catalog {
	return map[Language]Catalog{
		English: {"htmlLang": "en", "greeting": "Good evening"},
		Dutch:   {"htmlLang": "nl", "greeting": "Goedenavond"},
	}
}

func TestRenderPagesSubstitutesAndHandsTheScriptItsDictionary(t *testing.T) {
	pages, err := RenderPages(samplePage, sampleCatalogs())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pages[Dutch], "<h1>Goedenavond</h1>") {
		t.Fatalf("Dutch page = %q", pages[Dutch])
	}
	if !strings.Contains(pages[English], `<html lang="en">`) {
		t.Fatalf("English page = %q", pages[English])
	}
	// The script gets the same strings the markup did, so a string it composes
	// at runtime is translated by the same entry.
	if !strings.Contains(pages[Dutch], `var T = {"greeting":"Goedenavond","htmlLang":"nl"};`) {
		t.Fatalf("Dutch dictionary missing from %q", pages[Dutch])
	}
	for language, page := range pages {
		if strings.Contains(page, "%%") {
			t.Fatalf("the %q page still carries a token", language)
		}
	}
}

// A translation that has drifted must fail the build rather than reach someone
// as a raw token on the page.
func TestRenderPagesRefusesADriftedCatalog(t *testing.T) {
	catalogs := sampleCatalogs()
	delete(catalogs[Dutch], "greeting")
	_, err := RenderPages(samplePage, catalogs)
	if err == nil || !strings.Contains(err.Error(), "missing: greeting") {
		t.Fatalf("err = %v, want the missing key named", err)
	}

	catalogs = sampleCatalogs()
	catalogs[Dutch]["leftover"] = "Weg"
	_, err = RenderPages(samplePage, catalogs)
	if err == nil || !strings.Contains(err.Error(), "leftover") {
		t.Fatalf("err = %v, want the unknown key named", err)
	}

	catalogs = sampleCatalogs()
	delete(catalogs, Dutch)
	if _, err := RenderPages(samplePage, catalogs); err == nil {
		t.Fatal("a language with no catalog at all was accepted")
	}
}

// The page is assembled by substitution rather than by an HTML template, so a
// translation carrying markup is the thing that would break out of it.
func TestRenderPagesRefusesAStringItCannotSubstitute(t *testing.T) {
	for _, value := range []string{
		`</script><script>alert(1)</script>`,
		`quote " inside`,
		`back\slash`,
		`ampersand & co`,
		"",
	} {
		catalogs := sampleCatalogs()
		catalogs[Dutch]["greeting"] = value
		if _, err := RenderPages(samplePage, catalogs); err == nil {
			t.Fatalf("a translation of %q was accepted", value)
		}
	}
}

func TestRenderPagesReportsATokenNoCatalogFills(t *testing.T) {
	_, err := RenderPages(samplePage+"%%forgotten%%", sampleCatalogs())
	if err == nil || !strings.Contains(err.Error(), "unreplaced token") {
		t.Fatalf("err = %v, want an unreplaced token", err)
	}
}

func TestTextFallsBackToTheDefaultLanguageAndThenTheKey(t *testing.T) {
	catalogs := map[Language]Catalog{
		English: {"greeting": "Good evening", "onlyEnglish": "Only here"},
		Dutch:   {"greeting": "Goedenavond"},
	}
	if got := Text(catalogs, Dutch, "greeting"); got != "Goedenavond" {
		t.Fatalf("got %q", got)
	}
	if got := Text(catalogs, Dutch, "onlyEnglish"); got != "Only here" {
		t.Fatalf("got %q, want the English fallback", got)
	}
	if got := Text(catalogs, Dutch, "absent"); got != "absent" {
		t.Fatalf("got %q, want the key itself", got)
	}
	if got := Text(catalogs, "de", "greeting"); got != "Good evening" {
		t.Fatalf("got %q, want the default language", got)
	}
}
