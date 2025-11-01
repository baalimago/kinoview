package thumbnail

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"
)

func writeImg(t *testing.T, dir, name, enc string, w, h int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := SaveImage(img, enc, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	return p
}

func TestLoadImage(t *testing.T) {
	t.Run("png", func(t *testing.T) {
		tmp := t.TempDir()
		p := writeImg(t, tmp, "a.png", "png", 40, 20)
		got, err := LoadImage(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Path != p {
			t.Errorf("path")
		}
		if got.Width != 40 || got.Height != 20 {
			t.Errorf("size")
		}
		if got.Encoding != "png" {
			t.Errorf("enc")
		}
		if got.Raw == nil {
			t.Errorf("raw")
		}
	})

	t.Run("jpeg", func(t *testing.T) {
		tmp := t.TempDir()
		p := writeImg(t, tmp, "a.jpg", "jpeg", 33, 17)
		got, err := LoadImage(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Path != p {
			t.Errorf("path")
		}
		if got.Width != 33 || got.Height != 17 {
			t.Errorf("size")
		}
		if got.Encoding != "jpeg" {
			t.Errorf("enc")
		}
		if got.Raw == nil {
			t.Errorf("raw")
		}
	})

	t.Run("gif", func(t *testing.T) {
		tmp := t.TempDir()
		p := writeImg(t, tmp, "a.gif", "gif", 28, 28)
		got, err := LoadImage(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Path != p {
			t.Errorf("path")
		}
		if got.Width != 28 || got.Height != 28 {
			t.Errorf("size")
		}
		if got.Encoding != "gif" {
			t.Errorf("enc")
		}
		if got.Raw == nil {
			t.Errorf("raw")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		tmp := t.TempDir()
		p := filepath.Join(tmp, "no.png")
		_, err := LoadImage(p)
		if err == nil {
			t.Fatalf("want err")
		}
	})
}

func TestSaveImage(t *testing.T) {
	t.Run("png", func(t *testing.T) {
		tmp := t.TempDir()
		p := filepath.Join(tmp, "a.png")
		img := image.NewRGBA(image.Rect(0, 0, 10, 20))
		if err := SaveImage(img, "png", p); err != nil {
			t.Fatalf("err: %v", err)
		}
		got, err := LoadImage(p)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Encoding != "png" {
			t.Errorf("enc")
		}
		if got.Width != 10 || got.Height != 20 {
			t.Errorf("size")
		}
	})

	t.Run("jpeg", func(t *testing.T) {
		tmp := t.TempDir()
		p := filepath.Join(tmp, "a.jpeg")
		img := image.NewRGBA(image.Rect(0, 0, 11, 21))
		if err := SaveImage(img, "jpeg", p); err != nil {
			t.Fatalf("err: %v", err)
		}
		got, err := LoadImage(p)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Encoding != "jpeg" {
			t.Errorf("enc")
		}
		if got.Width != 11 || got.Height != 21 {
			t.Errorf("size")
		}
	})

	t.Run("jpg alias", func(t *testing.T) {
		tmp := t.TempDir()
		p := filepath.Join(tmp, "a.jpg")
		img := image.NewRGBA(image.Rect(0, 0, 12, 22))
		if err := SaveImage(img, "jpg", p); err != nil {
			t.Fatalf("err: %v", err)
		}
		got, err := LoadImage(p)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Encoding != "jpeg" {
			t.Errorf("enc")
		}
		if got.Width != 12 || got.Height != 22 {
			t.Errorf("size")
		}
	})

	t.Run("gif", func(t *testing.T) {
		tmp := t.TempDir()
		p := filepath.Join(tmp, "a.gif")
		img := image.NewRGBA(image.Rect(0, 0, 13, 23))
		if err := SaveImage(img, "gif", p); err != nil {
			t.Fatalf("err: %v", err)
		}
		got, err := LoadImage(p)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got.Encoding != "gif" {
			t.Errorf("enc")
		}
		if got.Width != 13 || got.Height != 23 {
			t.Errorf("size")
		}
	})

	t.Run("bad encoding", func(t *testing.T) {
		tmp := t.TempDir()
		p := filepath.Join(tmp, "a.bmp")
		img := image.NewRGBA(image.Rect(0, 0, 5, 5))
		if err := SaveImage(img, "bmp", p); err == nil {
			t.Fatalf("want err")
		}
	})

	t.Run("bad path", func(t *testing.T) {
		tmp := t.TempDir()
		p := filepath.Join(tmp, "no", "a.png")
		img := image.NewRGBA(image.Rect(0, 0, 5, 5))
		if err := SaveImage(img, "png", p); err == nil {
			t.Fatalf("want err")
		}
	})
}

func rgba(c color.Color) color.RGBA {
	return color.RGBAModel.Convert(c).(color.RGBA)
}

func filled(w, h int, col color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, col)
		}
	}
	return img
}

func TestCenterResize(t *testing.T) {
	red := color.RGBA{R: 200, G: 10, B: 10, A: 255}
	black := color.RGBA{0, 0, 0, 0}

	t.Run("square downscale center", func(t *testing.T) {
		src := filled(100, 100, red)
		dst, err := CenterResize(src, 50, 50)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if dst.Bounds().Dx() != 50 || dst.Bounds().Dy() != 50 {
			t.Fatalf("size")
		}
		if got := rgba(dst.At(25, 25)); got != red {
			t.Errorf("center")
		}
		if got := rgba(dst.At(0, 0)); got != red {
			t.Errorf("corner tl")
		}
		if got := rgba(dst.At(49, 49)); got != red {
			t.Errorf("corner br")
		}
	})

	t.Run("wide letterbox vertical", func(t *testing.T) {
		src := filled(200, 100, red)
		dst, err := CenterResize(src, 100, 100)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if dst.Bounds().Dx() != 100 || dst.Bounds().Dy() != 100 {
			t.Fatalf("size")
		}
		if got := rgba(dst.At(0, 0)); got != black {
			t.Errorf("top pad")
		}
		if got := rgba(dst.At(50, 50)); got != red {
			t.Errorf("center")
		}
		if got := rgba(dst.At(0, 99)); got != black {
			t.Errorf("bottom pad")
		}
	})

	t.Run("tall letterbox horizontal", func(t *testing.T) {
		src := filled(100, 200, red)
		dst, err := CenterResize(src, 100, 100)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if dst.Bounds().Dx() != 100 || dst.Bounds().Dy() != 100 {
			t.Fatalf("size")
		}
		if got := rgba(dst.At(0, 0)); got != black {
			t.Errorf("left pad")
		}
		if got := rgba(dst.At(50, 50)); got != red {
			t.Errorf("center")
		}
		if got := rgba(dst.At(99, 0)); got != black {
			t.Errorf("right pad")
		}
	})

	t.Run("upscale no letterbox", func(t *testing.T) {
		src := filled(50, 50, red)
		dst, err := CenterResize(src, 100, 100)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if dst.Bounds().Dx() != 100 || dst.Bounds().Dy() != 100 {
			t.Fatalf("size")
		}
		if got := rgba(dst.At(0, 0)); got != red {
			t.Errorf("corner tl")
		}
		if got := rgba(dst.At(99, 0)); got != red {
			t.Errorf("corner tr")
		}
		if got := rgba(dst.At(0, 99)); got != red {
			t.Errorf("corner bl")
		}
		if got := rgba(dst.At(99, 99)); got != red {
			t.Errorf("corner br")
		}
	})
}
