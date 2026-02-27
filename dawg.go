// Package morph provides Russian morphological analysis backed by pymorphy3
// dictionaries (OpenCorpora). The dictionary files are embedded at compile time.
//
// Binary DAWG format is compatible with dawg-python / dawg C-extension.
// See: https://github.com/pytries/dawg-python
package gomorphy

import (
	"encoding/base64"
	"encoding/binary"
	"io"
)

// DAWG unit bit-field constants
// Mirrors dawg_python/units.py exactly.

const (
	precisionMask uint32 = 0xFFFF_FFFF
	isLeafBit     uint32 = 1 << 31
	hasLeafBit    uint32 = 1 << 8
	extensionBit  uint32 = 1 << 9
)

func unitHasLeaf(u uint32) bool { return u&hasLeafBit != 0 }
func unitValue(u uint32) uint32 { return u & ^isLeafBit }
func unitLabel(u uint32) uint32 { return u & (isLeafBit | 0xFF) }
func unitOffset(u uint32) uint32 {
	return ((u >> 10) << ((u & extensionBit) >> 6)) & precisionMask
}

// dictionary is a read-only DAWG dictionary (array of 32-bit units).
type dictionary struct {
	units []uint32
}

func (d *dictionary) hasValue(index uint32) bool {
	return unitHasLeaf(d.units[index])
}

func (d *dictionary) value(index uint32) uint32 {
	off := unitOffset(d.units[index])
	vi := (index ^ off) & precisionMask
	return unitValue(d.units[vi])
}

// followChar follows a single byte transition from index.
// Returns (next_index, true) or (0, false) if no such arc exists.
func (d *dictionary) followChar(label uint32, index uint32) (uint32, bool) {
	off := unitOffset(d.units[index])
	next := (index ^ off ^ label) & precisionMask
	if unitLabel(d.units[next]) != label {
		return 0, false
	}
	return next, true
}

// followBytes follows a sequence of bytes from index.
func (d *dictionary) followBytes(b []byte, index uint32) (uint32, bool) {
	for _, ch := range b {
		var ok bool
		index, ok = d.followChar(uint32(ch), index)
		if !ok {
			return 0, false
		}
	}
	return index, true
}

// read deserialises a dictionary from r (native-endian uint32 array).
func (d *dictionary) read(r io.Reader) error {
	var size uint32
	if err := binary.Read(r, binary.NativeEndian, &size); err != nil {
		return err
	}
	d.units = make([]uint32, size)
	return binary.Read(r, binary.NativeEndian, d.units)
}

// guide stores completion metadata: for each node a (child_label, sibling_label) pair.
type guide struct {
	units []byte // len = 2 * n_nodes; units[i*2]=child, units[i*2+1]=sibling
}

func (g *guide) child(index uint32) byte   { return g.units[index*2] }
func (g *guide) sibling(index uint32) byte { return g.units[index*2+1] }
func (g *guide) size() int                 { return len(g.units) }

func (g *guide) read(r io.Reader) error {
	var size uint32
	if err := binary.Read(r, binary.NativeEndian, &size); err != nil {
		return err
	}
	g.units = make([]byte, size*2)
	_, err := io.ReadFull(r, g.units)
	return err
}

// completer enumerates all completions reachable from a given DAWG node.
// After each successful call to next(), key holds the bytes of the current completion.
type completer struct {
	dict       *dictionary
	guide      *guide
	Key        []byte
	indexStack []uint32
	lastIndex  uint32
}

func newCompleter(d *dictionary, g *guide) *completer {
	return &completer{dict: d, guide: g}
}

func (c *completer) start(index uint32, prefix []byte) {
	c.Key = append(c.Key[:0], prefix...)
	c.lastIndex = 0
	c.indexStack = c.indexStack[:0]
	c.indexStack = append(c.indexStack, index)
}

func (c *completer) next() bool {
	if len(c.indexStack) == 0 {
		return false
	}
	index := c.indexStack[len(c.indexStack)-1]

	if c.lastIndex != 0 { // not initial call
		childLabel := c.guide.child(index)
		if childLabel != 0 {
			index = c.follow(childLabel, index)
			if index == 0 {
				return false
			}
		} else {
			for {
				siblingLabel := c.guide.sibling(index)
				if len(c.Key) > 0 {
					c.Key = c.Key[:len(c.Key)-1]
				}
				c.indexStack = c.indexStack[:len(c.indexStack)-1]
				if len(c.indexStack) == 0 {
					return false
				}
				index = c.indexStack[len(c.indexStack)-1]
				if siblingLabel != 0 {
					index = c.follow(siblingLabel, index)
					if index == 0 {
						return false
					}
					break
				}
			}
		}
	}
	return c.findTerminal(index)
}

func (c *completer) follow(label byte, index uint32) uint32 {
	next, ok := c.dict.followChar(uint32(label), index)
	if !ok {
		return 0
	}
	c.Key = append(c.Key, label)
	c.indexStack = append(c.indexStack, next)
	return next
}

func (c *completer) findTerminal(index uint32) bool {
	for !c.dict.hasValue(index) {
		label := c.guide.child(index)
		next, ok := c.dict.followChar(uint32(label), index)
		if !ok {
			return false
		}
		c.Key = append(c.Key, label)
		c.indexStack = append(c.indexStack, next)
		index = next
	}
	c.lastIndex = index
	return true
}

const dawgPayloadSep = 0x01 // BytesDAWG payload separator byte

// wordEntry is a single (paradigmID, formIdx) pair from the words DAWG.
type wordEntry struct {
	paradigmID uint16
	formIdx    uint16
}

// wordsDawg is a RecordDAWG with format ">HH" mapping word → []wordEntry.
type wordsDawg struct {
	dict  dictionary
	guide guide
}

// load reads a words.dawg file from r.
// File layout: Dictionary data | Guide data (concatenated).
func (w *wordsDawg) load(r io.Reader) error {
	if err := w.dict.read(r); err != nil {
		return err
	}
	return w.guide.read(r)
}

// get returns all (paradigmID, formIdx) entries for the given word.
// Returns nil if the word is not in the dictionary.
func (w *wordsDawg) get(word string) []wordEntry {
	b := []byte(word)

	// Follow word bytes
	idx, ok := w.dict.followBytes(b, 0)
	if !ok {
		return nil
	}

	// Follow payload separator
	idx, ok = w.dict.followChar(dawgPayloadSep, idx)
	if !ok {
		return nil
	}

	// Enumerate all completions; each is a base64-encoded big-endian ">HH" struct.
	c := newCompleter(&w.dict, &w.guide)
	c.start(idx, nil)

	var result []wordEntry
	for c.next() {
		// Strip trailing newline that Python's b2a_base64 appends.
		key := c.Key
		if len(key) > 0 && key[len(key)-1] == '\n' {
			key = key[:len(key)-1]
		}
		decoded, err := base64.StdEncoding.DecodeString(string(key))
		if err != nil || len(decoded) < 4 {
			continue
		}
		result = append(result, wordEntry{
			paradigmID: binary.BigEndian.Uint16(decoded[0:2]),
			formIdx:    binary.BigEndian.Uint16(decoded[2:4]),
		})
	}
	return result
}
