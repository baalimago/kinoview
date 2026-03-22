package subtitles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

var ErrSubtitleNotFound = errors.New("subtitle not found")
var ErrBindingNotFound = errors.New("subtitle binding not found")

type jsonRepository struct {
	rootDir      string
	resourcesDir string
	bindingsDir  string

	mu           sync.RWMutex
	bySubtitleID map[string]model.SubtitleResource
	byItemID     map[string][]string
	bindings     map[string]model.SubtitleBinding
	byChecksum   map[string][]string
	bySourceRef  map[string]string
}

func NewRepository(rootDir string) (Repository, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("create subtitle repository: root dir is empty")
	}

	repo := &jsonRepository{
		rootDir:      rootDir,
		resourcesDir: filepath.Join(rootDir, "resources"),
		bindingsDir:  filepath.Join(rootDir, "bindings"),
		bySubtitleID: make(map[string]model.SubtitleResource),
		byItemID:     make(map[string][]string),
		bindings:     make(map[string]model.SubtitleBinding),
		byChecksum:   make(map[string][]string),
		bySourceRef:  make(map[string]string),
	}

	if err := repo.init(); err != nil {
		return nil, fmt.Errorf("initialize subtitle repository: %w", err)
	}

	return repo, nil
}

func (r *jsonRepository) init() error {
	for _, dir := range []string{r.rootDir, r.resourcesDir, r.bindingsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create repository directory %q: %w", dir, err)
		}
	}

	if err := r.rebuildIndexes(); err != nil {
		return fmt.Errorf("rebuild repository indexes: %w", err)
	}

	return nil
}

