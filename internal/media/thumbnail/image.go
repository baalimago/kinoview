package thumbnail

import (
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"

	"github.com/baalimago/kinoview/internal/model"
)

// LoadImage by:
// 1. ensuring there's a file on the path
// 2. determine encoding, width and height
// 3. encode to image.Image to allow working downstream
func LoadImage(imgPath string) (model.Image, error) {
	f, err := os.Open(imgPath)
	if err != nil {
		return model.Image{}, err
	}
	defer f.Close()
	imgCfg, encoding, err := image.DecodeConfig(f)
	if err != nil {
		return model.Image{}, err
	}

	// reset file offset after DecodeConfig
	_, err = f.Seek(0, 0)
	if err != nil {
		return model.Image{}, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return model.Image{}, err
	}

	return model.Image{
		Path:     imgPath,
		Width:    imgCfg.Width,
		Height:   imgCfg.Height,
		Encoding: encoding,
		Raw:      img,
	}, nil
}

// SaveImage by:
// 1. Opening file
// 2. Writing image using preferred encoding
func SaveImage(img image.Image, encoding string, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	switch encoding {
	case "jpeg", "jpg":
		return jpeg.Encode(f, img, nil)
	case "png":
		return png.Encode(f, img)
	case "gif":
		return gif.Encode(f, img, nil)
	default:
		return fmt.Errorf("unsupported encoding: %s", encoding)
	}
}

// CenterResize by:
//  1. Finding the smallest dimension (width or height)
//  2. Scaling the image down while keeping aspect ratio
//  3. Cropping excess from largest dimension (width or height) by cutting an equal amount
//     on both sides, leaving the image centered
//
// 4. Store the resized version with suffix
func CenterResize(src image.Image, width int, height int) (image.Image, error) {
	srcBounds := src.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	scale := float64(srcW) / float64(width)
	if float64(srcH)/float64(height) > scale {
		scale = float64(srcH) / float64(height)
	}
	newW := int(float64(srcW) / scale)
	newH := int(float64(srcH) / scale)
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	offX := (newW - width) / 2
	offY := (newH - height) / 2
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := int(float64(x+offX) * scale)
			srcY := int(float64(y+offY) * scale)
			if srcX >= 0 && srcX < srcW && srcY >= 0 && srcY < srcH {
				dst.Set(x, y, src.At(srcX, srcY))
			}
		}
	}
	return dst, nil
}
