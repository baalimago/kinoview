package butler

import (
	"context"
	"fmt"

	"github.com/baalimago/kinoview/internal/model"
)

type PreloadSubsError struct {
	ItemName string
	Err      error
}

func (e *PreloadSubsError) Error() string {
	return fmt.Sprintf("failed to preload subs for %s: %v",
		e.ItemName, e.Err)
}

func (e *PreloadSubsError) Unwrap() error {
	return e.Err
}

func (b *butler) preloadSubs(ctx context.Context,
	item model.Item, rec *model.Suggestion,
) error {
	info, err := b.subs.Find(item)
	if err != nil {
		return &PreloadSubsError{
			ItemName: item.Name,
			Err:      fmt.Errorf("failed to find subs: %w", err),
		}
	}
	var selectedIdx string

	if b.selector != nil {
		idx, selEnglishErr := b.selector.Select(ctx, info.Streams)
		if selEnglishErr != nil {
			return &PreloadSubsError{
				ItemName: item.Name,
				Err: fmt.Errorf(
					"failed to select english: %w", selEnglishErr),
			}
		}
		selectedIdx = fmt.Sprintf("%d", idx)
	}

	_, err = b.subs.ExtractSubtitles(item, selectedIdx)
	if err != nil {
		return fmt.Errorf("failed to extract subs for %s: %v",
			item.Name, err)
	}
	rec.SubtitleID = selectedIdx
	return nil
}

func (b *butler) prepSuggestion(ctx context.Context,
	sug suggestionResponse, items []model.Item) (
	model.Suggestion, error,
) {
	item, err := b.semanticIndexerSelect(ctx, sug, items)
	if err != nil {
		return model.Suggestion{},
			fmt.Errorf("failed to semanticIndexer select: %w",
				err)
	}
	rec := model.Suggestion{
		Item:       item,
		Motivation: sug.Motivation,
	}
	if b.subs == nil {
		return rec, nil
	}
	err = b.preloadSubs(ctx, item, &rec)
	if err != nil {
		return rec, fmt.Errorf("failed to preloadSubs: %w", err)
	}
	return rec, nil
}
