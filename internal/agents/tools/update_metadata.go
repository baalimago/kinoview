package tools

import (
	"errors"
	"fmt"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/media/constants"
)

type updateMetadataTool struct {
	metadataMgr agents.MetadataManager
	itemsGetter agents.ItemGetter
}

const metadataPrompt = `Metadata which should be in the following format: %s`

func NewUpdateMetadataTool(mm agents.MetadataManager, ig agents.ItemGetter) (*updateMetadataTool, error) {
	if mm == nil {
		return nil, errors.New("metadata manager can't be nil")
	}

	if ig == nil {
		return nil, errors.New("items getter can't be nil")
	}
	return &updateMetadataTool{
		metadataMgr: mm,
		itemsGetter: ig,
	}, nil
}

func (umt *updateMetadataTool) Call(input models.Input) (string, error) {
	ID, ok := input["ID"].(string)
	if !ok {
		return "", fmt.Errorf("ID must be a string")
	}

	metadata, ok := input["metadata"].(string)
	if !ok {
		return "", fmt.Errorf("metadata must be a string")
	}
	item, err := umt.itemsGetter.GetItemByID(ID)
	if err != nil {
		return "", fmt.Errorf("update metadata tool failed to get item: %v", err)
	}
	err = umt.metadataMgr.UpdateMetadata(item, metadata)
	if err != nil {
		return "", fmt.Errorf("failed to update metadata for item: '%v', error: %w", item.Name, err)
	}
	return fmt.Sprintf("successfully updated metadata for item: '%v'", item.Name), nil
}

func (umt *updateMetadataTool) Specification() models.Specification {
	return models.Specification{
		Name:        "update_metadata",
		Description: "Use this tool to update the metadata of some item by supplying new metadata.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "ID of the item",
				},
				"metadata": {
					Type:        "string",
					Description: fmt.Sprintf(metadataPrompt, constants.MetadataFormat),
				},
			},
			Required: []string{"ID", "metadata"},
		},
	}
}
