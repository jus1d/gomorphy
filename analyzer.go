// Package gomorphy provides Russian morphological analysis backed by pymorphy3
// dictionaries (OpenCorpora). All dictionary data is embedded at compile time,
// so the binary is fully self-contained with no runtime dependencies
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
package gomorphy

import (
	"bytes"
	"embed"
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed data/words.dawg data/paradigms.array data/suffixes.json data/gramtab-opencorpora-int.json data/meta.json
var dictFS embed.FS

// Analyzer performs Russian morphological analysis
// It is safe for concurrent use after initialisation
// Obtain the shared instance via [Default]
type Analyzer struct {
	words     wordsDawg
	paradigms [][]uint16 // paradigms[i] is a flat []uint16 of length N*3:
	//   [0:N]   -- suffix index for each form
	//   [N:2N]  -- gramtab tag ID for each form
	//   [2N:3N] -- paradigmPrefixes index for each form
	suffixes []string
	gramtab  []string // OpenCorpora tag string indexed by tag ID
}

// Default returns the shared Analyzer loaded from embedded dictionary data
// The dictionary is initialised on the first call and cached; subsequent calls
// return the same instance. Safe for concurrent use
func Default() (*Analyzer, error) {
	defaultOnce.Do(func() {
		defaultAnalyzer, defaultErr = newAnalyzer()
	})
	return defaultAnalyzer, defaultErr
}

// WordForms returns all grammatical forms of the given Russian word
// The word may be supplied in any grammatical form
// Returns nil if the word is not found in the dictionary
func (a *Analyzer) WordForms(word string) []string {
	trimmed := strings.TrimSpace(word)
	style := detectCase(trimmed)
	lower := strings.ToLower(trimmed)
	if lower == "" {
		return nil
	}

	entries, dictWord := a.lookupEntries(lower)
	if len(entries) == 0 {
		return nil
	}

	// Use the first (most probable) parse
	e := entries[0]
	para := a.paradigms[e.paradigmID]
	n := len(para) / 3

	if int(e.formIdx) >= n {
		return nil
	}

	stem, ok := a.extractStem(dictWord, para, n, int(e.formIdx))
	if !ok {
		return nil
	}

	seen := make(map[string]struct{}, n)
	forms := make([]string, 0, n)
	for i := 0; i < n; i++ {
		f := applyCase(restoreYo(lower, dictWord, paradigmPrefixes[para[2*n+i]]+stem+a.suffixes[para[i]]), style)
		if _, dup := seen[f]; !dup {
			seen[f] = struct{}{}
			forms = append(forms, f)
		}
	}
	return forms
}

// Tag returns the OpenCorpora tag string for the best parse of the word,
// e.g. "NOUN,inan,masc sing,nomn"
// When multiple parses exist, nominals (NOUN/ADJF) are preferred over verbs.
// Returns an empty string if the word is not found in the dictionary
func (a *Analyzer) Tag(word string) string {
	word = strings.ToLower(strings.TrimSpace(word))
	entries, _ := a.lookupEntries(word)
	if len(entries) == 0 {
		if _, last, ok := splitHyphen(word); ok {
			return a.Tag(last)
		}
		return ""
	}
	return a.bestTag(entries)
}

// posPriority defines disambiguation preference: lower = preferred.
var posPriority = map[string]int{
	"NOUN": 1, "NPRO": 1,
	"ADJF": 2, "ADJS": 2, "PRTF": 2, "PRTS": 2,
	"NUMR": 3, "ADVB": 3,
	"VERB": 4, "INFN": 4, "GRND": 4,
}

// bestTag picks the tag from entries with the highest-priority POS.
func (a *Analyzer) bestTag(entries []wordEntry) string {
	best := ""
	bestPri := 99
	for _, e := range entries {
		para := a.paradigms[e.paradigmID]
		n := len(para) / 3
		tagID := para[n+int(e.formIdx)]
		if int(tagID) >= len(a.gramtab) {
			continue
		}
		t := a.gramtab[tagID]
		pri, ok := posPriority[tagPOS(t)]
		if !ok {
			pri = 10
		}
		if best == "" || pri < bestPri {
			best = t
			bestPri = pri
		}
	}
	return best
}

// PhraseFormsConcordant generates all grammatical forms of a Russian phrase
// while keeping adjective–noun agreement intact
//
// The first noun (or pronoun) is treated as the grammatical head.
// For every case × number combination the head is declined, and adjectives and
// participles standing *before* it are agreed in case, number, gender, and
// animacy. Everything after the head belongs to a dependent group
// ("оборонных исследований" in "институт оборонных исследований") and is left
// in its original form, nouns included.
// Hyphenated words missing from the dictionary are inflected through their last
// segment ("татаро-башкирская" → "татаро-башкирской"), and words spelled with
// "е" instead of "ё" are looked up through their "ё" spelling while keeping the
// original spelling in the result.
// Prepositions, conjunctions, and words not found in the dictionary are
// left unchanged. Quoted segments (using any quote style: "", «», „", ” etc.)
// are always preserved verbatim and never declined.
// Letter case and punctuation of the source phrase are preserved.
// The original phrase is always the first element of the returned slice
func (a *Analyzer) PhraseFormsConcordant(phrase string) []string {
	origPhrase := strings.TrimSpace(phrase)
	tokens := tokenizePhrase(origPhrase)
	if len(tokens) == 0 {
		return nil
	}

	styles := make([]caseStyle, len(tokens))
	for i, t := range tokens {
		if !t.quoted {
			styles[i] = detectCase(t.orig)
		}
	}

	if len(tokens) == 1 && !tokens[0].quoted {
		t := tokens[0]
		forms := a.WordForms(t.text)
		if forms == nil {
			return []string{origPhrase}
		}
		for i := range forms {
			forms[i] = applyCase(forms[i], styles[0]) + t.punct
		}
		// Ensure the input form is first.
		target := applyCase(t.text, styles[0]) + t.punct
		if forms[0] != target {
			for i, f := range forms {
				if f == target {
					forms = append([]string{f}, append(forms[:i:i], forms[i+1:]...)...)
					break
				}
			}
		}
		return forms
	}

	type wordInfo struct {
		pos     string
		tag     string
		animacy string
		gender  string
	}
	infos := make([]wordInfo, len(tokens))
	headIdx := -1

	for i, t := range tokens {
		if t.quoted || serviceWords[t.text] {
			continue
		}
		tag := a.Tag(t.text)
		if tag == "" {
			continue
		}
		pos := tagPOS(tag)
		infos[i] = wordInfo{
			pos:     pos,
			tag:     tag,
			animacy: tagGrammeme(tag, []string{"anim", "inan"}),
			gender:  tagGrammeme(tag, []string{"masc", "femn", "neut"}),
		}
		if (pos == "NOUN" || pos == "NPRO") && headIdx == -1 {
			headIdx = i
		}
	}

	seen := map[string]struct{}{origPhrase: {}}
	result := []string{origPhrase}

	if headIdx == -1 {
		return result
	}

	head := infos[headIdx]
	cases := []string{"nomn", "gent", "datv", "accs", "ablt", "loct"}
	numbers := []string{"sing", "plur"}

	for _, number := range numbers {
		for _, cas := range cases {
			declined := make([]string, len(tokens))
			for i, t := range tokens {
				if t.quoted || serviceWords[t.text] {
					declined[i] = t.orig + t.punct
					continue
				}
				var raw string
				switch infos[i].pos {
				case "NOUN", "NPRO":
					if i == headIdx {
						raw = a.inflect(t.text, cas, number, "", "")
					} else {
						raw = t.text
					}
				case "ADJF", "PRTF":
					// Only modifiers standing before the head agree with it.
					// Adjectives after the head belong to a dependent group
					// ("оборонных исследований") and keep their own case.
					if i < headIdx {
						var extra []string
						if infos[i].pos == "PRTF" {
							extra = participleGrammemes(infos[i].tag)
						}
						raw = a.inflectAdj(t.text, cas, number, head.gender, head.animacy, extra...)
					} else {
						raw = t.text
					}
				default:
					raw = t.text
				}
				if raw == t.text {
					declined[i] = t.orig + t.punct
					continue
				}
				declined[i] = applyCase(raw, styles[i]) + t.punct
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

// caseStyle represents the casing pattern of a word.
type caseStyle int

const (
	caseLower caseStyle = iota // all lowercase
	caseUpper                  // ALL UPPERCASE
	caseTitle                  // Title case (first letter upper, rest lower)
)

// detectCase returns the casing style of a word.
func detectCase(word string) caseStyle {
	if word == "" {
		return caseLower
	}
	firstRune, _ := utf8.DecodeRuneInString(word)
	if !unicode.IsUpper(firstRune) {
		return caseLower
	}
	// First letter is upper — check if all are upper
	allUpper := true
	for _, r := range word {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			allUpper = false
			break
		}
	}
	if allUpper {
		return caseUpper
	}
	return caseTitle
}

// applyCase transforms a lowercase word to match the given casing style.
func applyCase(word string, style caseStyle) string {
	switch style {
	case caseUpper:
		return strings.ToUpper(word)
	case caseTitle:
		runes := []rune(word)
		if len(runes) == 0 {
			return word
		}
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	default:
		return word
	}
}

// phraseToken is a single element of a tokenized phrase.
// text is the lowercased word body used for dictionary lookups, orig keeps the
// original spelling, and punct holds the punctuation trailing the word
// (stripped before lookup, restored when the form is rendered).
// quoted tokens are quoted segments that must not be declined.
type phraseToken struct {
	text   string
	orig   string
	punct  string
	quoted bool
}

// quoteClose maps an opening quote rune to its expected closing rune.
// Straight quotes ('"', '\”) close with the same character.
var quoteClose = map[rune]rune{
	'«':      '»',
	'„':      '"',
	'\u201C': '\u201D', // " → "
	'\u2018': '\u2019', // ' → '
	'"':      '"',
	'\'':     '\'',
}

// tokenizePhrase splits s into tokens, treating quoted spans as single opaque tokens.
// Supported quote styles: "" ” «» „" \u201C\u201D \u2018\u2019
// trailingPunct is the set of punctuation characters stripped from plain token ends.
var trailingPunct = map[rune]bool{
	'.': true, ',': true, ':': true, ';': true, '!': true, '?': true,
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func tokenizePhrase(s string) []phraseToken {
	runes := []rune(s)
	var tokens []phraseToken
	i := 0
	for i < len(runes) {
		// skip whitespace
		for i < len(runes) && isWhitespace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			break
		}
		if closeQuote, ok := quoteClose[runes[i]]; ok {
			// quoted segment: consume until matching close quote
			start := i
			i++
			for i < len(runes) && runes[i] != closeQuote {
				i++
			}
			if i < len(runes) {
				i++ // include closing quote
			}
			seg := string(runes[start:i])
			tokens = append(tokens, phraseToken{text: strings.ToLower(seg), orig: seg, quoted: true})
		} else {
			// plain word: consume until whitespace or opening quote
			start := i
			for i < len(runes) && !isWhitespace(runes[i]) {
				if _, ok := quoteClose[runes[i]]; ok {
					break
				}
				i++
			}
			// strip trailing punctuation, but keep it for rendering
			end := i
			for end > start && trailingPunct[runes[end-1]] {
				end--
			}
			if end > start {
				word := string(runes[start:end])
				tokens = append(tokens, phraseToken{
					text:  strings.ToLower(word),
					orig:  word,
					punct: string(runes[end:i]),
				})
			}
		}
	}
	return tokens
}

var (
	defaultAnalyzer *Analyzer
	defaultOnce     sync.Once
	defaultErr      error
)

// paradigmPrefixes are the three fixed paradigm prefixes used by pymorphy
// Indices match meta.json → compile_options → paradigm_prefixes
var paradigmPrefixes = [3]string{"", "по", "наи"}

// serviceWords lists Russian prepositions and conjunctions that are never declined
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

	// paradigms.array: uint16 LE count, then per paradigm: uint16 LE length + data
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

// inflect declines word to the requested case/number/gender/animacy
// Empty strings for gender and animacy mean "don't care"
// extra lists additional grammemes the target form must carry (used to keep a
// participle in its own voice and tense, so "объединенный" does not turn into
// "объединивший")
// All parses are tried in POS-priority order; returns the original word if no match found
func (a *Analyzer) inflect(word, cas, number, gender, animacy string, extra ...string) string {
	entries, dictWord := a.lookupEntries(word)
	if len(entries) == 0 {
		prefix, last, ok := splitHyphen(word)
		if !ok {
			return word
		}
		if inflected := a.inflect(last, cas, number, gender, animacy, extra...); inflected != last {
			return prefix + inflected
		}
		return word
	}

	// Sort entries by POS priority so nominals are tried first
	sorted := make([]wordEntry, len(entries))
	copy(sorted, entries)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && a.entryPriority(sorted[j]) < a.entryPriority(sorted[j-1]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	for _, e := range sorted {
		para := a.paradigms[e.paradigmID]
		n := len(para) / 3
		stem, ok := a.extractStem(dictWord, para, n, int(e.formIdx))
		if !ok {
			continue
		}
		for i := 0; i < n; i++ {
			if tagMatches(a.gramtab[para[n+i]], cas, number, gender, animacy, extra...) {
				form := paradigmPrefixes[para[2*n+i]] + stem + a.suffixes[para[i]]
				return restoreYo(word, dictWord, form)
			}
		}
	}
	return word
}

func (a *Analyzer) entryPriority(e wordEntry) int {
	para := a.paradigms[e.paradigmID]
	n := len(para) / 3
	tagID := para[n+int(e.formIdx)]
	if int(tagID) >= len(a.gramtab) {
		return 99
	}
	pri, ok := posPriority[tagPOS(a.gramtab[tagID])]
	if !ok {
		return 10
	}
	return pri
}

// inflectAdj inflects an adjective, applying the Russian accusative rule:
// inanimate accusative is identical to nominative; animate is identical to genitive
func (a *Analyzer) inflectAdj(word, cas, number, gender, animacy string, extra ...string) string {
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
			// femn sing accs: keep "accs" -- the -ую ending is unambiguous
		}
	}
	// Plural adjective forms are gender-neutral
	g := gender
	if number == "plur" {
		g = ""
	}
	return a.inflect(word, effectiveCas, number, g, "", extra...)
}

// lookupEntries returns dictionary entries for word. The dictionary stores "ё"
// spellings ("объединённый"), while real texts often use "е", so when the exact
// spelling is missing every single е→ё replacement is tried. The spelling that
// actually matched is returned alongside the entries
func (a *Analyzer) lookupEntries(word string) ([]wordEntry, string) {
	if entries := a.words.get(word); len(entries) > 0 {
		return entries, word
	}
	for _, variant := range yoVariants(word) {
		if entries := a.words.get(variant); len(entries) > 0 {
			return entries, variant
		}
	}
	return nil, word
}

// yoVariants returns the spellings of word with a single "е" replaced by "ё"
func yoVariants(word string) []string {
	if !strings.ContainsRune(word, 'е') {
		return nil
	}
	runes := []rune(word)
	variants := make([]string, 0, 4)
	for i, r := range runes {
		if r != 'е' {
			continue
		}
		v := make([]rune, len(runes))
		copy(v, runes)
		v[i] = 'ё'
		variants = append(variants, string(v))
	}
	return variants
}

// restoreYo drops the "ё" of a generated form when the word was only found in
// the dictionary through an е→ё fallback, so forms keep the source spelling
// ("объединенный" → "объединенным", not "объединённым"). Words found as-is keep
// the dictionary spelling ("день" → "днём")
func restoreYo(word, dictWord, form string) string {
	if word == dictWord {
		return form
	}
	return strings.ReplaceAll(form, "ё", "е")
}

// splitHyphen splits a hyphenated word into its left part and the last segment,
// e.g. "татаро-башкирская" → "татаро-", "башкирская". Such compounds are
// usually absent from the dictionary while their last segment is present and
// carries the inflection
func splitHyphen(word string) (prefix, last string, ok bool) {
	i := strings.LastIndexByte(word, '-')
	if i <= 0 || i == len(word)-1 {
		return "", "", false
	}
	return word[:i+1], word[i+1:], true
}

// extractStem strips the paradigm prefix and suffix of form formIdx from word,
// returning the bare stem. Reports false if word does not match the expected affixes
func (a *Analyzer) extractStem(word string, para []uint16, n, formIdx int) (string, bool) {
	suffix := a.suffixes[para[formIdx]]
	prefix := paradigmPrefixes[para[2*n+formIdx]]
	if !strings.HasPrefix(word, prefix) || !strings.HasSuffix(word, suffix) {
		return "", false
	}
	stem := word[len(prefix) : len(word)-len(suffix)]
	return stem, true
}

// tagPOS returns the part-of-speech token from an OpenCorpora tag string
// Format: "POS[,grammemes] ..." -- the first token before a comma or space
func tagPOS(tag string) string {
	if i := strings.IndexAny(tag, ", "); i >= 0 {
		return tag[:i]
	}
	return tag
}

// tagGrammeme returns the first value from candidates that appears in tag,
// or an empty string if none match
func tagGrammeme(tag string, candidates []string) string {
	for _, g := range candidates {
		if strings.Contains(tag, g) {
			return g
		}
	}
	return ""
}

// tagMatches reports whether tag contains all of the specified grammemes
// An empty string for any parameter means "don't care"
func tagMatches(tag, cas, number, gender, animacy string, extra ...string) bool {
	if !((cas == "" || strings.Contains(tag, cas)) &&
		(number == "" || strings.Contains(tag, number)) &&
		(gender == "" || strings.Contains(tag, gender)) &&
		(animacy == "" || strings.Contains(tag, animacy))) {
		return false
	}
	for _, g := range extra {
		if g != "" && !strings.Contains(tag, g) {
			return false
		}
	}
	return true
}

// participleGrammemes returns the voice/tense/aspect grammemes of a participle
// tag. Keeping them fixed while declining prevents jumping to another
// participle of the same verb
func participleGrammemes(tag string) []string {
	var g []string
	for _, candidates := range [][]string{
		{"actv", "pssv"},
		{"past", "pres"},
		{"perf", "impf"},
	} {
		if v := tagGrammeme(tag, candidates); v != "" {
			g = append(g, v)
		}
	}
	return g
}
