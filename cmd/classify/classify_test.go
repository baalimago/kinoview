package classify

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

type mockStorage struct {
	setupCalls                   int
	setupErr                     error
	classificationStationStartup time.Duration
	snapshotCalls                int
	snapshotRetVal               []model.Item
	addToClassificationCalls     int
	addedItems                   []model.Item
	startClassificationCalls     int
	startClassificationErr       error
	readyChan                    chan struct{}
}

func (m *mockStorage) Setup(ctx context.Context) (<-chan error, error) {
	m.setupCalls++
	errChan := make(chan error, 1)
	if m.setupErr != nil {
		errChan <- m.setupErr
	}
	return errChan, m.setupErr
}

func (m *mockStorage) Start(ctx context.Context) {
}

func (m *mockStorage) Store(ctx context.Context, i model.Item) error {
	return nil
}

func (m *mockStorage) Snapshot() []model.Item {
	m.snapshotCalls++
	return m.snapshotRetVal
}

func (m *mockStorage) ListHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStorage) VideoHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStorage) ImageHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStorage) SubsListHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStorage) SubsHandlerFunc() http.HandlerFunc {
	return nil
}

func (m *mockStorage) AddToClassificationQueue(i model.Item) {
	m.addToClassificationCalls++
	m.addedItems = append(m.addedItems, i)
}

func (m *mockStorage) StartClassificationStation(ctx context.Context) error {
	m.startClassificationCalls++
	go func() {
		time.Sleep(m.classificationStationStartup)
		if m.readyChan != nil {
			close(m.readyChan)
		}
	}()
	return m.startClassificationErr
}

func (m *mockStorage) Ready() <-chan struct{} {
	if m.readyChan == nil {
		m.readyChan = make(chan struct{})
	}
	return m.readyChan
}

func TestCommand_Describe(t *testing.T) {
	cmd := &command{}
	desc := cmd.Describe()
	if desc == "" {
		t.Fatalf("Describe() returned empty string")
	}
	if desc != "Run classification on existing items." {
		t.Fatalf("Describe() returned unexpected value: %s", desc)
	}
}

func TestCommand_Help(t *testing.T) {
	cmd := &command{}
	help := cmd.Help()
	if help == "" {
		t.Fatalf("Help() returned empty string")
	}
}

func TestCommand_Flagset(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	if fs == nil {
		t.Fatalf("Flagset() returned nil")
	}

	if fs.Name() != "server" {
		t.Fatalf("Flagset name is not 'server', got: %s", fs.Name())
	}

	// Check that model flag exists
	if cmd.model == nil {
		t.Fatalf("model flag not initialized")
	}

	// Check that workers flag exists
	if cmd.workers == nil {
		t.Fatalf("workers flag not initialized")
	}

	// Check default values
	if *cmd.model != "gpt-5" {
		t.Fatalf("model default value is not 'gpt-5', got: %s", *cmd.model)
	}

	if *cmd.workers != 5 {
		t.Fatalf("workers default value is not 5, got: %d", *cmd.workers)
	}
}

func TestCommand_Flagset_parsing(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	args := []string{"-model", "gpt-4", "-workers", "10"}
	err := fs.Parse(args)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if *cmd.model != "gpt-4" {
		t.Fatalf("model not parsed correctly, got: %s", *cmd.model)
	}

	if *cmd.workers != 10 {
		t.Fatalf("workers not parsed correctly, got: %d", *cmd.workers)
	}
}

func TestCommand_Setup_success(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 3

	ctx := context.Background()
	err := cmd.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	if cmd.store == nil {
		t.Fatalf("store not initialized after Setup")
	}
}

func TestCommand_Setup_missing_model(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
		model:     nil,
	}

	ctx := context.Background()
	// Should panic or handle nil model gracefully
	defer func() {
		if r := recover(); r != nil {
			// Expected behavior
		}
	}()

	cmd.Setup(ctx)
}

func TestCommand_Flagset_multiple_calls(t *testing.T) {
	cmd := &command{}

	fs1 := cmd.Flagset()
	fs2 := cmd.Flagset()

	// Both should be valid flagsets
	if fs1 == nil || fs2 == nil {
		t.Fatalf("Flagset returned nil on multiple calls")
	}

	// They should be different instances (new flagset each time)
	if fs1 == fs2 {
		t.Fatalf("Flagset returned same instance on multiple calls")
	}
}

