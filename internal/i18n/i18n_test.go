package i18n

import "testing"

func TestMatchReadsTheBrowsersPreference(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   Language
	}{
		{"a Dutch browser", "nl-NL,nl;q=0.9,en-US;q=0.8,en;q=0.7", Dutch},
		{"an English browser", "en-GB,en;q=0.9,nl;q=0.8", English},
		{"Flemish is Dutch", "nl-BE", Dutch},
		{"a language this add-on does not speak", "de-DE,de;q=0.9", Default},
		{"no header at all", "", Default},
		{"a wildcard expresses no preference", "*", Default},
		{"quality decides over order", "en;q=0.4,nl;q=0.8", Dutch},
		{"a q of zero refuses that language", "nl;q=0,de;q=0.9", Default},
		{"order decides when quality ties", "nl,en", Dutch},
		{"a malformed q counts as a full preference", "nl;q=banana,en", Dutch},
		{"whitespace and case are irrelevant", "  NL-nl ; q=0.9 ", Dutch},
		{"an unreadable header is never an error", ";;;,,,;q=", Default},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Match(testCase.header); got != testCase.want {
				t.Fatalf("Match(%q) = %q, want %q", testCase.header, got, testCase.want)
			}
		})
	}
}

func TestParseOptionReadsTheConfiguredLanguage(t *testing.T) {
	cases := []struct {
		value string
		want  Language
		ok    bool
	}{
		{"", "", true},
		{"auto", "", true},
		{" AUTO ", "", true},
		{"nl", Dutch, true},
		{"NL", Dutch, true},
		{"en", English, true},
		{"de", "", false},
		{"dutch", "", false},
	}
	for _, testCase := range cases {
		language, ok := ParseOption(testCase.value)
		if ok != testCase.ok || language != testCase.want {
			t.Fatalf("ParseOption(%q) = %q, %t; want %q, %t", testCase.value, language, ok, testCase.want, testCase.ok)
		}
	}
}

func TestOptionsListsAutoAndEveryLanguage(t *testing.T) {
	options := Options()
	if len(options) != len(Supported)+1 || options[0] != Auto {
		t.Fatalf("Options() = %v", options)
	}
	for index, language := range Supported {
		if options[index+1] != string(language) {
			t.Fatalf("Options() = %v, want %q at %d", options, language, index+1)
		}
	}
}
