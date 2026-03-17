package gomorphy

import (
	"slices"
	"strings"
	"testing"
)

// shared analyzer instance reused across all tests
var testAnalyzer = func() *Analyzer {
	a, err := Default()
	if err != nil {
		panic("failed to load analyzer: " + err.Error())
	}
	return a
}()

func TestDefault(t *testing.T) {
	a, err := Default()
	if err != nil {
		t.Fatalf("Default() error: %v", err)
	}
	if a == nil {
		t.Fatal("Default() returned nil analyzer")
	}

	// Second call must return the exact same instance
	a2, err := Default()
	if err != nil {
		t.Fatalf("Default() second call error: %v", err)
	}
	if a != a2 {
		t.Error("Default() returned different instances on subsequent calls")
	}
}

func TestWordForms(t *testing.T) {
	a := testAnalyzer

	tests := []struct {
		word     string
		contains []string // forms that must appear in the result
	}{
		{
			// Feminine inanimate noun - full declension singular + plural
			word:     "кошка",
			contains: []string{"кошка", "кошки", "кошке", "кошку", "кошкой", "кошек", "кошкам", "кошками", "кошках"},
		},
		{
			// Masculine inanimate noun
			word:     "стол",
			contains: []string{"стол", "стола", "столу", "столом", "столе", "столы", "столов", "столам", "столами", "столах"},
		},
		{
			// Input in genitive form -- must still return full paradigm
			word:     "кошки",
			contains: []string{"кошка", "кошки", "кошке", "кошку"},
		},
		{
			// Verb
			word:     "читать",
			contains: []string{"читать", "читаю", "читаешь", "читает", "читаем", "читаете", "читают"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			forms := a.WordForms(tt.word)
			if forms == nil {
				t.Fatalf("WordForms(%q) = nil", tt.word)
			}
			for _, want := range tt.contains {
				if !slices.Contains(forms, want) {
					t.Errorf("WordForms(%q) does not contain %q; got %v", tt.word, want, forms)
				}
			}
		})
	}
}

func TestWordForms_EdgeCases(t *testing.T) {
	a := testAnalyzer

	t.Run("empty string", func(t *testing.T) {
		if got := a.WordForms(""); got != nil {
			t.Errorf("WordForms(\"\") = %v, want nil", got)
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		if got := a.WordForms("   "); got != nil {
			t.Errorf("WordForms(spaces) = %v, want nil", got)
		}
	})

	t.Run("unknown word", func(t *testing.T) {
		if got := a.WordForms("ыыыыыыы"); got != nil {
			t.Errorf("WordForms(unknown) = %v, want nil", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		lower := a.WordForms("кошка")
		upper := a.WordForms("КОШКА")
		mixed := a.WordForms("Кошка")
		if lower == nil || upper == nil || mixed == nil {
			t.Fatal("WordForms returned nil for some casing variant")
		}
		if strings.Join(lower, ",") != strings.Join(upper, ",") {
			t.Error("WordForms differs for lower vs upper case")
		}
		if strings.Join(lower, ",") != strings.Join(mixed, ",") {
			t.Error("WordForms differs for lower vs mixed case")
		}
	})

	t.Run("no duplicates", func(t *testing.T) {
		forms := a.WordForms("кошка")
		seen := make(map[string]struct{}, len(forms))
		for _, f := range forms {
			if _, dup := seen[f]; dup {
				t.Errorf("WordForms returned duplicate form %q", f)
			}
			seen[f] = struct{}{}
		}
	})
}

func TestTag(t *testing.T) {
	a := testAnalyzer

	tests := []struct {
		word    string
		wantPOS string // just the POS prefix, enough to assert correct parse
		wantTag string // exact tag (empty = skip exact check)
	}{
		{"кошка", "NOUN", "NOUN,inan,femn sing,nomn"},
		{"стол", "NOUN", "NOUN,inan,masc sing,nomn"},
		// "день" is ambiguous: it also matches the verb "деть" (to put).
		// POS-priority disambiguation must resolve it as a noun.
		{"день", "NOUN", "NOUN,inan,masc sing,nomn"},
		{"красивый", "ADJF", ""},
		{"читать", "INFN", ""},
		// "всегда" is unambiguously an adverb; "быстро" is avoided because it
		// also parses as ADJS (short adjective neuter), which the POS-priority
		// logic now prefers.
		{"всегда", "ADVB", ""},
	}

	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			tag := a.Tag(tt.word)
			if tag == "" {
				t.Fatalf("Tag(%q) = \"\", want non-empty", tt.word)
			}
			if got := tagPOS(tag); got != tt.wantPOS {
				t.Errorf("Tag(%q) POS = %q, want %q (full tag: %q)", tt.word, got, tt.wantPOS, tag)
			}
			if tt.wantTag != "" && tag != tt.wantTag {
				t.Errorf("Tag(%q) = %q, want %q", tt.word, tag, tt.wantTag)
			}
		})
	}
}

