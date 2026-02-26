# gomorphy

Russian morphological analyzer for Go, backed by [pymorphy3](https://github.com/no-plagiarism/pymorphy3) / [OpenCorpora](http://opencorpora.org/) dictionaries embedded at compile time.

## Installation

```
go get github.com/jus1d/gomorphy
```

## Usage

```go
import "github.com/jus1d/gomorphy"

a, err := morph.Default()
if err != nil {
    log.Fatal(err)
}

// All grammatical forms of a word
forms := a.WordForms("кошка")
// [кошка кошки кошке кошку кошкой кошке кошки кошек кошкам кошек кошками кошках]

// OpenCorpora tag for a word
tag := a.Tag("кошка")
// "NOUN,inan,femn sing,nomn"

// All forms of a phrase with adjective–noun agreement
forms = a.PhraseFormsConcordant("красивая кошка")
// [красивая кошка красивой кошки красивой кошке красивую кошку ...]
```

## API

### `Default() (*Analyzer, error)`

Returns the shared `Analyzer` loaded from embedded dictionary data. Thread-safe; the dictionary is loaded once on first call and cached.

### `(*Analyzer) WordForms(word string) []string`

Returns all grammatical forms of a Russian word. The word may be in any grammatical form. Returns `nil` if the word is not found in the dictionary.

### `(*Analyzer) Tag(word string) string`

Returns the [OpenCorpora](http://opencorpora.org/dict.php?act=gram) tag string for the first parse of the word (e.g. `"NOUN,inan,masc sing,nomn"`). Returns an empty string if the word is not found.

### `(*Analyzer) PhraseFormsConcordant(phrase string) []string`

Generates all grammatical forms of a Russian phrase while preserving adjective–noun agreement. Prepositions, conjunctions, and unknown words are left unchanged.

## Dictionary

The embedded dictionary is built from the OpenCorpora v0.92 dataset (revision 417127) compiled by pymorphy2 v0.9.1. It contains:

- 5 140 055 word entries
- 3 456 paradigms
- 5 532 grammatical tags

> Please note that embedding the dictionary into the executable increases its size by ~8.8 MB.

## License

The **Go source code** is licensed under the [MIT License](LICENSE).

The **embedded dictionary data** (`data/`) is derived from [OpenCorpora](http://opencorpora.org/)
and is licensed under [CC BY-SA 4.0](data/LICENSE). If you distribute a binary that embeds
this data, you must comply with the CC BY-SA 4.0 terms (attribution + ShareAlike).
