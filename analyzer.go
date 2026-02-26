// Package morph provides Russian morphological analysis backed by pymorphy3
// dictionaries (OpenCorpora). All dictionary data is embedded at compile time,
// so the binary is fully self-contained with no runtime dependencies.
//
// Basic usage:
//
//	a, err := morph.Default()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	forms := a.WordForms("кошка")           // all grammatical forms of a word
//	tag   := a.Tag("кошка")                 // "NOUN,inan,femn sing,nomn"
//	forms  = a.PhraseFormsConcordant("красивая кошка") // phrase with agreement
package morph

import (
	"bytes"
	"embed"
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed data/words.dawg data/paradigms.array data/suffixes.json data/gramtab-opencorpora-int.json data/meta.json
var dictFS embed.FS

// Analyzer performs Russian morphological analysis.
// It is safe for concurrent use after initialisation.
// Obtain the shared instance via [Default].
type Analyzer struct {
	words     wordsDawg
	paradigms [][]uint16 // paradigms[i] is a flat []uint16 of length N*3:
	//   [0:N]   — suffix index for each form
	//   [N:2N]  — gramtab tag ID for each form
	//   [2N:3N] — paradigmPrefixes index for each form
	suffixes []string
	gramtab  []string // OpenCorpora tag string indexed by tag ID
}

// Default returns the shared Analyzer loaded from embedded dictionary data.
// The dictionary is initialised on the first call and cached; subsequent calls
// return the same instance. Safe for concurrent use.
func Default() (*Analyzer, error) {
	defaultOnce.Do(func() {
		defaultAnalyzer, defaultErr = newAnalyzer()
	})
	return defaultAnalyzer, defaultErr
}

// WordForms returns all grammatical forms of the given Russian word.
// The word may be supplied in any grammatical form.
// Returns nil if the word is not found in the dictionary.
func (a *Analyzer) WordForms(word string) []string {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil
	}

	entries := a.words.get(word)
	if len(entries) == 0 {
		return nil
	}

	// Use the first (most probable) parse.
	e := entries[0]
	para := a.paradigms[e.paradigmID]
	n := len(para) / 3

	if int(e.formIdx) >= n {
		return nil
	}

	stem, ok := a.extractStem(word, para, n, int(e.formIdx))
	if !ok {
		return nil
	}

	seen := make(map[string]struct{}, n)
	forms := make([]string, 0, n)
	for i := 0; i < n; i++ {
		f := paradigmPrefixes[para[2*n+i]] + stem + a.suffixes[para[i]]
		if _, dup := seen[f]; !dup {
			seen[f] = struct{}{}
			forms = append(forms, f)
		}
	}
	return forms
}

// Tag returns the OpenCorpora tag string for the first parse of the word,
// e.g. "NOUN,inan,masc sing,nomn".
// Returns an empty string if the word is not found in the dictionary.
func (a *Analyzer) Tag(word string) string {
	word = strings.ToLower(strings.TrimSpace(word))
	entries := a.words.get(word)
	if len(entries) == 0 {
		return ""
	}
	e := entries[0]
	para := a.paradigms[e.paradigmID]
	n := len(para) / 3
	tagID := para[n+int(e.formIdx)]
	if int(tagID) >= len(a.gramtab) {
		return ""
	}
	return a.gramtab[tagID]
}

