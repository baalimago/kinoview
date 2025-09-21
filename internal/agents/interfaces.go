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

type Recommender interface {
	Setup(context.Context) error

	// Recommend some item from a slice of items based on some request
	// Expected to be slow and blocking, as recommendation is most likely done
	// by some agentic LLM
	Recommend(context.Context, string, []model.Item) (model.Item, error)
}
