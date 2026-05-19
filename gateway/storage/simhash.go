package storage

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"unicode"
)

// SimHash64 computes a 64-bit locality-sensitive hash of text.
// Two texts with similar token sets will have a small Hamming distance.
func SimHash64(text string) uint64 {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return 0
	}

	var v [64]int
	for _, tok := range tokens {
		h := tokenHash(tok)
		for i := 0; i < 64; i++ {
			if (h>>uint(i))&1 == 1 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}

	var result uint64
	for i := 0; i < 64; i++ {
		if v[i] > 0 {
			result |= 1 << uint(i)
		}
	}
	return result
}

// HammingDistance returns the number of differing bits between a and b.
func HammingDistance(a, b uint64) int {
	x := a ^ b
	count := 0
	for x != 0 {
		count += int(x & 1)
		x >>= 1
	}
	return count
}

func tokenize(text string) []string {
	lower := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)
	fields := strings.Fields(lower)
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if _, ok := seen[f]; !ok {
			seen[f] = struct{}{}
			out = append(out, f)
		}
	}
	return out
}

func tokenHash(tok string) uint64 {
	h := sha256.Sum256([]byte(tok))
	return binary.LittleEndian.Uint64(h[:8])
}
