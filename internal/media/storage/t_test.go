package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

type mockClassifier struct {
	SetupFunc    func(context.Context) error
	ClassifyFunc func(context.Context, model.Item) (model.Item, error)
}

func (m *mockClassifier) Setup(ctx context.Context) error {
	if m.SetupFunc != nil {
		return m.SetupFunc(ctx)
	}
	return nil
}

func (m *mockClassifier) Classify(ctx context.Context, item model.Item) (model.Item, error) {
	if m.ClassifyFunc != nil {
		return m.ClassifyFunc(ctx, item)
	}
	return item, nil
}

func mockHTTPRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	return req
}

type mockResponseWriter struct {
	header     http.Header
	statusCode int
	buffer     []byte
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{header: make(http.Header)}
}

func (m *mockResponseWriter) Header() http.Header {
	return m.header
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	m.buffer = append(m.buffer, b...)
	return len(b), nil
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}

type mockSubtitleManager struct {
	shouldFail   bool
	shouldReturn model.MediaInfo
}

func (m *mockSubtitleManager) ExtractSubtitles(item model.Item, streamIndex string) (string, error) {
	if m.shouldFail {
		return "", errors.New("whopsidops")
	}
	return "", nil
}

func (m *mockSubtitleManager) Find(item model.Item) (model.MediaInfo, error) {
	if m.shouldFail {
		return model.MediaInfo{}, errors.New("whopsidops")
	}
	return m.shouldReturn, nil
}

func newTestStore(t *testing.T) *store {
	t.Helper()
	s := NewStore(WithStorePath(t.TempDir()))
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			return i, nil
		},
	}
	return s
}
