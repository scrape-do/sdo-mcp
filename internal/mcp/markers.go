// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The untrusted-content markers carry a random nonce the page cannot know,
// and that nonce is what actually delimits the block. Neutralizing
// look-alikes is a second layer, for a model that does not compare nonces
// carefully: text shaped like a marker never reaches it.
var (
	// markerPhrase is "untrusted [page] content" with anything but letters,
	// digits and line breaks (up to 8 characters) between the words.
	markerPhrase = regexp.MustCompile(`untrusted[^a-z0-9\n]{0,8}(?:page[^a-z0-9\n]{0,8})?content`)
	// markerLine is a whole line shaped like a marker line: three or more
	// dashes, then BEGIN or END.
	markerLine = regexp.MustCompile(`(?m)^[ \t]*-{3,}[ \t]*(?:begin|end)\b[^\n]*`)
)

// neutralize replaces marker look-alikes in text: each marker-shaped line as
// a whole, then each "untrusted content" phrase left elsewhere. Both are
// matched on a folded copy of the text (see fold), so fullwidth letters,
// zero-width or other format characters, no-break or em spaces, Unicode
// dashes and Cyrillic or Greek letters that look Latin do not hide one.
func neutralize(text string) string {
	text = replaceFolded(text, markerLine, "[marker-like line removed]")
	return replaceFolded(text, markerPhrase, "[marker-like text removed]")
}

// folded is text reduced to what a reader sees, with a map back to the
// original: bytes b[i] came from text[from[i]:to[i]].
type folded struct {
	b        []byte
	from, to []int
}

// fold reduces text rune by rune: NFKC compatibility form (fullwidth,
// circled, superscript letters become plain ones), lower case, look-alike
// letters mapped to the Latin letter they imitate, format and combining
// characters dropped, every space read as ' ', every dash as '-' and every
// line or paragraph separator as '\n'.
func fold(text string) folded {
	f := folded{b: make([]byte, 0, len(text))}
	var buf [utf8.UTFMax]byte
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		for _, fr := range foldRune(r) {
			n := utf8.EncodeRune(buf[:], fr)
			for _, c := range buf[:n] {
				f.b = append(f.b, c)
				f.from, f.to = append(f.from, i), append(f.to, i+size)
			}
		}
		i += size
	}
	return f
}

func foldRune(r rune) []rune {
	switch {
	case r == '\r' || r == '\v' || r == '\f' || r == '\u0085' || r == '\u2028' || r == '\u2029':
		return []rune{'\n'}
	case r < utf8.RuneSelf:
		return []rune{unicode.ToLower(r)}
	case unicode.In(r, unicode.Cf, unicode.Mn, unicode.Me):
		return nil // zero-width, joiners, soft hyphen, BOM, combining marks: invisible or decoration
	case unicode.IsSpace(r):
		return []rune{' '}
	case unicode.Is(unicode.Pd, r):
		return []rune{'-'}
	}
	if l, ok := lookalike[r]; ok {
		return []rune{l}
	}
	if l, ok := lookalike[unicode.ToLower(r)]; ok {
		return []rune{l}
	}
	if nfkc := norm.NFKC.String(string(r)); nfkc != string(r) {
		var out []rune
		for _, nr := range nfkc {
			if nr != r { // NFKC is idempotent, so this recursion ends
				out = append(out, foldRune(nr)...)
			}
		}
		return out
	}
	return []rune{unicode.ToLower(r)}
}

// lookalike maps letters from other scripts, and Latin letters NFKC leaves
// alone (dotless i, small capitals), to the Latin letter they are commonly
// used to imitate. Upper and lower case are listed apart because they can
// imitate different letters (Greek Ν is N, ν is v).
var lookalike = func() map[rune]rune {
	pairs := map[rune]string{
		'a': "АаΑαɑᴀᎪ", 'b': "ВЬьΒβʙ", 'c': "СсϹϲᴄᏟ", 'd': "ԀԁᴅᎠ", 'e': "ЕеΕεᴇᎬ",
		'g': "Ԍɡɢ", 'h': "НһΗ", 'i': "ІіӀӏΙιıɩɪ", 'j': "Јјȷ", 'k': "КкΚκᴋ",
		'm': "МмΜᴍ", 'n': "пΝηոɴ", 'o': "ОоΟοՕօᴏ", 'p': "РрΡρᴘᏢ", 'q': "Ԛԛ", 'r': "гʀ",
		's': "ЅѕՏꜱᏚ", 't': "ТтΤτᴛᎢ", 'u': "υμՍսᴜ", 'v': "ν", 'w': "Ԝԝω",
		'x': "ХхΧχ", 'y': "УуҮүΥγ", 'z': "Ζᴢ",
	}
	m := map[rune]rune{}
	for latin, others := range pairs {
		for _, o := range others {
			m[o] = latin
		}
	}
	return m
}()

// replaceFolded replaces every match of re in the folded text with repl, in
// the original text: a match covers the original runes its folded bytes
// came from.
func replaceFolded(text string, re *regexp.Regexp, repl string) string {
	f := fold(text)
	locs := re.FindAllIndex(f.b, -1)
	if locs == nil {
		return text
	}
	var out strings.Builder
	prev := 0
	for _, loc := range locs {
		start, end := max(f.from[loc[0]], prev), f.to[loc[1]-1]
		if end <= start {
			continue
		}
		out.WriteString(text[prev:start])
		out.WriteString(repl)
		prev = end
	}
	out.WriteString(text[prev:])
	return out.String()
}
