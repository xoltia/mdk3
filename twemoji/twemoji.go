package twemoji

import (
	"bytes"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
)

//go:embed assets/*.png
var assetsFS embed.FS

type prefixMap struct {
	final bool
	next  map[rune]*prefixMap
}

var pmap *prefixMap

type EmojiText struct {
	Runes  []rune
	Offset int
	Length int
}

func init() {
	loadPrefixMap()
}

var ErrInvalidEmoji = errors.New("invalid emoji")

func Image(emojiRunes []rune) (img image.Image, err error) {
	emojiCodepointsHex := emojiToHex(emojiRunes)
	if emojiCodepointsHex == "" {
		err = ErrInvalidEmoji
		return
	}

	path := fmt.Sprintf("assets/%s.png", emojiCodepointsHex)
	emojiBytes, err := assetsFS.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		err = ErrInvalidEmoji
		return
	}

	return png.Decode(bytes.NewReader(emojiBytes))
}

func FindEmojis(runes []rune) (emojis []EmojiText) {
	for i := 0; i < len(runes); i++ {
		m := pmap
		var (
			longestMatch    *prefixMap
			longestMatchLen int
		)
		for j := i; j < len(runes); j++ {
			r := runes[j]
			if _, ok := m.next[r]; !ok {
				break
			}
			m = m.next[r]
			if m.final {
				longestMatch = m
				longestMatchLen = j - i + 1
			}
		}
		if longestMatch != nil {
			emojis = append(emojis, EmojiText{
				Runes:  runes[i : i+longestMatchLen],
				Offset: i,
				Length: longestMatchLen,
			})
			i += longestMatchLen - 1
		}
	}
	return
}

func loadPrefixMap() {
	pmap = new(prefixMap)
	pmap.next = make(map[rune]*prefixMap)

	files, _ := assetsFS.ReadDir("assets")
	for _, file := range files {
		name := strings.TrimSuffix(file.Name(), ".png")
		parts := strings.Split(name, "-")
		m := pmap
		for i, part := range parts {
			for len(part) < 8 {
				part = "0" + part
			}
			b, err := hex.DecodeString(part)
			if err != nil {
				continue
			}

			r := bytesToRune(b)
			if m.next == nil {
				m.next = make(map[rune]*prefixMap)
			}
			if _, ok := m.next[r]; !ok {
				m.next[r] = new(prefixMap)
			}

			if i == len(parts)-1 {
				m.next[r].final = true
			} else {
				m = m.next[r]
			}
		}
	}
}

func emojiToHex(runes []rune) string {
	if len(runes) == 0 {
		return ""
	}

	var b strings.Builder
	for i, r := range runes {
		bytes := runeToBytes(r)
		codepointHex := hex.EncodeToString(bytes[:])
		codepointHex = strings.TrimLeft(codepointHex, "0")
		b.WriteString(codepointHex)
		if i < len(runes)-1 {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func runeToBytes(r rune) (b [4]byte) {
	b[0] = byte(r >> 24)
	b[1] = byte(r >> 16)
	b[2] = byte(r >> 8)
	b[3] = byte(r)
	return b
}

func bytesToRune(b []byte) rune {
	if len(b) != 4 {
		panic("invalid byte slice length")
	}

	r := rune(b[0]) << 24
	r |= rune(b[1]) << 16
	r |= rune(b[2]) << 8
	r |= rune(b[3])
	return r
}
