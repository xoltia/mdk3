package main

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/http"
	"os"
	"path/filepath"

	_ "embed"
	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/webp"

	"github.com/fogleman/gg"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/xoltia/mdk3/queue"
	"github.com/xoltia/mdk3/twemoji"
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

func writePreviewPoster(
	song queue.QueuedSong,
	username string,
	nextSongs []queue.QueuedSong,
	thumbnail image.Image,
) (string, error) {
	poster, err := os.Create(previewPath)
	if err != nil {
		return "", err
	}

	defer poster.Close()

	smallThumbnail := image.NewRGBA(image.Rect(0, 0, 1024, 576))
	xdraw.ApproxBiLinear.Scale(smallThumbnail, smallThumbnail.Bounds(), thumbnail, thumbnail.Bounds(), xdraw.Over, nil)

	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(img, img.Bounds().Add(image.Point{100, 100}), smallThumbnail, image.Point{}, draw.Over)

	face := truetype.NewFace(notoSansFont, &truetype.Options{
		Size: 48,
		DPI:  72,
	})

	y := twemoji.DrawText(img, twemoji.DrawTextOptions{
		Text:         song.Title,
		MaxWidth:     1720,
		X:            100,
		Y:            800,
		Face:         face,
		OverflowMode: twemoji.OverflowModeWrap,
		MaxLines:     3,
	})

	face = truetype.NewFace(notoSansFont, &truetype.Options{
		Size: 36,
		DPI:  72,
	})

	twemoji.DrawText(img, twemoji.DrawTextOptions{
		Text:         username,
		MaxWidth:     1720,
		X:            100,
		Y:            800 + y + 48,
		Face:         face,
		OverflowMode: twemoji.OverflowModeClip,
	})

	twemoji.DrawText(img, twemoji.DrawTextOptions{
		Text:         "Up next:",
		MaxWidth:     675,
		X:            1175,
		Y:            150,
		Face:         face,
		OverflowMode: twemoji.OverflowModeClip,
	})

	for i := 0; i < len(nextSongs) && i < 10; i++ {
		twemoji.DrawText(img, twemoji.DrawTextOptions{
			Text:         fmt.Sprintf("%d. %s", i+1, nextSongs[i].Title),
			MaxWidth:     675,
			X:            1175,
			Y:            200 + i*48,
			Face:         face,
			OverflowMode: twemoji.OverflowModeClip,
		})
	}

	return previewPath, savePNG(previewPath, img)
}

// TODO: remove gg dependency
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

func savePNG(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}