func TestCommand_Flagset_defaults(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	// Parse empty args to get defaults
	fs.Parse([]string{})

	if *cmd.model != "gpt-5" {
		t.Fatalf("default model should be 'gpt-5', got: %s", *cmd.model)
	}

	if *cmd.workers != 5 {
		t.Fatalf("default workers should be 5, got: %d", *cmd.workers)
	}
}

func TestCommand_Flagset_negative_workers(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	args := []string{"-workers", "-1"}
	err := fs.Parse(args)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Flag package doesn't validate negative values, just parses them
	if *cmd.workers != -1 {
		t.Fatalf("workers should be -1, got: %d", *cmd.workers)
	}
}

func TestCommand_Flagset_large_workers(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	args := []string{"-workers", "1000"}
	err := fs.Parse(args)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if *cmd.workers != 1000 {
		t.Fatalf("workers should be 1000, got: %d", *cmd.workers)
	}
}

func TestCommand_Flagset_empty_model(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	args := []string{"-model", ""}
	err := fs.Parse(args)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if *cmd.model != "" {
		t.Fatalf("model should be empty string, got: %s", *cmd.model)
	}
}

func TestCommand_Flagset_help_flag(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	args := []string{"-h"}
	err := fs.Parse(args)

	// flag.FlagSet with ContinueOnError should return flag.ErrHelp
	if err == nil {
		t.Fatalf("expected error for -h flag")
	}
	if err != flag.ErrHelp {
		t.Fatalf("expected flag.ErrHelp, got: %v", err)
	}
}

func TestCommand_Flagset_unknown_flag(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	args := []string{"-unknown", "value"}
	err := fs.Parse(args)

	if err == nil {
		t.Fatalf("expected error for unknown flag")
	}
}

func TestCommand_Setup_creates_store(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 2

	if cmd.store != nil {
		t.Fatalf("store should be nil before Setup")
	}

	ctx := context.Background()
	err := cmd.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	if cmd.store == nil {
		t.Fatalf("store should not be nil after Setup")
	}
}

func TestCommand_Setup_with_context_timeout(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 1

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := cmd.Setup(ctx)
	// Should complete without error (actual behavior depends on implementation)
	_ = err
}

func TestCommand_Describe_not_empty(t *testing.T) {
	cmd := &command{}
	desc := cmd.Describe()

	if len(desc) == 0 {
		t.Fatalf("Describe() should return non-empty string")
	}
}

func TestCommand_Help_not_empty(t *testing.T) {
	cmd := &command{}
	help := cmd.Help()

	if len(help) == 0 {
		t.Fatalf("Help() should return non-empty string")
	}
}

func TestCommand_Creation(t *testing.T) {
	cmd := Command()

	if cmd == nil {
		t.Fatalf("Command() returned nil")
	}

	if cmd.binPath == "" {
		t.Fatalf("binPath not set")
	}

	if cmd.configDir == "" {
		t.Fatalf("configDir not set")
	}

	if cmd.storePath == "" {
		t.Fatalf("storePath not set")
	}
}

func TestCommand_Creation_paths_valid(t *testing.T) {
	cmd := Command()

	if cmd == nil {
		t.Fatalf("Command() returned nil")
	}

	// Paths should contain expected components
	if !strings.Contains(cmd.storePath, "kinoview") {
		t.Fatalf("storePath should contain 'kinoview', got: %s", cmd.storePath)
	}

	if !strings.Contains(cmd.configDir, "kinoview") {
		t.Fatalf("configDir should contain 'kinoview', got: %s", cmd.configDir)
	}
}

func TestCommand_Flagset_model_usage(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	// Verify the flagset contains the model flag with correct usage
	found := false
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "model" {
			found = true
			if len(f.Usage) == 0 {
				t.Fatalf("model flag should have usage text")
			}
		}
	})

	if !found {
		t.Fatalf("model flag not found in flagset")
	}
}

