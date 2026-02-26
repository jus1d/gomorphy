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
