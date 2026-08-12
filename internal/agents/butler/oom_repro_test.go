package butler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

// TestRepro_EndlessReasoningStreamHonorsDeadline reproduces the production
// OOM scenario end-to-end through the real clai v1.10.18 stack: the butler's
// picker call runs against a local SSE server that emits one tool call and
// then streams reasoning tokens forever, never sending [DONE]. runCascade
// wraps PrepSuggestions in a context.WithTimeout; the test asserts
// PrepSuggestions returns shortly after the deadline instead of hanging and
// accumulating reasoning text unboundedly.
//
// Gated behind REPRO_OOM=1 so the normal QA run stays fast (the test holds a
// server open for ~4s). Run with:
//
//	REPRO_OOM=1 go test ./internal/agents/butler -run TestRepro_EndlessReasoningStreamHonorsDeadline -v
func TestRepro_EndlessReasoningStreamHonorsDeadline(t *testing.T) {
	if os.Getenv("REPRO_OOM") == "" {
		t.Skip("set REPRO_OOM=1 to run the OOM repro")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)

		// One tool call to "date" (a real clai built-in tool), then endless
		// reasoning tokens. Never [DONE].
		toolChunk := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"date","arguments":"{}"}}]}}]}` + "\n\n"
		if _, err := fmt.Fprint(w, toolChunk); err != nil {
			return
		}
		fl.Flush()
		time.Sleep(50 * time.Millisecond)

		chunk := `data: {"choices":[{"delta":{"reasoning_content":"thinking token %d about the office ..."}}]}` + "\n\n"
		for i := 0; ; i++ {
			if _, err := fmt.Fprintf(w, chunk, i); err != nil {
				return
			}
			fl.Flush()
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer srv.Close()

	tmp := t.TempDir()
	claiDir := filepath.Join(tmp, "clai")
	if err := os.MkdirAll(claiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"url": srv.URL + "/chat/completions"}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(claiDir, "openai_gpt_gpt-repro.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "test-key")

	alfred := New(models.Configurations{
		Model:         "gpt-repro",
		ConfigDir:     tmp,
		InternalTools: []models.ToolName{},
		Out:           io.Discard,
	}, nil)

	items := []model.Item{
		{Name: "The.Office.S06E23.1080p.BluRay.x265-RARBG.mp4", MIMEType: "video/mp4", ID: "a"},
		{Name: "The.Office.S06E24.1080p.BluRay.x265-RARBG.mp4", MIMEType: "video/mp4", ID: "b"},
	}
	clientCtx := model.ClientContext{LastPlayedName: items[0].Name}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	var recs []model.Suggestion
	var prepErr error
	go func() {
		recs, prepErr = alfred.PrepSuggestions(ctx, clientCtx, items)
		close(done)
	}()

	select {
	case <-done:
		t.Logf("PrepSuggestions returned after deadline; recs=%d prepErr=%v", len(recs), prepErr)
	case <-time.After(15 * time.Second):
		t.Fatal("HANG: PrepSuggestions did not return 15s after the 3s deadline — the OOM root cause is reproduced")
	}
}

// TestRepro_HeapGrowthPerReasoningByte measures how much Go heap the real
// clai v1.10.18 stack consumes per byte of streamed reasoning content, using
// the exact production path (butler picker) against a local SSE server that
// streams reasoning forever. This settles whether unbounded reasoning
// accumulation can explain the production 2.53 GB heap.
//
// The server streams chunks as fast as the client reads them (no sleep). The
// test samples runtime.MemStats every 500ms and reports the growth rate.
//
//	REPRO_OOM=1 go test ./internal/agents/butler -run TestRepro_HeapGrowthPerReasoningByte -v -timeout 180s
func TestRepro_HeapGrowthPerReasoningByte(t *testing.T) {
	if os.Getenv("REPRO_OOM") == "" {
		t.Skip("set REPRO_OOM=1 to run the OOM repro")
	}
	const chunkBytes = 4 * 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		// Endless reasoning: one large chunk, no delay. The client reads as
		// fast as the network delivers.
		content := strings.Repeat("reasoning-token-", chunkBytes/16)
		for {
			if _, err := fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":%q}}]}\n\n", content); err != nil {
				return
			}
			fl.Flush()
		}
	}))
	defer srv.Close()

	tmp := t.TempDir()
	claiDir := filepath.Join(tmp, "clai")
	if err := os.MkdirAll(claiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"url": srv.URL + "/chat/completions"}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(claiDir, "openai_gpt_gpt-repro.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "test-key")

	alfred := New(models.Configurations{
		Model:         "gpt-repro",
		ConfigDir:     tmp,
		InternalTools: []models.ToolName{},
		Out:           io.Discard,
	}, nil)

	items := []model.Item{
		{Name: "The.Office.S06E23.1080p.BluRay.x265-RARBG.mp4", MIMEType: "video/mp4", ID: "a"},
		{Name: "The.Office.S06E24.1080p.BluRay.x265-RARBG.mp4", MIMEType: "video/mp4", ID: "b"},
	}
	clientCtx := model.ClientContext{LastPlayedName: items[0].Name}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	start := time.Now()
	done := make(chan struct{})
	go func() {
		_, _ = alfred.PrepSuggestions(ctx, clientCtx, items)
		close(done)
	}()

	var lastHeap, lastSys uint64
	var peakHeapSys uint64
	lastSample := time.Now()
	for range 100 {
		select {
		case <-done:
			t.Logf("PrepSuggestions returned after %v", time.Since(start))
			return
		case <-time.After(500 * time.Millisecond):
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		dt := time.Since(lastSample).Seconds()
		if lastHeap > 0 {
			heapGrowth := float64(int64(m.HeapInuse)-int64(lastHeap)) / dt
			sysGrowth := float64(int64(m.Sys)-int64(lastSys)) / dt
			t.Logf("t=%4.1fs heapInuse=%9.1fMiB heapSys=%9.1fMiB heapGrowth=%9.1fKiB/s sysGrowth=%9.1fKiB/s",
				time.Since(start).Seconds(), float64(m.HeapInuse)/1048576, float64(m.HeapSys)/1048576,
				heapGrowth/1024, sysGrowth/1024)
		}
		if m.HeapSys > peakHeapSys {
			peakHeapSys = m.HeapSys
		}
		lastHeap, lastSys, lastSample = m.HeapInuse, m.Sys, time.Now()
	}
	// Sampling window done; wait for the deadline to fire. The heap bound is
	// the regression guard: unpatched clai v1.10.18 grew heapSys past 599 MiB
	// within this window (and kept going); the capped fork stays ~47 MiB. 256
	// MiB gives 5x headroom over the fixed code while staying far below the
	// unfixed trajectory — a reintroduced unbounded accumulator fails here.
	if peakHeapSys > 256*1048576 {
		t.Fatalf("heapSys peaked at %.1f MiB in the sampling window — reasoning accumulation is unbounded again", float64(peakHeapSys)/1048576)
	}
	select {
	case <-done:
		t.Logf("PrepSuggestions returned after %v (ctx deadline 120s)", time.Since(start))
	case <-time.After(90 * time.Second):
		t.Fatalf("HANG: PrepSuggestions did not return 90s after the sampling window (ctx expired at 120s) — the GC-thrash death spiral stalls ctx.Done()")
	}
}