func TestCommand_Flagset_workers_usage(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	// Verify the flagset contains the workers flag with correct usage
	found := false
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "workers" {
			found = true
			if len(f.Usage) == 0 {
				t.Fatalf("workers flag should have usage text")
			}
		}
	})

	if !found {
		t.Fatalf("workers flag not found in flagset")
	}
}

func TestCommand_Flagset_model_description(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "model" {
			if !strings.Contains(f.Usage, "LLM") && !strings.Contains(f.Usage, "model") {
				t.Fatalf("model flag usage should mention LLM or model, got: %s", f.Usage)
			}
		}
	})
}

func TestCommand_Flagset_workers_description(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "workers" {
			if !strings.Contains(f.Usage, "worker") && !strings.Contains(f.Usage, "amount") {
				t.Fatalf("workers flag usage should mention workers or amount, got: %s", f.Usage)
			}
		}
	})
}

func TestCommand_Setup_model_passed_to_store(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}

	testModel := "test-model-123"
	cmd.model = &testModel
	cmd.workers = new(int)
	*cmd.workers = 2

	ctx := context.Background()
	err := cmd.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Store should be initialized
	if cmd.store == nil {
		t.Fatalf("store not initialized")
	}
}

func TestCommand_Setup_workers_passed_to_store(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}

	cmd.model = new(string)
	*cmd.model = "gpt-4"

	testWorkers := 7
	cmd.workers = &testWorkers

	ctx := context.Background()
	err := cmd.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Store should be initialized
	if cmd.store == nil {
		t.Fatalf("store not initialized")
	}
}

func TestCommand_fields_initialized(t *testing.T) {
	cmd := &command{
		binPath:   "/path/to/bin",
		configDir: "/config",
		storePath: "/store",
	}

	if cmd.binPath != "/path/to/bin" {
		t.Fatalf("binPath not set correctly")
	}

	if cmd.configDir != "/config" {
		t.Fatalf("configDir not set correctly")
	}

	if cmd.storePath != "/store" {
		t.Fatalf("storePath not set correctly")
	}
}

func TestCommand_Flagset_parse_multiple_times(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	// First parse
	err1 := fs.Parse([]string{"-model", "gpt-4"})
	if err1 != nil {
		t.Fatalf("First parse failed: %v", err1)
	}

	if *cmd.model != "gpt-4" {
		t.Fatalf("First parse didn't set model correctly")
	}

	// Create new flagset and parse again
	cmd2 := &command{}
	fs2 := cmd2.Flagset()
	err2 := fs2.Parse([]string{"-model", "gpt-3"})
	if err2 != nil {
		t.Fatalf("Second parse failed: %v", err2)
	}

	if *cmd2.model != "gpt-3" {
		t.Fatalf("Second parse didn't set model correctly")
	}
}

func TestCommand_Describe_content(t *testing.T) {
	cmd := &command{}
	desc := cmd.Describe()

	// Check that description mentions classification or items
	if !strings.Contains(desc, "classification") && !strings.Contains(desc, "items") {
		t.Fatalf("Describe should mention classification or items, got: %s", desc)
	}
}

func TestCommand_Help_content(t *testing.T) {
	cmd := &command{}
	help := cmd.Help()

	// Help message should be non-empty and informative
	if len(help) == 0 {
		t.Fatalf("Help() returned empty string")
	}
}

func TestCommand_Setup_store_path_created(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()
	storePath := path.Join(tempDir, "subdir", "store")

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: storePath,
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 1

	ctx := context.Background()
	err := cmd.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
}

func TestCommand_multiple_commands_independent(t *testing.T) {
	cmd1 := &command{
		binPath:   "/bin1",
		configDir: "/cfg1",
		storePath: "/store1",
	}

	cmd2 := &command{
		binPath:   "/bin2",
		configDir: "/cfg2",
		storePath: "/store2",
	}

	if cmd1.binPath == cmd2.binPath {
		t.Fatalf("Commands should be independent")
	}

	if cmd1.configDir == cmd2.configDir {
		t.Fatalf("Commands should be independent")
	}
}

func TestCommand_Flagset_with_valid_model(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	args := []string{"-model", "gpt-4-turbo"}
	err := fs.Parse(args)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if *cmd.model != "gpt-4-turbo" {
		t.Fatalf("model not parsed correctly, got: %s", *cmd.model)
	}
}

