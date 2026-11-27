package store

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
)

func BenchmarkSearch1000Stages(b *testing.B) {
	ctx, _, db := openIntegrationDatabase(b)
	defer db.Close()

	now := time.Now().UTC()
	phases := []string{"dev", "test", "preprod", "prod"}
	var stages []promotion.Stage
	for project := range 25 {
		for application := range 10 {
			name := fmt.Sprintf("svc-%02d-%02d", project, application)
			for i, phase := range phases {
				stage := promotionTestStage("kargo", "west", fmt.Sprintf("project-%02d", project), name+"-"+phase, now)
				stage.Application = name
				request := promotion.FreightRequest{Origin: "Warehouse/" + name, Direct: i == 0}
				if i > 0 {
					request.SourceStages = []string{name + "-" + phases[i-1]}
				}
				stage.RequestedFreight = []promotion.FreightRequest{request}
				stages = append(stages, stage)
			}
		}
	}
	snapshot := promotionTestSnapshot(uuid.New(), "kargo", "west", now)
	if err := db.ReplacePromotionStageSnapshot(ctx, snapshot, stages); err != nil {
		b.Fatal(err)
	}

	config := promotion.IntegrationSet{"kargo": {ClusterName: "west"}}
	queries := [][]string{{"west"}, {"prod", "project-04"}, {"svc-15-05"}, {"nomatch"}}
	search := func(ctx context.Context, i int) error {
		terms := queries[i%len(queries)]
		if _, err := db.SearchTargets(ctx, terms, 5); err != nil {
			return err
		}
		results, err := db.SearchPromotionInventory(ctx, config, terms, 5)
		if err == nil && i%len(queries) == 0 && (len(results.Stages) != 5 || len(results.Pipelines) != 5) {
			return fmt.Errorf("broad search returned %d Stages and %d Pipelines", len(results.Stages), len(results.Pipelines))
		}
		return err
	}
	for i := range 16 {
		if err := search(ctx, i); err != nil {
			b.Fatal(err)
		}
	}

	for _, workers := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			latencies := make([]time.Duration, b.N)
			var group sync.WaitGroup
			b.ResetTimer()
			for worker := range min(workers, b.N) {
				group.Go(func() {
					for i := worker; i < b.N; i += workers {
						start := time.Now()
						if err := search(b.Context(), i); err != nil {
							b.Error(err)
							return
						}
						latencies[i] = time.Since(start)
					}
				})
			}
			group.Wait()
			b.StopTimer()
			slices.Sort(latencies)
			b.ReportMetric(float64(latencies[(b.N-1)/2])/float64(time.Millisecond), "ms/p50")
			b.ReportMetric(float64(latencies[(95*b.N-1)/100])/float64(time.Millisecond), "ms/p95")
		})
	}
}
