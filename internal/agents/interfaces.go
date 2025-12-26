package agents

import (
	"context"

	"github.com/baalimago/kinoview/internal/model"
)

type Classifier interface {
	Setup(context.Context) error
	// Classify some item in a blocking manner. Expected to take up to 10-30 seconds
	// since implementation may be LLM based
	Classify(context.Context, model.Item) (model.Item, error)
}

// Recommender recommends some piece of media given some semantic
// request from the user, along with context. It's the predecessor of
// the Butler.
type Recommender interface {
	Setup(context.Context) error

	// Recommend some item from a slice of items based on some request
	// Expected to be slow and blocking, as recommendation is most likely done
	// by some agentic LLM
	Recommend(context.Context, string, []model.Item) (model.Item, error)
}

// Butler will attempt to figure out the needs of the user before
// the user does, and serve it on the viewers next return. Whenever
// the user has ended their session, the butler will start figuring out
// what recommendations to give on the next return based on viewing
// patterns and available content.
type Butler interface {
	// Setup the butler
	Setup(context.Context) error

	// PrepSuggestions by analyzing the client context and library
	PrepSuggestions(context.Context, model.ClientContext, []model.Item) ([]model.Suggestion, error)
}

// Concierge is a better butler. Butler was an advanced workflow with
// many tools at its disposal, but ultimately it had a singulra purpose.
// Concierge will act autonomously and be run at a fixed interval using a set
// of tools designed to act on the Kinoview media state
type Concierge interface {
	// Setup the Concierge, validate config etc. Return a chan err for runtime errors.
	Setup(context.Context) error

	// Run the Concierge, blocking operation. Returns the last message from the concierge
	// On Run error -> returns error
	// On context cancel -> Gracefully shuts down operations, closes chan error on defer
	Run(ctx context.Context) (string, error)
}

type ItemGetter interface {
	GetItemByID(ID string) (model.Item, error)
	GetItemByName(Name string) (model.Item, error)
}

// ItemLister provides a read-only snapshot of items in the media library.
// Useful for building search/browse tools without tying them to a specific store implementation.
type ItemLister interface {
	Snapshot() []model.Item
}

type MetadataManager interface {
	UpdateMetadata(model.Item, string) error
}

// SuggestionManager manages suggestions. Stores them for whoever wants some suggestions
type SuggestionManager interface {
	// List the currently stored suggestions
	List() ([]model.Suggestion, error)
	// Remove some suggestion from the store
	Remove(ID string) error
	// Add suggestion to the store
	Add(model.Suggestion) error
}

// StreamManager which handles subtitle extraction and analysis
type StreamManager interface {
	// Find subtitle information about some item
	Find(item model.Item) (model.MediaInfo, error)

	// ExtractSubtitles the subtitles, return string to path to the file where the subtitles are extracted. On subsequent
	// calls, the subtitles will be checked from fle. As such, it's possible to preload subs
	ExtractSubtitles(item model.Item, streamIndex string) (string, error)
}

type SubtitleSelector interface {
	// Select returns the index of the best english subtitle stream, or error if none found
	Select(ctx context.Context, streams []model.Stream) (int, error)
}

// UserContextManager manages the user context and allows
type UserContextManager interface {
	// AllClientContexts returns a snapshot of all client contexts for every session
	AllClientContexts() []model.ClientContext

	// StoreClientContext and persist on disk. Will error on failure to store.
	StoreClientContext(model.ClientContext) error
}