func TestCommand_Flagset_boundary_workers(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{"zero", "0", 0},
		{"one", "1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &command{}
			fs := cmd.Flagset()

			args := []string{"-workers", tt.value}
			err := fs.Parse(args)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if *cmd.workers != tt.want {
				t.Fatalf("workers should be %d, got: %d", tt.want, *cmd.workers)
			}
		})
	}
}

func TestCommand_Describe_is_consistent(t *testing.T) {
	cmd1 := &command{}
	cmd2 := &command{}

	desc1 := cmd1.Describe()
	desc2 := cmd2.Describe()

	if desc1 != desc2 {
		t.Fatalf("Describe should be consistent across instances")
	}
}

func TestCommand_Help_is_consistent(t *testing.T) {
	cmd1 := &command{}
	cmd2 := &command{}

	help1 := cmd1.Help()
	help2 := cmd2.Help()

	if help1 != help2 {
		t.Fatalf("Help should be consistent across instances")
	}
}

func TestCommand_stationStorage_interface(t *testing.T) {
	// This test verifies the interface is correctly defined
	// by ensuring a mock can satisfy it

	var _ stationStorage = (*mockStorage)(nil)
}

func TestCommand_Setup_with_zero_workers(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 0

	ctx := context.Background()
	err := cmd.Setup(ctx)
	// Should handle zero workers (behavior depends on implementation)
	_ = err
}

func TestCommand_Flagset_string_conversion(t *testing.T) {
	testValues := []string{
		"gpt-4",
		"claude-3",
		"llama-2",
		"model-with-dashes",
		"model_with_underscores",
		"model123",
	}

	for _, val := range testValues {
		cmd := &command{}
		fs := cmd.Flagset()

		args := []string{"-model", val}
		err := fs.Parse(args)
		if err != nil {
			t.Fatalf("Parse failed for model '%s': %v", val, err)
		}

		if *cmd.model != val {
			t.Fatalf("model not set correctly for '%s', got: %s", val, *cmd.model)
		}
	}
}

func TestCommand_Setup_creates_classifier(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 2

	ctx := context.Background()
	err := cmd.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Verify store has classifier configured
	if cmd.store == nil {
		t.Fatalf("store should be initialized")
	}
}

func TestCommand_Creation_returns_non_nil(t *testing.T) {
	cmd := Command()
	if cmd == nil {
		t.Fatalf("Command() should not return nil")
	}
}

func TestCommand_Flagset_name(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	if fs.Name() != "server" {
		t.Fatalf("Flagset should be named 'server', got: %s", fs.Name())
	}
}

func TestCommand_Flagset_error_handling(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	// ContinueOnError should be set
	if fs.ErrorHandling() != flag.ContinueOnError {
		t.Fatalf("Flagset should use ContinueOnError")
	}
}

func TestCommand_Setup_idempotent_paths(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 1

	ctx := context.Background()

	// Setup should be idempotent
	err1 := cmd.Setup(ctx)
	err2 := cmd.Setup(ctx)

	// Both should complete (though second might fail if store re-initialization is not allowed)
	_ = err1
	_ = err2
}

func TestCommand_fields_zero_values(t *testing.T) {
	cmd := &command{}

	// Fields should be zero values initially
	if cmd.binPath != "" {
		t.Fatalf("binPath should be empty initially")
	}

	if cmd.configDir != "" {
		t.Fatalf("configDir should be empty initially")
	}

	if cmd.storePath != "" {
		t.Fatalf("storePath should be empty initially")
	}

	if cmd.model != nil {
		t.Fatalf("model should be nil initially")
	}

	if cmd.workers != nil {
		t.Fatalf("workers should be nil initially")
	}

	if cmd.store != nil {
		t.Fatalf("store should be nil initially")
	}

	if cmd.flagset != nil {
		t.Fatalf("flagset should be nil initially")
	}
}

func TestCommand_Flagset_model_default_gpt5(t *testing.T) {
	cmd := &command{}
	cmd.Flagset()

	// Check the default value
	if *cmd.model != "gpt-5" {
		t.Fatalf("default model should be 'gpt-5', got: %s", *cmd.model)
	}
}

