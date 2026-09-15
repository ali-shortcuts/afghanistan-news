package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/afnews/backend/internal/model"
)

// ActivationWave implements the staged feed activation strategy (§268, §310).
//
//	Wave 1 — CORE:      direct / validated-direct / official feeds (the 99-feed set)
//	Wave 2 — DISCOVERY: + aggregator-topic feeds
//	Wave 3 — SELECTED:  + high-value aggregator search feeds (priority >= 3)
//	Wave 4 — FULL:      + the remaining long tail of discovery feeds
//
// The catalog is broad by design; production polling is selective by design (§311).
type ActivationWave int

const (
	WaveCore      ActivationWave = 1
	WaveDiscovery ActivationWave = 2
	WaveSelected  ActivationWave = 3
	WaveFull      ActivationWave = 4
)

// WaveDescription documents a wave for the admin console.
type WaveDescription struct {
	Wave    int    `json:"wave"`
	Label   string `json:"label"`
	Detail  string `json:"detail"`
	Enabled int    `json:"enabledFeeds"`
	Total   int    `json:"totalFeeds"`
}

// ApplyActivationWave enables the feeds belonging to a wave and disables the rest.
// Runtime history is untouched: only the enabled flag and next_poll_at change.
func (s *Store) ApplyActivationWave(ctx context.Context, wave ActivationWave) (enabled, disabled int, err error) {
	if wave < WaveCore {
		wave = WaveCore
	}
	if wave > WaveFull {
		wave = WaveFull
	}
	now := s.timeVal(time.Now().UTC())

	coreTypes := []string{
		string(model.SourceValidatedDirect), string(model.SourceDirectPublisher),
		string(model.SourceOfficialRealtime), string(model.SourceOfficialInstitution),
		string(model.SourceOpportunityFeed), string(model.SourceTenderFeed),
	}
	toEnable := []string{}

	switch wave {
	case WaveCore:
		toEnable = coreTypes
	case WaveDiscovery:
		toEnable = append(coreTypes, string(model.SourceAggregatorTopic))
	case WaveSelected:
		toEnable = append(coreTypes, string(model.SourceAggregatorTopic))
	case WaveFull:
		toEnable = []string{
			string(model.SourceValidatedDirect), string(model.SourceDirectPublisher),
			string(model.SourceOfficialRealtime), string(model.SourceOfficialInstitution),
			string(model.SourceOpportunityFeed), string(model.SourceTenderFeed),
			string(model.SourceAggregatorTopic), string(model.SourceAggregatorSearch),
		}
	}

	enableQuery := `UPDATE feeds SET enabled = $1, next_poll_at = $2, updated_at = $3 WHERE source_type IN (` +
		inClause(4, len(toEnable)) + `) AND (enabled <> $1 OR next_poll_at IS NULL)`
	args := []any{true, now, now}
	for _, t := range toEnable {
		args = append(args, t)
	}
	if wave == WaveSelected {
		// Only high-value discovery searches are activated in wave 3.
		enableQuery = `UPDATE feeds SET enabled = $1, next_poll_at = $2, updated_at = $3
			WHERE (source_type IN (` + inClause(4, len(toEnable)) + `) OR (source_type = $` + itoa(4+len(toEnable)) + ` AND priority >= $` + itoa(5+len(toEnable)) + `))
			AND (enabled <> $1 OR next_poll_at IS NULL)`
		args = append(args, string(model.SourceAggregatorSearch), 3)
	}
	res, err := s.exec(ctx, enableQuery, args...)
	if err != nil {
		return 0, 0, fmt.Errorf("activate feeds: %w", err)
	}
	if n, ok := res.RowsAffected(); ok == nil {
		enabled = int(n)
	}

	disableArgs := []any{false, s.timeVal(time.Now().UTC().Add(365 * 24 * time.Hour)), now}
	placeholders := make([]string, 0, len(toEnable)+1)
	for i, t := range toEnable {
		placeholders = append(placeholders, "$"+itoa(i+4))
		disableArgs = append(disableArgs, t)
	}
	enableIdx := "$" + itoa(len(disableArgs)+1)
	disableQuery := `UPDATE feeds SET enabled = $1, next_poll_at = $2, updated_at = $3
		WHERE source_type NOT IN (` + strings.Join(placeholders, ",") + `) AND enabled = ` + enableIdx
	disableArgs = append(disableArgs, true)
	switch wave {
	case WaveCore:
	case WaveFull:
		disableQuery = `UPDATE feeds SET enabled = $1, next_poll_at = $2, updated_at = $3
			WHERE source_type = $4 AND enabled = $5`
		disableArgs = []any{false, s.timeVal(time.Now().UTC().Add(365 * 24 * time.Hour)), now,
			string(model.SourceUnknown), true}
	case WaveSelected:
		disableQuery += ` AND (priority < $` + itoa(len(disableArgs)+1) + ` OR source_type <> $` + itoa(len(disableArgs)+2) + `)`
		disableArgs = append(disableArgs, 3, string(model.SourceAggregatorSearch))
	}
	res2, err := s.exec(ctx, disableQuery, disableArgs...)
	if err != nil {
		return enabled, 0, fmt.Errorf("deactivate feeds: %w", err)
	}
	if n, ok := res2.RowsAffected(); ok == nil {
		disabled = int(n)
	}
	return enabled, disabled, nil
}

// ActivationWaves reports how many feeds each wave would enable (§310).
func (s *Store) ActivationWaves(ctx context.Context) ([]WaveDescription, error) {
	total, err := s.CountRow(ctx, `SELECT COUNT(*) FROM feeds`)
	if err != nil {
		return nil, err
	}
	coreTypes := []string{
		string(model.SourceValidatedDirect), string(model.SourceDirectPublisher),
		string(model.SourceOfficialRealtime), string(model.SourceOfficialInstitution),
		string(model.SourceOpportunityFeed), string(model.SourceTenderFeed),
	}
	args := make([]any, 0, len(coreTypes))
	for _, t := range coreTypes {
		args = append(args, t)
	}
	core, err := s.CountRow(ctx, `SELECT COUNT(*) FROM feeds WHERE source_type IN (`+inClause(1, len(coreTypes))+`)`, args...)
	if err != nil {
		return nil, err
	}
	topic, err := s.CountRow(ctx, `SELECT COUNT(*) FROM feeds WHERE source_type = $1`, string(model.SourceAggregatorTopic))
	if err != nil {
		return nil, err
	}
	selected, err := s.CountRow(ctx,
		`SELECT COUNT(*) FROM feeds WHERE source_type = $1 AND priority >= $2`, string(model.SourceAggregatorSearch), 3)
	if err != nil {
		return nil, err
	}
	return []WaveDescription{
		{Wave: 1, Label: "CORE — direct / validated / official", Detail: "Wave 1 activation candidate set", Enabled: core, Total: total},
		{Wave: 2, Label: "DISCOVERY TOPICS", Detail: "Wave 1 + aggregator topic feeds", Enabled: core + topic, Total: total},
		{Wave: 3, Label: "SELECTED SEARCH", Detail: "Wave 2 + high-value Afghanistan/province/economy searches", Enabled: core + topic + selected, Total: total},
		{Wave: 4, Label: "LONG TAIL", Detail: "Full catalog (adaptive polling required)", Enabled: total, Total: total},
	}, nil
}
