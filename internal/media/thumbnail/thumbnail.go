package thumbnail

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

var (
	ThumbnailWidth  = 300
	ThumbnailHeight = 300
	ThumbnailSuffix = "thumb"
)

func IsThumbnail(imgPath string) bool {
	base := path.Base(imgPath)
	ext := path.Ext(base)
	name := base[:len(base)-len(ext)]
	return strings.HasSuffix(name, "_"+ThumbnailSuffix)
}

// CreateThumbnail by looking at the image MIMEtype and then take
// appropriate actions to create it.
//
// For images, scale the image to 300x300px without changing the ratio
// by first scaling it by the smallest side too 300px, then
// cropping/trimming the excess of the longest side
func CreateThumbnail(i model.Item) (model.Image, error) {
	// Simply return if it seems like the image is itself a thumbnail to
	// avoid recursive thumbnail creation
	if IsThumbnail(i.Path) {
		return model.Image{}, nil
	}
	ancli.Noticef("attempting to create thumbnail for: '%v'", i.Name)
	// WIP: Detect file type and delegate
	switch i.MIMEType {
	case "image/jpeg", "image/png", "image/gif":
		return createImageThumbnail(i)
	case "video/mp4", "video/webm":
		// WIP: Video thumbnail generation
		return model.Image{}, errors.New("thumbnail creation for video is not yet implemented")
	default:
		return model.Image{}, errors.New("unhandled MIMEtype")
	}
}

func GetThumbnailPath(mediaPath string) string {
	base := path.Base(mediaPath)
	ext := path.Ext(base)
	name := base[:len(base)-len(ext)]
	thumbName := name + "_thumb" + ext
	return path.Join(path.Dir(mediaPath), thumbName)
}

func createImageThumbnail(i model.Item) (model.Image, error) {
	full, err := LoadImage(i.Path)
	if err != nil {
		return model.Image{}, fmt.Errorf("createImageThumbnail failed to LoadImage: %err", err)
	}
	thumbRaw, err := CenterResize(full.Raw, ThumbnailWidth, ThumbnailHeight)
	if err != nil {
		return model.Image{}, fmt.Errorf("createImageThumbnail failed to CenterResize: %v", err)
	}

	thumbPath := GetThumbnailPath(i.Path)
	err = SaveImage(thumbRaw, full.Encoding, thumbPath)
	if err != nil {
		return model.Image{}, fmt.Errorf("createImageThumbnail failed to SaveImage: %v", err)
	}

	thumb := model.Image{
		Width:  ThumbnailWidth,
		Height: ThumbnailHeight,
		Path:   thumbPath,
		Raw:    thumbRaw,
	}

	ancli.Okf("thmbnail for: '%v' created at: '%v'", i.Name, thumb.Path)
	return thumb, nil
}