func (r *jsonRepository) Save(_ context.Context, resource model.SubtitleResource) (model.SubtitleResource, error) {
	if err := validateSubtitleResource(resource); err != nil {
		return model.SubtitleResource{}, fmt.Errorf("validate subtitle resource before save: %w", err)
	}

	payload, err := json.MarshalIndent(resource, "", "  ")
	if err != nil {
		return model.SubtitleResource{}, fmt.Errorf("marshal subtitle resource %q: %w", resource.ID, err)
	}

	targetPath := filepath.Join(r.resourcesDir, resource.ID+".json")
	if err := writeFileAtomic(targetPath, payload, 0o644); err != nil {
		return model.SubtitleResource{}, fmt.Errorf("persist subtitle resource %q: %w", resource.ID, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexResource(resource)

	return resource, nil
}

func (r *jsonRepository) GetByID(_ context.Context, subtitleID string) (model.SubtitleResource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	resource, ok := r.bySubtitleID[subtitleID]
	if !ok {
		return model.SubtitleResource{}, fmt.Errorf("get subtitle by id %q: %w", subtitleID, ErrSubtitleNotFound)
	}

	return resource, nil
}

func (r *jsonRepository) ListByItemID(_ context.Context, itemID string) ([]model.SubtitleResource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := append([]string(nil), r.byItemID[itemID]...)
	sort.Strings(ids)

	resources := make([]model.SubtitleResource, 0, len(ids))
	for _, subtitleID := range ids {
		resource, ok := r.bySubtitleID[subtitleID]
		if !ok {
			continue
		}
		resources = append(resources, resource)
	}

	return resources, nil
}

func (r *jsonRepository) GetBySourceRef(_ context.Context, itemID, sourceRef string) (model.SubtitleResource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	subtitleID, ok := r.bySourceRef[indexKey(itemID, sourceRef)]
	if !ok {
		return model.SubtitleResource{}, fmt.Errorf("get subtitle by source ref %q for item %q: %w", sourceRef, itemID, ErrSubtitleNotFound)
	}

	resource, ok := r.bySubtitleID[subtitleID]
	if !ok {
		return model.SubtitleResource{}, fmt.Errorf("get subtitle by source ref %q for item %q from resource index: %w", sourceRef, itemID, ErrSubtitleNotFound)
	}

	return resource, nil
}

func (r *jsonRepository) GetByChecksum(_ context.Context, itemID, checksum string) ([]model.SubtitleResource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := append([]string(nil), r.byChecksum[indexKey(itemID, checksum)]...)
	sort.Strings(ids)

	resources := make([]model.SubtitleResource, 0, len(ids))
	for _, subtitleID := range ids {
		resource, ok := r.bySubtitleID[subtitleID]
		if !ok {
			continue
		}
		resources = append(resources, resource)
	}

	return resources, nil
}

func (r *jsonRepository) SetDefault(_ context.Context, binding model.SubtitleBinding) (model.SubtitleBinding, error) {
	if binding.UpdatedAt.IsZero() {
		binding.UpdatedAt = time.Now().UTC()
	}
	if binding.ItemID == "" {
		return model.SubtitleBinding{}, fmt.Errorf("validate binding before set default: item id is empty")
	}
	if binding.DefaultSubtitleID == "" {
		return model.SubtitleBinding{}, fmt.Errorf("validate binding before set default: subtitle id is empty")
	}

	r.mu.RLock()
	resource, ok := r.bySubtitleID[binding.DefaultSubtitleID]
	r.mu.RUnlock()
	if !ok {
		return model.SubtitleBinding{}, fmt.Errorf("validate default binding subtitle %q: %w", binding.DefaultSubtitleID, ErrSubtitleNotFound)
	}
	if resource.ItemID != binding.ItemID {
		return model.SubtitleBinding{}, fmt.Errorf("validate default binding ownership for subtitle %q: subtitle belongs to item %q", binding.DefaultSubtitleID, resource.ItemID)
	}

	payload, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return model.SubtitleBinding{}, fmt.Errorf("marshal subtitle binding for item %q: %w", binding.ItemID, err)
	}

	targetPath := filepath.Join(r.bindingsDir, binding.ItemID+".json")
	if err := writeFileAtomic(targetPath, payload, 0o644); err != nil {
		return model.SubtitleBinding{}, fmt.Errorf("persist subtitle binding for item %q: %w", binding.ItemID, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[binding.ItemID] = binding

	return binding, nil
}

func (r *jsonRepository) GetDefault(_ context.Context, itemID string) (model.SubtitleBinding, model.SubtitleResource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	binding, ok := r.bindings[itemID]
	if !ok {
		return model.SubtitleBinding{}, model.SubtitleResource{}, fmt.Errorf("get default binding for item %q: %w", itemID, ErrBindingNotFound)
	}

	resource, ok := r.bySubtitleID[binding.DefaultSubtitleID]
	if !ok {
		return model.SubtitleBinding{}, model.SubtitleResource{}, fmt.Errorf("get default resource %q for item %q: %w", binding.DefaultSubtitleID, itemID, ErrSubtitleNotFound)
	}

	return binding, resource, nil
}

func (r *jsonRepository) DeleteByItemID(_ context.Context, itemID string) error {
	resources, err := r.ListByItemID(context.Background(), itemID)
	if err != nil {
		return fmt.Errorf("list subtitles for item %q before delete: %w", itemID, err)
	}

	for _, resource := range resources {
		resourcePath := filepath.Join(r.resourcesDir, resource.ID+".json")
		if err := os.Remove(resourcePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove subtitle resource file %q for item %q: %w", resourcePath, itemID, err)
		}
	}

	bindingPath := filepath.Join(r.bindingsDir, itemID+".json")
	if err := os.Remove(bindingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove subtitle binding file %q for item %q: %w", bindingPath, itemID, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.bindings, itemID)
	delete(r.byItemID, itemID)
	for _, resource := range resources {
		delete(r.bySubtitleID, resource.ID)
		delete(r.bySourceRef, indexKey(itemID, resource.SourceRef))
		delete(r.byChecksum, indexKey(itemID, resource.ChecksumSHA256))
	}

	return nil
}

func (r *jsonRepository) rebuildIndexes() error {
	newBySubtitleID := make(map[string]model.SubtitleResource)
	newByItemID := make(map[string][]string)
	newBindings := make(map[string]model.SubtitleBinding)
	newByChecksum := make(map[string][]string)
	newBySourceRef := make(map[string]string)

	resourceEntries, err := os.ReadDir(r.resourcesDir)
	if err != nil {
		return fmt.Errorf("read subtitle resource directory: %w", err)
	}

	for _, entry := range resourceEntries {
		if entry.IsDir() {
			continue
		}

		resourcePath := filepath.Join(r.resourcesDir, entry.Name())
		resourceBytes, err := os.ReadFile(resourcePath)
		if err != nil {
			continue
		}

		var resource model.SubtitleResource
		if err := json.Unmarshal(resourceBytes, &resource); err != nil {
			continue
		}
		if err := validateSubtitleResource(resource); err != nil {
			continue
		}

		newBySubtitleID[resource.ID] = resource
		newByItemID[resource.ItemID] = append(newByItemID[resource.ItemID], resource.ID)
		if resource.ChecksumSHA256 != "" {
			key := indexKey(resource.ItemID, resource.ChecksumSHA256)
			newByChecksum[key] = append(newByChecksum[key], resource.ID)
		}
		if resource.SourceRef != "" {
			newBySourceRef[indexKey(resource.ItemID, resource.SourceRef)] = resource.ID
		}
	}

	bindingEntries, err := os.ReadDir(r.bindingsDir)
	if err != nil {
		return fmt.Errorf("read subtitle binding directory: %w", err)
	}

	for _, entry := range bindingEntries {
		if entry.IsDir() {
			continue
		}

		bindingPath := filepath.Join(r.bindingsDir, entry.Name())
		bindingBytes, err := os.ReadFile(bindingPath)
		if err != nil {
			continue
		}

		var binding model.SubtitleBinding
		if err := json.Unmarshal(bindingBytes, &binding); err != nil {
			continue
		}
		resource, ok := newBySubtitleID[binding.DefaultSubtitleID]
		if !ok || resource.ItemID != binding.ItemID {
			continue
		}
		newBindings[binding.ItemID] = binding
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.bySubtitleID = newBySubtitleID
	r.byItemID = newByItemID
	r.bindings = newBindings
	r.byChecksum = newByChecksum
	r.bySourceRef = newBySourceRef

	return nil
}

func (r *jsonRepository) indexResource(resource model.SubtitleResource) {
	r.bySubtitleID[resource.ID] = resource
	if !containsString(r.byItemID[resource.ItemID], resource.ID) {
		r.byItemID[resource.ItemID] = append(r.byItemID[resource.ItemID], resource.ID)
	}
	if resource.ChecksumSHA256 != "" && !containsString(r.byChecksum[indexKey(resource.ItemID, resource.ChecksumSHA256)], resource.ID) {
		key := indexKey(resource.ItemID, resource.ChecksumSHA256)
		r.byChecksum[key] = append(r.byChecksum[key], resource.ID)
	}
	if resource.SourceRef != "" {
		r.bySourceRef[indexKey(resource.ItemID, resource.SourceRef)] = resource.ID
	}
}

func validateSubtitleResource(resource model.SubtitleResource) error {
	if resource.ID == "" {
		return fmt.Errorf("subtitle id is empty")
	}
	if resource.ItemID == "" {
		return fmt.Errorf("item id is empty")
	}
	if resource.StorageKey == "" {
		return fmt.Errorf("storage key is empty")
	}
	if resource.Format == "" {
		return fmt.Errorf("format is empty")
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func indexKey(itemID, value string) string {
	return itemID + "\x00" + value
}