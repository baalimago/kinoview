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
	PrepSuggestions(context.Context, model.ClientContext, []model.Item) ([]model.Recommendation, error)
}
