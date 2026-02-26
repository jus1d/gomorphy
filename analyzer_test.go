package morph

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
		{"красивый", "ADJF", ""},
		{"читать", "INFN", ""},
		{"быстро", "ADVB", ""},
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
			// Preposition must remain unchanged
			phrase:   "в большом городе",
			contains: []string{"в большом городе", "в большой город"},
		},
		{
			// Single noun -- delegates to WordForms
			phrase:   "кошка",
			contains: []string{"кошка", "кошки", "кошке", "кошку"},
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
