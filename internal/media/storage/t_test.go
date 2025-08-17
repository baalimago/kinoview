package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/baalimago/kinoview/internal/model"
)

// mockClassifier is a mock implementation of the Classifier interface.
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

// Mock implementations for testing
type mockSubtitleStreamFinder struct{}

func (m *mockSubtitleStreamFinder) find(item model.Item) (model.MediaInfo, error) {
	return model.MediaInfo{}, nil
}

type mockSubtitleStreamExtractor struct{}

func (m *mockSubtitleStreamExtractor) extract(item model.Item, streamIndex string) (string, error) {
	return "", nil
}
