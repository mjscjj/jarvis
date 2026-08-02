package semantic

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

type legacyStatusClient interface {
	Count(context.Context, *qdrant.CountPoints) (uint64, error)
	SetPayload(context.Context, *qdrant.SetPayloadPoints) (*qdrant.UpdateResult, error)
}

var legacyTodoStatusMappings = []struct {
	from string
	to   string
}{
	{from: "auto", to: "materialized"},
	{from: "dropped", to: "observing"},
}

// migrateLegacyTodoStatuses keeps Qdrant's denormalized Todo status payload in
// lockstep with the durable Todo lifecycle. SQLite remains the source of
// truth; this only renames the two retired payload values without touching any
// vectors or other payload fields.
//
// Each update filters exclusively on the old value and waits until it is ready
// for search, so rerunning the migration is a no-op. Exact counts before and
// after each update make an incomplete Qdrant write fail startup instead of
// silently excluding legacy points from semantic deduplication.
func migrateLegacyTodoStatuses(ctx context.Context, client legacyStatusClient, collection string) error {
	if client == nil {
		return fmt.Errorf("migrate legacy semantic Todo statuses: client is nil")
	}
	if collection == "" {
		return fmt.Errorf("migrate legacy semantic Todo statuses: collection is empty")
	}
	exact := true
	wait := true
	for _, mapping := range legacyTodoStatusMappings {
		filter := &qdrant.Filter{Must: []*qdrant.Condition{
			qdrant.NewMatchKeyword("status", mapping.from),
		}}
		count, err := client.Count(ctx, &qdrant.CountPoints{
			CollectionName: collection,
			Filter:         filter,
			Exact:          &exact,
		})
		if err != nil {
			return fmt.Errorf("count legacy semantic Todo status %q in collection %q: %w", mapping.from, collection, err)
		}
		if count == 0 {
			continue
		}
		payload, err := qdrant.TryValueMap(map[string]any{"status": mapping.to})
		if err != nil {
			return fmt.Errorf("encode semantic Todo status migration %q to %q: %w", mapping.from, mapping.to, err)
		}
		result, err := client.SetPayload(ctx, &qdrant.SetPayloadPoints{
			CollectionName: collection,
			Wait:           &wait,
			Payload:        payload,
			PointsSelector: qdrant.NewPointsSelectorFilter(filter),
		})
		if err != nil {
			return fmt.Errorf("migrate %d semantic Todo statuses %q to %q in collection %q: %w", count, mapping.from, mapping.to, collection, err)
		}
		status := qdrant.UpdateStatus_UnknownUpdateStatus
		if result != nil {
			status = result.GetStatus()
		}
		if status != qdrant.UpdateStatus_Completed {
			return fmt.Errorf(
				"migrate %d semantic Todo statuses %q to %q in collection %q: update status=%s, want Completed",
				count, mapping.from, mapping.to, collection, status.String(),
			)
		}
		remaining, err := client.Count(ctx, &qdrant.CountPoints{
			CollectionName: collection,
			Filter:         filter,
			Exact:          &exact,
		})
		if err != nil {
			return fmt.Errorf("verify legacy semantic Todo status %q in collection %q: %w", mapping.from, collection, err)
		}
		if remaining != 0 {
			return fmt.Errorf(
				"migrate semantic Todo statuses %q to %q in collection %q: %d legacy points remain after updating %d",
				mapping.from, mapping.to, collection, remaining, count,
			)
		}
	}
	return nil
}
