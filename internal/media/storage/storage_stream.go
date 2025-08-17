package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// This is to allow for testing
var ffmpegLookPath = "ffmpeg"

func streamMkvToMp4(w http.ResponseWriter, r *http.Request, pathToMkv string) {
	ancli.Noticef("starting resilient conversion to mp4...")

	// Check if ffmpeg is installed
	_, err := exec.LookPath(ffmpegLookPath)
	if err != nil {
		http.Error(w, "internal server error: ffmpeg must be installed", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	// Check if mkv file exists
	if _, err := os.Stat(pathToMkv); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "mkv file does not exist", http.StatusInternalServerError)
			return
		}
		http.Error(w, "error checking mkv file", http.StatusInternalServerError)
		return
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 1)
	cmd := exec.CommandContext(ctx, ffmpegLookPath, "-y", "-i", pathToMkv,
		"-f", "mp4", "-movflags", "frag_keyframe+empty_moov",
		"-vcodec", "libx264", "-preset", "veryfast",
		"-acodec", "aac", "-strict", "-2", "pipe:1")

	tmpStderr, _ := os.CreateTemp("", "ffmpeg_stderr_*.log")
	cmd.Stderr = tmpStderr
	ancli.Noticef("progress at: %v", tmpStderr.Name())

	// Start ffmpeg with stdout piped
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pipeWriter.Close()

		cmd.Stdout = pipeWriter

		if err := cmd.Run(); err != nil {
			select {
			case errChan <- err:
			default:
			}
		}
	}()

	// Set headers
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")

	// Stream with retry logic
	buffer := make([]byte, 32*1024*1024)
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errChan:
			ancli.Errf("ffmpeg error: %v", err)
			http.Error(w, "conversion failed",
				http.StatusInternalServerError)
			return
		default:
		}

		n, err := pipeReader.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				ancli.Errf("client disconnected: %v", writeErr)
				cmd.Process.Kill()
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			ancli.Errf("pipe read error: %v", err)
			return
		}
	}

	wg.Wait()
}

type ffmpegSubsUtil struct {
	// mediaCache of pre-scanned media, allowing for validation and speedup
	mediaCache map[string]model.MediaInfo
	subsCache  map[string]string
}

func (f *ffmpegSubsUtil) find(item model.Item) (info model.MediaInfo, err error) {
	info, exists := f.mediaCache[item.ID]
	if exists {
		return
	}
	cmd := exec.Command("ffprobe",
		item.Path,
		"-v",
		"quiet",
		"-print_format",
		"json",
		"-show_streams",
		"-select_streams",
		"s",
	)
	tmpStderr, _ := os.CreateTemp("", "ffmpeg_sub_stderr_*.log")
	cmd.Stderr = tmpStderr
	var out bytes.Buffer
	cmd.Stdout = &out
	ancli.Noticef("subs conversion ffmpeg info: %v", tmpStderr.Name())
	err = cmd.Run()
	if err != nil {
		err = fmt.Errorf("failed to extract media info: %w", err)
		return
	}
	err = json.Unmarshal(out.Bytes(), &info)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal media info: %w", err)
		return
	}
	f.mediaCache[item.ID] = info
	return
}

func (f *ffmpegSubsUtil) extract(item model.Item, streamIndex string) (pathToSubs string, err error) {
	pathToSubs, exists := f.subsCache[item.ID]
	if exists {
		return
	}
	subs, err := os.CreateTemp("", "*.vtt")
	defer func() {
		closeErr := subs.Close()
		if closeErr != nil {
			ancli.Errf("failed to close subs file: %v", closeErr)
		}
	}()
	if err != nil {
		err = fmt.Errorf("failed to create temp sub file: %w", err)
		return
	}
	mapArg := "0:" + streamIndex
	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", item.Path,
		"-map", mapArg,
		"-f", "webvtt",
		subs.Name())
	convOutp, err := os.CreateTemp("", "subs_convert_*.log")
	defer func() {
		convCloseErr := convOutp.Close()
		if convCloseErr != nil {
			ancli.Errf("failed to close subs conversion output file: %v", convCloseErr)
		}
	}()
	ancli.Noticef("subs extract info at: %v", convOutp.Name())
	cmd.Stderr = convOutp

	if err = cmd.Run(); err != nil {
		err = fmt.Errorf("subtitle extraction failed: %w", err)
		return
	}

	pathToSubs = subs.Name()
	return
}
