package media

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
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

	// Start ffmpeg with stdout piped
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pipeWriter.Close()

		cmd.Stdout = pipeWriter
		cmd.Stderr = os.Stderr

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