// PhraseFormsConcordant generates all grammatical forms of a Russian phrase
// while keeping adjective–noun agreement intact.
//
// The rightmost noun (or pronoun) is treated as the grammatical head.
// For every case × number combination the head is declined, and any
// adjectives/participles are agreed in case, number, gender, and animacy.
// Prepositions, conjunctions, and words not found in the dictionary are
// left unchanged. The original phrase is always the first element of the
// returned slice.
func (a *Analyzer) PhraseFormsConcordant(phrase string) []string {
	phrase = strings.ToLower(strings.TrimSpace(phrase))
	words := strings.Fields(phrase)
	if len(words) == 0 {
		return nil
	}

	if len(words) == 1 {
		if forms := a.WordForms(words[0]); forms != nil {
			return forms
		}
		return []string{words[0]}
	}

	type wordInfo struct {
		pos     string
		animacy string
		gender  string
	}
	infos := make([]wordInfo, len(words))
	headIdx := -1

	for i, w := range words {
		if serviceWords[w] {
			continue
		}
		tag := a.Tag(w)
		if tag == "" {
			continue
		}
		pos := tagPOS(tag)
		infos[i] = wordInfo{
			pos:     pos,
			animacy: tagGrammeme(tag, []string{"anim", "inan"}),
			gender:  tagGrammeme(tag, []string{"masc", "femn", "neut"}),
		}
		if pos == "NOUN" || pos == "NPRO" {
			headIdx = i
		}
	}

	seen := map[string]struct{}{phrase: {}}
	result := []string{phrase}

	if headIdx == -1 {
		// No noun found — flatten individual word forms.
		for _, w := range words {
			if serviceWords[w] {
				continue
			}
			for _, f := range a.WordForms(w) {
				if _, ok := seen[f]; !ok {
					seen[f] = struct{}{}
					result = append(result, f)
				}
			}
		}
		return result
	}

	head := infos[headIdx]
	cases := []string{"nomn", "gent", "datv", "accs", "ablt", "loct"}
	numbers := []string{"sing", "plur"}

	for _, number := range numbers {
		for _, cas := range cases {
			declined := make([]string, len(words))
			for i, w := range words {
				if serviceWords[w] {
					declined[i] = w
					continue
				}
				switch infos[i].pos {
				case "NOUN", "NPRO":
					declined[i] = a.inflect(w, cas, number, "", "")
				case "ADJF", "PRTF":
					declined[i] = a.inflectAdj(w, cas, number, head.gender, head.animacy)
				default:
					declined[i] = w
				}
			}
			form := strings.Join(declined, " ")
			if _, ok := seen[form]; !ok {
				seen[form] = struct{}{}
				result = append(result, form)
			}
		}
	}
	return result
}

// ── Initialisation ────────────────────────────────────────────────────────────

var (
	defaultAnalyzer *Analyzer
	defaultOnce     sync.Once
	defaultErr      error
)

// paradigmPrefixes are the three fixed paradigm prefixes used by pymorphy.
// Indices match meta.json → compile_options → paradigm_prefixes.
var paradigmPrefixes = [3]string{"", "по", "наи"}

// serviceWords lists Russian prepositions and conjunctions that are never declined.
var serviceWords = map[string]bool{
	"в": true, "во": true, "на": true, "по": true, "из": true, "за": true,
	"от": true, "до": true, "об": true, "обо": true, "при": true, "про": true,
	"над": true, "под": true, "без": true, "для": true, "через": true,
	"между": true, "среди": true, "около": true, "после": true, "перед": true,
	"вокруг": true, "против": true, "вместо": true, "кроме": true,
	"с": true, "со": true, "к": true, "ко": true, "о": true,
	"и": true, "или": true, "но": true, "а": true, "не": true, "ни": true,
	"как": true, "что": true, "это": true,
}

func newAnalyzer() (*Analyzer, error) {
	a := &Analyzer{}

	raw, err := dictFS.ReadFile("data/words.dawg")
	if err != nil {
		return nil, err
	}
	if err := a.words.load(bytes.NewReader(raw)); err != nil {
		return nil, err
	}

	// paradigms.array: uint16 LE count, then per paradigm: uint16 LE length + data.
	raw, err = dictFS.ReadFile("data/paradigms.array")
	if err != nil {
		return nil, err
	}
	if err := a.loadParadigms(raw); err != nil {
		return nil, err
	}

	raw, err = dictFS.ReadFile("data/suffixes.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &a.suffixes); err != nil {
		return nil, err
	}

	raw, err = dictFS.ReadFile("data/gramtab-opencorpora-int.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &a.gramtab); err != nil {
		return nil, err
	}

	return a, nil
}

