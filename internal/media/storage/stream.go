package storage

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

// This is to allow for testing
var ffmpegLookPath = "ffmpeg"

// parseStartSeconds reads the optional `t` query parameter (seconds, may be
// fractional) used to seek within a transcoded stream. Invalid or negative
// values are treated as 0 (start from the beginning).
func parseStartSeconds(r *http.Request) float64 {
	raw := r.URL.Query().Get("t")
	if raw == "" {
		return 0
	}
	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil || sec < 0 {
		return 0
	}
	return sec
}

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

	// Build ffmpeg args. When a start offset is requested we place -ss *before*
	// -i for fast (keyframe) input seeking, and reset timestamps so the client
	// receives a stream that begins at ~0. This lets the browser seek within a
	// fragmented stream by requesting a fresh transcode from the target point,
	// instead of stalling on a Range request the pipe can't satisfy.
	args := []string{"-y"}
	startSec := parseStartSeconds(r)
	if startSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	args = append(args, "-i", pathToMkv)
	if startSec > 0 {
		args = append(args, "-avoid_negative_ts", "make_zero")
	}
	args = append(args,
		"-f", "mp4", "-movflags", "frag_keyframe+empty_moov",
		"-vcodec", "libx264", "-preset", "veryfast",
		"-acodec", "aac", "-strict", "-2", "pipe:1")
	cmd := exec.CommandContext(ctx, ffmpegLookPath, args...)

	tmpStderr, _ := os.CreateTemp("", "ffmpeg_stderr_*.log")
	cmd.Stderr = tmpStderr
	ancli.Noticef("progress at: %v", tmpStderr.Name())

	// Start ffmpeg with stdout piped
	wg.Go(func() {
		defer pipeWriter.Close()

		cmd.Stdout = pipeWriter

		if err := cmd.Run(); err != nil {
			select {
			case errChan <- err:
			default:
			}
		}
	})

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
