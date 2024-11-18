package main

import (
	"errors"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"

	_ "embed"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/fogleman/gg"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	xdraw "golang.org/x/image/draw"
)

//go:embed font/NotoSansJP-VariableFont_wght.ttf
var notoSansFontData []byte
var notoSansFont *truetype.Font

func init() {
	var err error
	notoSansFont, err = freetype.ParseFont(notoSansFontData)
	if err != nil {
		panic(err)
	}
}

var (
	previewPath = filepath.Join(os.TempDir(), "mdk3-preview.png")
	loadingPath = filepath.Join(os.TempDir(), "mdk3-loading.png")
)

func downloadThumbnail(url string) (image.Image, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var img image.Image

	switch resp.Header.Get("Content-Type") {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		img, _, err = image.Decode(resp.Body)
	default:
		return nil, errors.New("unsupported image format")
	}

	if err != nil {
		return nil, fmt.Errorf("cannot decode image: %w", err)
	}

	return img, nil
}

func writePreviewPoster(song queuedSong, username string, nextSongs []queuedSong, thumbnail image.Image) (string, error) {
	poster, err := os.Create(previewPath)
	if err != nil {
		return "", err
	}

	defer poster.Close()

	smallThumbnail := image.NewRGBA(image.Rect(0, 0, 1024, 576))
	xdraw.ApproxBiLinear.Scale(smallThumbnail, smallThumbnail.Bounds(), thumbnail, thumbnail.Bounds(), xdraw.Over, nil)

	face := truetype.NewFace(notoSansFont, &truetype.Options{
		Size: 48,
		DPI:  72,
	})

	dc := gg.NewContext(1920, 1080)
	dc.SetRGB(1, 1, 1)
	dc.Clear()
	dc.DrawImage(smallThumbnail, 100, 100)
	dc.SetFontFace(face)
	dc.SetRGB(0, 0, 0)

	bottom := drawWrappedString(dc, song.Title, 100, 800, 1720, 48)
	face = truetype.NewFace(notoSansFont, &truetype.Options{
		Size: 36,
		DPI:  72,
	})

	dc.SetFontFace(face)
	drawTruncatedString(dc, username, 100, bottom+48, 1720)

	drawTruncatedString(dc, "Up next:", 1175, 150, 675)
	for i := 0; i < len(nextSongs) && i < 10; i++ {
		drawTruncatedString(dc, fmt.Sprintf("%d. %s", i+1, nextSongs[i].Title), 1175, 200+float64(i)*48, 675)
	}

	return previewPath, dc.SavePNG(previewPath)
}

func writeLoadingPoster(thumbnail image.Image) (string, error) {
	poster, err := os.Create(loadingPath)
	if err != nil {
		return "", err
	}

	defer poster.Close()

	largeThumbnail := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	xdraw.ApproxBiLinear.Scale(largeThumbnail, largeThumbnail.Bounds(), thumbnail, thumbnail.Bounds(), xdraw.Over, nil)

	dc := gg.NewContext(1920, 1080)
	dc.DrawImage(largeThumbnail, 0, 0)
	dc.SetRGBA(0, 0, 0, 0.5)
	dc.DrawRectangle(0, 0, 1920, 1080)
	dc.Fill()

	dc.SetRGB(1, 1, 1)
	dc.SetFontFace(truetype.NewFace(notoSansFont, &truetype.Options{
		Size: 72,
		DPI:  72,
	}))

	dc.DrawStringAnchored("Loading...", 960, 540, 0.5, 0.5)

	return loadingPath, dc.SavePNG(loadingPath)
}

func drawWrappedString(dc *gg.Context, text string, x, y, maxWidth, fontSize float64) (bottom float64) {
	runes := []rune(text)
	lines := []string{}
	for len(runes) > 0 {
		pos := 0
		for i := 0; i < len(runes); i++ {
			w, _ := dc.MeasureString(string(runes[:i+1]))
			if w > maxWidth {
				break
			}

			pos = i
		}

		lines = append(lines, string(runes[:pos+1]))
		runes = runes[pos+1:]
	}

	for i, line := range lines {
		dc.DrawString(line, x, y+float64(i)*fontSize+float64(i)*8)
	}

	return y + float64(len(lines))*(fontSize+8)
}

func drawTruncatedString(dc *gg.Context, text string, x, y, maxWidth float64) {
	runes := []rune(text)
	originalLength := len(runes)
	w, _ := dc.MeasureString(text)
	for w > maxWidth {
		runes = runes[:len(runes)-1]
		w, _ = dc.MeasureString(string(runes))
	}

	if len(runes) < originalLength {
		elipsisWidth, _ := dc.MeasureString("…")
		for w+elipsisWidth > maxWidth {
			runes = runes[:len(runes)-1]
			w, _ = dc.MeasureString(string(runes))
		}

		runes = append(runes, '…')
	}

	dc.DrawString(string(runes), x, y)
}
