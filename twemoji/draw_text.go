package twemoji

import (
	"image"
	"image/color"
	"image/draw"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type OverflowMode int

const (
	// OverflowModeClip clips the text to the MaxWidth.
	OverflowModeClip OverflowMode = iota
	// OverflowModeWrap wraps the text to the MaxWidth.
	OverflowModeWrap
	// OverflowModeTruncate truncates the text to the MaxWidth.
	// OverflowModeTruncate
)

type DrawTextOptions struct {
	// Text is the text to draw.
	Text string
	// X is the x-coordinate of the top-left corner of the text.
	X int
	// Y is the y-coordinate of the top-left corner of the text.
	Y int
	// MaxWidth is the maximum width of the text.
	MaxWidth int
	// MaxLines is the maximum number of lines to draw. Has no effect if OverflowMode
	// is not OverflowModeWrap.
	MaxLines int
	// OverflowMode is the mode to use when the text overflows the MaxWidth.
	OverflowMode OverflowMode
	// Face is the font to use for drawing the non-emoji text.
	Face font.Face
	// Color is the color to use for drawing the text.
	Color color.Color
	// TruncateIndicator is the string to use to indicate that the text was truncated.
	TruncateIndicator string
}

func (opts *DrawTextOptions) applyDefaults() {
	if opts.MaxLines == 0 {
		opts.MaxLines = 1
	}
	if opts.TruncateIndicator == "" {
		opts.TruncateIndicator = "…"
	}
	if opts.Color == nil {
		opts.Color = color.Black
	}
}

type textSegment struct {
	emoji     []rune
	character rune
}

func (s *textSegment) isEmoji() bool {
	return s.emoji != nil
}

// DrawText draws text on an image, putting emojis inline where they are found in the text.
// Assumes UTF-8 encoding.
func DrawText(img draw.Image, opts DrawTextOptions) int {
	opts.applyDefaults()

	drawer := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(opts.Color),
		Face: opts.Face,
		Dot: fixed.Point26_6{
			X: fixed.I(opts.X),
			Y: fixed.I(opts.Y),
		},
	}

	yAdvance := drawer.Face.Metrics().Height.Ceil()

	switch opts.OverflowMode {
	case OverflowModeClip:
		drawTextClipped(drawer, opts)
	case OverflowModeWrap:
		lineCount := drawTextWrapped(drawer, opts)
		yAdvance *= lineCount
	// case OverflowModeTruncate:
	// 	drawTextTruncated(drawer, opts)
	default:
		panic("unknown overflow mode")
	}

	return yAdvance
}

func drawTextClipped(drawer font.Drawer, opts DrawTextOptions) {
	lines := wrapLines([]rune(opts.Text), opts.MaxWidth, drawer.Face, 1)
	if len(lines) == 0 {
		return
	}
	line := lines[0]
	if len(line) == 0 {
		return
	}
	drawLine(drawer, line, opts.X, opts.Y)
}

func drawTextWrapped(drawer font.Drawer, opts DrawTextOptions) (lineCount int) {
	lines := wrapLines([]rune(opts.Text), opts.MaxWidth, drawer.Face, opts.MaxLines)
	y := opts.Y
	for _, line := range lines {
		x := opts.X
		drawLine(drawer, line, x, y)
		y += drawer.Face.Metrics().Height.Ceil()
	}
	lineCount = len(lines)
	return
}

func drawLine(drawer font.Drawer, line []textSegment, x, y int) {
	drawer.Dot.Y = fixed.I(y)
	drawer.Dot.X = fixed.I(x)

	for _, s := range line {
		if s.isEmoji() {
			img := getEmojiImage(s.emoji, drawer.Face.Metrics().Height.Ceil())
			if img != nil {
				placementX := drawer.Dot.X.Ceil()
				placementY := drawer.Dot.Y.Ceil() - drawer.Face.Metrics().Height.Ceil()
				placement := image.Pt(placementX, placementY)
				bounds := img.Bounds().Add(placement)
				draw.Draw(drawer.Dst, bounds, img, image.Point{}, draw.Over)
				drawer.Dot.X += fixed.I(bounds.Dx())
			}
		} else {
			b := [4]byte{}
			l := utf8.EncodeRune(b[:], s.character)
			drawer.DrawBytes(b[:l])
		}
	}
}

func wrapLines(text []rune, maxWidth int, font font.Face, lineLimit int) [][]textSegment {
	emojis := FindEmojis(text)
	segments := make([]textSegment, 0, len(text))
	for i := 0; i < len(text); i++ {
		if len(emojis) > 0 && i == emojis[0].Offset {
			segments = append(segments, textSegment{emoji: emojis[0].Runes})
			i += emojis[0].Length - 1
			emojis = emojis[1:]
		} else {
			segments = append(segments, textSegment{character: text[i]})
		}
	}

	emojiSize := font.Metrics().Height
	lines := make([][]textSegment, 0, lineLimit)
	line := make([]textSegment, 0, len(text))
	lineWidth := 0
	for _, s := range segments {
		var w fixed.Int26_6
		if s.isEmoji() {
			w = emojiSize
		} else {
			// TODO: kerning?
			w, _ = font.GlyphAdvance(s.character)
		}

		if lineWidth+w.Ceil() > maxWidth {
			lines = append(lines, line)
			if len(lines) == lineLimit {
				return lines
			}
			line = make([]textSegment, 0, len(text))
			lineWidth = 0
		}

		line = append(line, s)
		lineWidth += w.Ceil()
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines
}

// TODO: Make this more efficient.
func getEmojiImage(emoji []rune, size int) image.Image {
	img, err := Image(emoji)
	if err != nil {
		return nil
	}
	scaled := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), xdraw.Over, nil)
	return scaled
}
