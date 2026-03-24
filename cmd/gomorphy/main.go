// Package main is the gomorphy CLI entry point.
package main

import (
	"fmt"
	"os"
	"strings"

	morph "github.com/jus1d/gomorphy"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gomorphy <phrase>")
		os.Exit(1)
	}

	phrase := strings.Join(os.Args[1:], " ")

	a, err := morph.Default()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for _, form := range a.PhraseFormsConcordant(phrase) {
		fmt.Println(form)
	}
}
