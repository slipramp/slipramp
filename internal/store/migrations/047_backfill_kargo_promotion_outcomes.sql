-- +goose Up
-- Promotion outcomes did not exist before migration 046. Existing approved
-- Promotions were scheduled for observation by that migration, but their
-- Kargo resources may no longer exist. The two known installations accept a
-- one-time succeeded backfill for that historical data. Restrict the repair to
-- rows whose product state predates migration 046 so an actually running
-- Promotion approved after the rollout is never rewritten.
WITH outcome_rollout AS (
    SELECT tstamp AS applied_at
    FROM public.slipramp_schema_migrations
    WHERE version_id = 46
      AND is_applied
    ORDER BY id DESC
    LIMIT 1
)
UPDATE promotions
SET kargo_promotion_uid = COALESCE(
        kargo_promotion_uid,
        'historical-' || promotions.id::text
    ),
    next_reconciliation_at = NULL,
    kargo_outcome_phase = 'Succeeded',
    kargo_outcome_observed_at = outcome_rollout.applied_at,
    kargo_outcome_diagnostic =
        'Historical Promotion marked succeeded during the outcome-tracking rollout; Kargo''s terminal phase was not recorded.'
FROM outcome_rollout
WHERE promotions.status = 'approved'
  AND promotions.kargo_outcome_phase IS NULL
  AND promotions.updated_at < outcome_rollout.applied_at;

-- +goose Down
UPDATE promotions
SET next_reconciliation_at = kargo_outcome_observed_at,
    kargo_outcome_phase = NULL,
    kargo_outcome_observed_at = NULL,
    kargo_outcome_diagnostic = NULL,
    kargo_promotion_uid = CASE
        WHEN kargo_promotion_uid = 'historical-' || id::text THEN NULL
        ELSE kargo_promotion_uid
    END
WHERE kargo_outcome_phase = 'Succeeded'
  AND kargo_outcome_diagnostic =
      'Historical Promotion marked succeeded during the outcome-tracking rollout; Kargo''s terminal phase was not recorded.';