func (a *Analyzer) loadParadigms(raw []byte) error {
	r := bytes.NewReader(raw)

	var n uint16
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return err
	}
	a.paradigms = make([][]uint16, n)
	for i := range a.paradigms {
		var length uint16
		if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
			return err
		}
		para := make([]uint16, length)
		if err := binary.Read(r, binary.LittleEndian, para); err != nil {
			return err
		}
		a.paradigms[i] = para
	}
	return nil
}

// ── Inflection helpers ────────────────────────────────────────────────────────

// inflect declines word to the requested case/number/gender/animacy.
// Empty strings for gender and animacy mean "don't care".
// Returns the original word if no matching form is found.
func (a *Analyzer) inflect(word, cas, number, gender, animacy string) string {
	entries := a.words.get(word)
	if len(entries) == 0 {
		return word
	}
	e := entries[0]
	para := a.paradigms[e.paradigmID]
	n := len(para) / 3

	stem, ok := a.extractStem(word, para, n, int(e.formIdx))
	if !ok {
		return word
	}

	for i := 0; i < n; i++ {
		if tagMatches(a.gramtab[para[n+i]], cas, number, gender, animacy) {
			return paradigmPrefixes[para[2*n+i]] + stem + a.suffixes[para[i]]
		}
	}
	return word
}

// inflectAdj inflects an adjective, applying the Russian accusative rule:
// inanimate accusative is identical to nominative; animate is identical to genitive.
func (a *Analyzer) inflectAdj(word, cas, number, gender, animacy string) string {
	effectiveCas := cas
	if cas == "accs" {
		switch {
		case number == "plur":
			if animacy == "inan" {
				effectiveCas = "nomn"
			} else {
				effectiveCas = "gent"
			}
		case number == "sing" && gender == "masc":
			if animacy == "inan" {
				effectiveCas = "nomn"
			} else {
				effectiveCas = "gent"
			}
		case number == "sing" && gender == "neut":
			effectiveCas = "nomn"
			// femn sing accs: keep "accs" — the -ую ending is unambiguous.
		}
	}
	// Plural adjective forms are gender-neutral.
	g := gender
	if number == "plur" {
		g = ""
	}
	return a.inflect(word, effectiveCas, number, g, "")
}

// extractStem strips the paradigm prefix and suffix of form formIdx from word,
// returning the bare stem. Reports false if word does not match the expected affixes.
func (a *Analyzer) extractStem(word string, para []uint16, n, formIdx int) (string, bool) {
	suffix := a.suffixes[para[formIdx]]
	prefix := paradigmPrefixes[para[2*n+formIdx]]
	if !strings.HasPrefix(word, prefix) || !strings.HasSuffix(word, suffix) {
		return "", false
	}
	stem := word[len(prefix) : len(word)-len(suffix)]
	if len(stem) < 0 { // guard: len(prefix)+len(suffix) > len(word)
		return "", false
	}
	return stem, true
}

// ── Tag parsing ───────────────────────────────────────────────────────────────

// tagPOS returns the part-of-speech token from an OpenCorpora tag string.
// Format: "POS[,grammemes] ..." — the first token before a comma or space.
func tagPOS(tag string) string {
	if i := strings.IndexAny(tag, ", "); i >= 0 {
		return tag[:i]
	}
	return tag
}

// tagGrammeme returns the first value from candidates that appears in tag,
// or an empty string if none match.
func tagGrammeme(tag string, candidates []string) string {
	for _, g := range candidates {
		if strings.Contains(tag, g) {
			return g
		}
	}
	return ""
}

// tagMatches reports whether tag contains all of the specified grammemes.
// An empty string for any parameter means "don't care".
func tagMatches(tag, cas, number, gender, animacy string) bool {
	return (cas == "" || strings.Contains(tag, cas)) &&
		(number == "" || strings.Contains(tag, number)) &&
		(gender == "" || strings.Contains(tag, gender)) &&
		(animacy == "" || strings.Contains(tag, animacy))
}
