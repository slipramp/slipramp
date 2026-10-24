package store

import (
	"encoding/json"
	"fmt"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func encodePromotionSnapshots(record promotion.Promotion, params *storedb.CreatePromotionParams) error {
	stage := record.StageSnapshot
	stage.Freight = promotionFreightValue(stage.Freight)
	var err error
	params.StageSnapshot, err = json.Marshal(stage)
	if err != nil {
		return fmt.Errorf("encode Promotion Stage snapshot: %w", err)
	}
	params.RepositorySnapshot, err = json.Marshal(record.RepositorySnapshot)
	if err != nil {
		return fmt.Errorf("encode Promotion repository snapshot: %w", err)
	}
	params.PolicySnapshot, err = json.Marshal(record.PolicySnapshot)
	if err != nil {
		return fmt.Errorf("encode Promotion policy snapshot: %w", err)
	}
	if record.SelectedFreightSnapshot != nil {
		params.SelectedFreightSnapshot, err = json.Marshal(record.SelectedFreightSnapshot)
		if err != nil {
			return fmt.Errorf("encode selected Promotion Freight snapshot: %w", err)
		}
	}

	return nil
}

func decodePromotionSnapshots(row storedb.Promotion, record *promotion.Promotion) error {
	stage, err := decodePromotionStageSnapshot(row.StageSnapshot)
	if err != nil {
		return err
	}
	record.StageSnapshot = stage
	if err := json.Unmarshal(row.RepositorySnapshot, &record.RepositorySnapshot); err != nil {
		return fmt.Errorf("decode Promotion repository snapshot: %w", err)
	}
	if err := json.Unmarshal(row.PolicySnapshot, &record.PolicySnapshot); err != nil {
		return fmt.Errorf("decode Promotion policy snapshot: %w", err)
	}
	if len(row.SelectedFreightSnapshot) > 0 {
		if err := json.Unmarshal(row.SelectedFreightSnapshot, &record.SelectedFreightSnapshot); err != nil {
			return fmt.Errorf("decode selected Promotion Freight snapshot: %w", err)
		}
	}

	return nil
}

func decodePromotionStageSnapshot(encoded []byte) (promotion.PromotionStageSnapshot, error) {
	var snapshot promotion.PromotionStageSnapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return promotion.PromotionStageSnapshot{}, fmt.Errorf("decode Promotion Stage snapshot: %w", err)
	}
	snapshot.Freight = promotionFreightValue(snapshot.Freight)

	return snapshot, nil
}

func encodePromotionStageJSON(record promotion.Stage, params *storedb.UpsertPromotionStageParams) error {
	requests := record.RequestedFreight
	if requests == nil {
		requests = []promotion.FreightRequest{}
	}
	var err error
	params.RequestedFreightJson, err = json.Marshal(requests)
	if err != nil {
		return fmt.Errorf("encode requested Stage Freight: %w", err)
	}
	params.FreightJson, err = json.Marshal(promotionFreightValue(record.Freight))
	if err != nil {
		return fmt.Errorf("encode Stage Freight: %w", err)
	}
	if record.SuccessfulRenderAcknowledgement != nil {
		params.SuccessfulRenderAcknowledgement, err = json.Marshal(record.SuccessfulRenderAcknowledgement)
		if err != nil {
			return fmt.Errorf("encode Stage render acknowledgement: %w", err)
		}
	}

	return nil
}

func decodePromotionStageJSON(row storedb.PromotionStage, record *promotion.Stage) error {
	if err := json.Unmarshal(row.RequestedFreightJson, &record.RequestedFreight); err != nil {
		return fmt.Errorf("decode requested Stage Freight: %w", err)
	}
	if err := json.Unmarshal(row.FreightJson, &record.Freight); err != nil {
		return fmt.Errorf("decode Stage Freight: %w", err)
	}
	record.Freight = promotionFreightValue(record.Freight)
	if len(row.SuccessfulRenderAcknowledgement) > 0 {
		if err := json.Unmarshal(row.SuccessfulRenderAcknowledgement, &record.SuccessfulRenderAcknowledgement); err != nil {
			return fmt.Errorf("decode Stage render acknowledgement: %w", err)
		}
	}

	return nil
}

func promotionFreightValue(value promotion.Freight) promotion.Freight {
	if value.Items == nil {
		value.Items = []promotion.FreightItem{}
	}

	return value
}