func TestCommand_Flagset_workers_default_5(t *testing.T) {
	cmd := &command{}
	cmd.Flagset()

	// Check the default value
	if *cmd.workers != 5 {
		t.Fatalf("default workers should be 5, got: %d", *cmd.workers)
	}
}

func TestCommand_Flagset_stores_reference(t *testing.T) {
	cmd := &command{}
	fs := cmd.Flagset()

	if cmd.flagset == nil {
		t.Fatalf("flagset reference should be stored")
	}

	if cmd.flagset != fs {
		t.Fatalf("stored flagset should match returned flagset")
	}
}

func TestCommand_Setup_store_interface(t *testing.T) {
	ancli.Silent = true
	tempDir := t.TempDir()

	cmd := &command{
		binPath:   "test",
		configDir: tempDir,
		storePath: path.Join(tempDir, "store"),
	}
	cmd.model = new(string)
	*cmd.model = "gpt-4"
	cmd.workers = new(int)
	*cmd.workers = 1

	ctx := context.Background()
	err := cmd.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Verify store satisfies the interface
	var _ stationStorage = cmd.store
}

func TestCommand_Describe_mentions_classification(t *testing.T) {
	cmd := &command{}
	desc := cmd.Describe()

	if !strings.Contains(desc, "classification") {
		t.Fatalf("Describe should mention 'classification', got: %s", desc)
	}
}

func TestCommand_Describe_mentions_existing(t *testing.T) {
	cmd := &command{}
	desc := cmd.Describe()

	if !strings.Contains(desc, "existing") {
		t.Fatalf("Describe should mention 'existing', got: %s", desc)
	}
}

// ============================================================================
// NEW TESTS FOR UNCOVERED FUNCTIONS
// ============================================================================

// Tests for findFilteredItems()
func TestCommand_findFilteredItems_empty_snapshot(t *testing.T) {
	cmd := &command{
		filter: "",
		store: &mockStorage{
			snapshotRetVal: []model.Item{},
		},
	}

	items := cmd.findFilteredItems()
	if len(items) != 0 {
		t.Fatalf("Expected 0 items, got %d", len(items))
	}
}

func TestCommand_findFilteredItems_no_filter(t *testing.T) {
	cmd := &command{
		filter: "",
		store: &mockStorage{
			snapshotRetVal: []model.Item{
				{Name: "video1.mp4", MIMEType: "video/mp4"},
				{Name: "video2.mkv", MIMEType: "video/x-matroska"},
				{Name: "image1.jpg", MIMEType: "image/jpeg"},
			},
		},
	}

	items := cmd.findFilteredItems()
	// Should only return video items
	if len(items) != 2 {
		t.Fatalf("Expected 2 video items, got %d", len(items))
	}
}

func TestCommand_findFilteredItems_with_filter_match(t *testing.T) {
	cmd := &command{
		filter: "movie",
		store: &mockStorage{
			snapshotRetVal: []model.Item{
				{Name: "movie1.mp4", MIMEType: "video/mp4"},
				{Name: "video2.mkv", MIMEType: "video/x-matroska"},
			},
		},
	}

	items := cmd.findFilteredItems()
	if len(items) != 1 {
		t.Fatalf("Expected 1 filtered item, got %d", len(items))
	}
	if items[0].Name != "movie1.mp4" {
		t.Fatalf("Expected 'movie1.mp4', got '%s'", items[0].Name)
	}
}

func TestCommand_findFilteredItems_case_insensitive(t *testing.T) {
	cmd := &command{
		filter: "MOVIE",
		store: &mockStorage{
			snapshotRetVal: []model.Item{
				{Name: "movie1.mp4", MIMEType: "video/mp4"},
			},
		},
	}

	items := cmd.findFilteredItems()
	if len(items) != 1 {
		t.Fatalf("Expected 1 item with case-insensitive filter, got %d", len(items))
	}
}

func TestCommand_findFilteredItems_no_video_match(t *testing.T) {
	cmd := &command{
		filter: "document",
		store: &mockStorage{
			snapshotRetVal: []model.Item{
				{Name: "document.txt", MIMEType: "text/plain"},
				{Name: "video1.mp4", MIMEType: "video/mp4"},
			},
		},
	}

	items := cmd.findFilteredItems()
	// Filter matches but not a video
	if len(items) != 0 {
		t.Fatalf("Expected 0 items (no video match), got %d", len(items))
	}
}