func TestTag_EdgeCases(t *testing.T) {
	a := testAnalyzer

	t.Run("empty string", func(t *testing.T) {
		if got := a.Tag(""); got != "" {
			t.Errorf("Tag(\"\") = %q, want \"\"", got)
		}
	})

	t.Run("unknown word", func(t *testing.T) {
		if got := a.Tag("ыыыыыыы"); got != "" {
			t.Errorf("Tag(unknown) = %q, want \"\"", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		if a.Tag("кошка") != a.Tag("КОШКА") {
			t.Error("Tag differs for lower vs upper case")
		}
	})
}

func TestPhraseFormsConcordant(t *testing.T) {
	a := testAnalyzer

	tests := []struct {
		phrase   string
		contains []string // phrases that must appear in the result
	}{
		{
			// Adjective + noun agreement
			phrase:   "красивая кошка",
			contains: []string{"красивая кошка", "красивой кошки", "красивой кошке", "красивую кошку", "красивой кошкой"},
		},
		{
			// Preposition stays unchanged; adjective agrees with noun head
			phrase:   "в большом городе",
			contains: []string{"в большом городе", "в большой город", "в большого города", "в большому городу"},
		},
		{
			// Genitive chain: only the first noun (head) is declined;
			// "защитника" and "отечества" are genitive dependents and must stay unchanged.
			// Regression for "день" / mobile-vowel nouns.
			phrase:   "день защитника отечества",
			contains: []string{
				"день защитника отечества",   // nomn sing
				"дня защитника отечества",    // gent sing
				"дню защитника отечества",    // datv sing
				"днём защитника отечества",   // ablt sing
				"дне защитника отечества",    // loct sing
				"дни защитника отечества",    // nomn plur
				"дней защитника отечества",   // gent plur
				"дням защитника отечества",   // datv plur
				"днями защитника отечества",  // ablt plur
				"днях защитника отечества",   // loct plur
			},
		},
		{
			// Single noun in nominative singular
			phrase:   "кошка",
			contains: []string{"кошка", "кошки", "кошке", "кошку"},
		},
		{
			// Single noun supplied in plural — forms[0] must match input, not nominative singular.
			// Regression: WordForms always starts from nomn sing; PhraseFormsConcordant must reorder.
			phrase:   "кошки",
			contains: []string{"кошки", "кошка", "кошке", "кошку"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.phrase, func(t *testing.T) {
			forms := a.PhraseFormsConcordant(tt.phrase)
			if len(forms) == 0 {
				t.Fatalf("PhraseFormsConcordant(%q) returned empty slice", tt.phrase)
			}
			// Original phrase must be first
			if forms[0] != tt.phrase {
				t.Errorf("PhraseFormsConcordant(%q)[0] = %q, want original phrase", tt.phrase, forms[0])
			}
			for _, want := range tt.contains {
				if !slices.Contains(forms, want) {
					t.Errorf("PhraseFormsConcordant(%q) does not contain %q", tt.phrase, want)
				}
			}
		})
	}
}

func TestPhraseFormsConcordant_EdgeCases(t *testing.T) {
	a := testAnalyzer

	t.Run("empty string", func(t *testing.T) {
		if got := a.PhraseFormsConcordant(""); got != nil {
			t.Errorf("PhraseFormsConcordant(\"\") = %v, want nil", got)
		}
	})

	t.Run("unknown word", func(t *testing.T) {
		got := a.PhraseFormsConcordant("ыыыыы")
		if len(got) == 0 {
			t.Fatal("PhraseFormsConcordant(unknown) returned empty")
		}
		// Unknown single word must be returned as-is
		if got[0] != "ыыыыы" {
			t.Errorf("got[0] = %q, want %q", got[0], "ыыыыы")
		}
	})

	t.Run("no duplicates", func(t *testing.T) {
		forms := a.PhraseFormsConcordant("красивая кошка")
		seen := make(map[string]struct{}, len(forms))
		for _, f := range forms {
			if _, dup := seen[f]; dup {
				t.Errorf("PhraseFormsConcordant returned duplicate %q", f)
			}
			seen[f] = struct{}{}
		}
	})
}

func TestPhraseFormsConcordant_QuotedSegments(t *testing.T) {
	a := testAnalyzer

	quoteStyles := []struct {
		name   string
		open   string
		close  string
	}{
		{"double", `"`, `"`},
		{"guillemets", "«", "»"},
		{"german", "„", "\u201D"},
		{"curly-double", "\u201C", "\u201D"},
	}

	for _, qs := range quoteStyles {
		t.Run(qs.name, func(t *testing.T) {
			phrase := "компания " + qs.open + "золотой ключ" + qs.close
			forms := a.PhraseFormsConcordant(phrase)
			if len(forms) == 0 {
				t.Fatalf("PhraseFormsConcordant(%q) returned empty", phrase)
			}
			quoted := qs.open + "золотой ключ" + qs.close
			for _, f := range forms {
				if !strings.Contains(f, quoted) {
					t.Errorf("form %q does not preserve quoted segment %q", f, quoted)
				}
			}
		})
	}

	t.Run("first form is original", func(t *testing.T) {
		phrase := `компания "золотой ключ"`
		forms := a.PhraseFormsConcordant(phrase)
		if len(forms) == 0 || forms[0] != phrase {
			t.Errorf("first form = %q, want %q", forms[0], phrase)
		}
	})

	t.Run("head noun is declined", func(t *testing.T) {
		// "компания" must appear in multiple cases; quoted part stays intact
		phrase := `компания "золотой ключ"`
		forms := a.PhraseFormsConcordant(phrase)
		var hasGenitive bool
		for _, f := range forms {
			if strings.HasPrefix(f, "компании") {
				hasGenitive = true
			}
		}
		if !hasGenitive {
			t.Errorf("expected genitive form of компания among: %v", forms)
		}
	})
}

func TestTagPOS(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"NOUN,inan,masc sing,nomn", "NOUN"},
		{"ADJF,Qual masc,sing,nomn", "ADJF"},
		{"VERB,impf,tran sing,1per,pres,indc", "VERB"},
		{"ADVB", "ADVB"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := tagPOS(tt.tag); got != tt.want {
			t.Errorf("tagPOS(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

func TestTagGrammeme(t *testing.T) {
	tag := "NOUN,inan,femn sing,nomn"
	tests := []struct {
		candidates []string
		want       string
	}{
		{[]string{"anim", "inan"}, "inan"},
		{[]string{"masc", "femn", "neut"}, "femn"},
		{[]string{"sing", "plur"}, "sing"},
		{[]string{"datv", "nomn"}, "nomn"},
		{[]string{"VERB", "ADJF"}, ""},
	}
	for _, tt := range tests {
		if got := tagGrammeme(tag, tt.candidates); got != tt.want {
			t.Errorf("tagGrammeme(%q, %v) = %q, want %q", tag, tt.candidates, got, tt.want)
		}
	}
}

func TestTagMatches(t *testing.T) {
	tag := "NOUN,inan,femn sing,nomn"
	tests := []struct {
		cas, number, gender, animacy string
		want                         bool
	}{
		{"nomn", "sing", "femn", "inan", true},
		{"nomn", "sing", "", "", true},
		{"gent", "sing", "femn", "inan", false},
		{"nomn", "plur", "femn", "inan", false},
		{"nomn", "sing", "masc", "inan", false},
		{"nomn", "sing", "femn", "anim", false},
		{"", "", "", "", true},
	}
	for _, tt := range tests {
		got := tagMatches(tag, tt.cas, tt.number, tt.gender, tt.animacy)
		if got != tt.want {
			t.Errorf("tagMatches(%q, %q, %q, %q, %q) = %v, want %v",
				tag, tt.cas, tt.number, tt.gender, tt.animacy, got, tt.want)
		}
	}
}