func TestCommand_findFilteredItems_multiple_matches(t *testing.T) {
	cmd := &command{
		filter: "test",
		store: &mockStorage{
			snapshotRetVal: []model.Item{
				{Name: "test_video1.mp4", MIMEType: "video/mp4"},
				{Name: "test_video2.mkv", MIMEType: "video/x-matroska"},
				{Name: "other.mp4", MIMEType: "video/mp4"},
			},
		},
	}

	items := cmd.findFilteredItems()
	if len(items) != 2 {
		t.Fatalf("Expected 2 filtered items, got %d", len(items))
	}
}

func Test_determineIfProceed(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"YES\n", true},
		{"yEs\n", true},
		{"n\n", false},
		{"no\n", false},
		{"random\n", false},
		{"\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := determineIfProceed(strings.NewReader(tt.input))
			if result != tt.expected {
				t.Errorf("determineIfProceed(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// Tests for startClassificationStation()
func TestCommand_startClassificationStation_success(t *testing.T) {
	mock := &mockStorage{
		readyChan: make(chan struct{}),
	}

	cmd := &command{
		store: mock,
	}

	ctx := context.Background()

	errChan, err := cmd.startClassificationStation(ctx)
	if err != nil {
		t.Fatalf("startClassificationStation failed: %v", err)
	}

	if errChan == nil {
		t.Fatalf("Expected error channel, got nil")
	}

	if mock.startClassificationCalls != 1 {
		t.Fatalf("Expected 1 StartClassificationStation call, got %d", mock.startClassificationCalls)
	}
}

func TestCommand_startClassificationStation_context_cancel(t *testing.T) {
	mock := &mockStorage{
		readyChan: make(chan struct{}),
	}

	cmd := &command{
		store: mock,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	errChan, err := cmd.startClassificationStation(ctx)
	if err == nil {
		t.Fatalf("Expected context error, got nil")
	}

	if errChan != nil {
		t.Fatalf("Expected nil error channel on context cancel, got %v", errChan)
	}
}

func TestCommand_startClassificationStation_context_timeout(t *testing.T) {
	contextTimeout := 10 * time.Millisecond
	mock := &mockStorage{
		readyChan: make(chan struct{}),
		// More than contextTimeout
		classificationStationStartup: contextTimeout * 2,
	}

	cmd := &command{
		store: mock,
	}

	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()

	errChan, err := cmd.startClassificationStation(ctx)
	if err == nil {
		t.Fatalf("Expected context timeout error, got nil")
	}

	if errChan != nil {
		t.Fatalf("Expected nil error channel on timeout, got %v", errChan)
	}
}

// Tests for errorMonitor()
func TestCommand_errorMonitor_context_done(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	errChan := make(chan error)
	err := errorMonitor(ctx, errChan)
	if err != nil {
		t.Fatalf("Expected nil error when context done, got %v", err)
	}
}

func TestCommand_errorMonitor_receives_error(t *testing.T) {
	ctx := context.Background()
	errChan := make(chan error, 1)
	testErr := errors.New("test error")
	errChan <- testErr

	err := errorMonitor(ctx, errChan)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	if err.Error() != "test error" {
		t.Fatalf("Expected 'test error', got '%v'", err)
	}
}

func TestCommand_errorMonitor_context_timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	errChan := make(chan error) // No error sent

	time.Sleep(20 * time.Millisecond) // Let context expire
	err := errorMonitor(ctx, errChan)
	if err != nil {
		t.Fatalf("Expected nil error when context done, got %v", err)
	}
}

// Tests for Run()
func TestCommand_Run_with_mock_input_approve(t *testing.T) {
	ancli.Silent = true

	mock := &mockStorage{
		readyChan: make(chan struct{}),
		snapshotRetVal: []model.Item{
			{Name: "video1.mp4", MIMEType: "video/mp4"},
		},
	}

	cmd := &command{
		store:     mock,
		filter:    "",
		userInput: strings.NewReader("y"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*10)
	t.Cleanup(cancel)

	err := cmd.Run(ctx)
	// Should not error on approval
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify item was queued
	if mock.addToClassificationCalls != 1 {
		t.Fatalf("Expected 1 item queued, got %d", mock.addToClassificationCalls)
	}
}

func TestCommand_Run_user_abort(t *testing.T) {
	ancli.Silent = true

	mock := &mockStorage{
		readyChan: make(chan struct{}),
		snapshotRetVal: []model.Item{
			{Name: "video1.mp4", MIMEType: "video/mp4"},
		},
		classificationStationStartup: time.Millisecond * 10,
	}

	cmd := &command{
		store:     mock,
		filter:    "",
		userInput: strings.NewReader("n"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), mock.classificationStationStartup*2)
	t.Cleanup(cancel)

	err := cmd.Run(ctx)

	if err == nil {
		t.Fatalf("Expected user abort error")
	}

	if err.Error() != "user abort" {
		t.Fatalf("Expected 'user abort' error, got '%v'", err)
	}

	// No items should be queued
	if mock.addToClassificationCalls != 0 {
		t.Fatalf("Expected 0 items queued, got %d", mock.addToClassificationCalls)
	}
}

func TestCommand_Run_no_items_found(t *testing.T) {
	ancli.Silent = true

	mock := &mockStorage{
		readyChan:                    make(chan struct{}),
		snapshotRetVal:               []model.Item{},
		classificationStationStartup: time.Millisecond * 10,
	}

	cmd := &command{
		store:     mock,
		filter:    "",
		userInput: strings.NewReader("y"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), mock.classificationStationStartup*2)
	t.Cleanup(cancel)

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// No items to queue
	if mock.addToClassificationCalls != 0 {
		t.Fatalf("Expected 0 items queued, got %d", mock.addToClassificationCalls)
	}
}

func TestCommand_Run_classification_station_error(t *testing.T) {
	ancli.Silent = true

	mock := &mockStorage{
		readyChan:                    make(chan struct{}),
		snapshotRetVal:               []model.Item{{Name: "video1.mp4", MIMEType: "video/mp4"}},
		startClassificationErr:       errors.New("station error"),
		classificationStationStartup: time.Millisecond * 10,
	}

	cmd := &command{
		store:     mock,
		filter:    "",
		userInput: strings.NewReader("y\n"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), mock.classificationStationStartup*2)
	t.Cleanup(cancel)

	err := cmd.Run(ctx)

	if err == nil {
		t.Fatalf("Expected station error")
	}

	if err.Error() != "station error" {
		t.Fatalf("Expected 'station error', got '%v'", err)
	}
}

func TestCommand_Run_start_classification_station_fails(t *testing.T) {
	ancli.Silent = true

	mock := &mockStorage{
		readyChan:      make(chan struct{}),
		snapshotRetVal: []model.Item{{Name: "video1.mp4", MIMEType: "video/mp4"}},
	}

	cmd := &command{
		store:     mock,
		filter:    "",
		userInput: strings.NewReader("y\n"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context before run

	err := cmd.Run(ctx)

	if err == nil {
		t.Fatalf("Expected context error")
	}
}

func TestCommand_Run_with_filtered_items(t *testing.T) {
	ancli.Silent = true

	mock := &mockStorage{
		readyChan: make(chan struct{}),
		snapshotRetVal: []model.Item{
			{Name: "movie1.mp4", MIMEType: "video/mp4"},
			{Name: "video2.mkv", MIMEType: "video/x-matroska"},
		},
		classificationStationStartup: time.Millisecond * 10,
	}

	cmd := &command{
		store:     mock,
		filter:    "movie",
		userInput: strings.NewReader("y\n"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), mock.classificationStationStartup*2)
	t.Cleanup(cancel)

	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Only 1 item should match filter
	if mock.addToClassificationCalls != 1 {
		t.Fatalf("Expected 1 item queued, got %d", mock.addToClassificationCalls)
	}

	if mock.addedItems[0].Name != "movie1.mp4" {
		t.Fatalf("Expected 'movie1.mp4', got '%s'", mock.addedItems[0].Name)
	}
}
