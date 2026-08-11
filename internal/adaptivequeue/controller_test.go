package adaptivequeue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Woolfer0097/GoodQueue/internal/loadtest"
	"go.uber.org/zap"
)

type metricsStub struct {
	httpStats     loadtest.RequestSuccessStats
	purchaseStats loadtest.PurchaseSuccessStats
	httpErr       error
	purchaseErr   error
}

func (stub metricsStub) RequestSuccessStats(context.Context, time.Duration) (loadtest.RequestSuccessStats, error) {
	return stub.httpStats, stub.httpErr
}

func (stub metricsStub) PurchaseSuccessStats(context.Context, time.Duration) (loadtest.PurchaseSuccessStats, error) {
	return stub.purchaseStats, stub.purchaseErr
}

func TestControllerAppliesBoundedStepsFromPurchaseConversion(t *testing.T) {
	tests := []struct {
		name        string
		purchased   float64
		cancelled   float64
		expired     float64
		wantTarget  int
		wantCurrent int
	}{
		{name: "high conversion reduces buffer", purchased: 80, cancelled: 15, expired: 5, wantTarget: 25, wantCurrent: 75},
		{name: "low conversion grows buffer", purchased: 20, cancelled: 60, expired: 20, wantTarget: 400, wantCurrent: 125},
		{name: "zero purchases clamps to maximum", purchased: 0, cancelled: 80, expired: 20, wantTarget: 500, wantCurrent: 125},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := NewPercentSource(100)
			controller := mustController(t, metricsStub{
				httpStats: loadtest.RequestSuccessStats{Successful: 995, Total: 1000},
				purchaseStats: loadtest.PurchaseSuccessStats{
					Purchased: test.purchased, Cancelled: test.cancelled, CheckoutExpired: test.expired,
				},
			}, source)
			controller.Refresh(context.Background())
			snapshot := controller.Snapshot()
			if snapshot.Reason != ReasonApplied || snapshot.TargetPercent == nil || *snapshot.TargetPercent != test.wantTarget {
				t.Fatalf("unexpected decision: %+v", snapshot)
			}
			if snapshot.CurrentPercent != test.wantCurrent || source.CurrentWaitingBufferPercent() != test.wantCurrent {
				t.Fatalf("current percent=%d source=%d want=%d", snapshot.CurrentPercent, source.CurrentWaitingBufferPercent(), test.wantCurrent)
			}
		})
	}
}

func TestControllerSkipsUnsafeOrInsufficientMetrics(t *testing.T) {
	tests := []struct {
		name    string
		metrics metricsStub
		want    string
	}{
		{name: "metrics unavailable", metrics: metricsStub{httpErr: errors.New("prometheus down")}, want: ReasonMetricsUnavailable},
		{name: "few HTTP samples", metrics: metricsStub{httpStats: loadtest.RequestSuccessStats{Successful: 9, Total: 10}}, want: ReasonInsufficientHTTPSamples},
		{name: "degraded HTTP", metrics: metricsStub{httpStats: loadtest.RequestSuccessStats{Successful: 90, Total: 100}}, want: ReasonHTTPReliabilityBelowMinimum},
		{name: "purchase metrics unavailable", metrics: metricsStub{httpStats: loadtest.RequestSuccessStats{Successful: 100, Total: 100}, purchaseErr: errors.New("query failed")}, want: ReasonMetricsUnavailable},
		{name: "few purchase samples", metrics: metricsStub{httpStats: loadtest.RequestSuccessStats{Successful: 100, Total: 100}, purchaseStats: loadtest.PurchaseSuccessStats{Purchased: 5, Cancelled: 4, CheckoutExpired: 1}}, want: ReasonInsufficientPurchaseSamples},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := NewPercentSource(100)
			controller := mustController(t, test.metrics, source)
			controller.Refresh(context.Background())
			snapshot := controller.Snapshot()
			if snapshot.Reason != test.want || snapshot.CurrentPercent != 100 || snapshot.TargetPercent != nil {
				t.Fatalf("unexpected skipped decision: %+v", snapshot)
			}
		})
	}
}

func TestControllerReportsStableTargetAndRetainsLastAppliedAt(t *testing.T) {
	source := NewPercentSource(25)
	controller := mustController(t, metricsStub{
		httpStats:     loadtest.RequestSuccessStats{Successful: 100, Total: 100},
		purchaseStats: loadtest.PurchaseSuccessStats{Purchased: 80, Cancelled: 20},
	}, source)
	controller.Refresh(context.Background())
	snapshot := controller.Snapshot()
	if snapshot.Reason != ReasonStable || snapshot.TargetPercent == nil || *snapshot.TargetPercent != 25 || snapshot.AppliedAt != nil {
		t.Fatalf("unexpected stable decision: %+v", snapshot)
	}
}

func TestDisabledControllerKeepsConfiguredFallback(t *testing.T) {
	config := testConfig()
	config.Enabled = false
	source := NewPercentSource(config.BasePercent)
	controller, err := NewController(config, nil, source, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	controller.Refresh(context.Background())
	if snapshot := controller.Snapshot(); snapshot.Reason != ReasonDisabled || snapshot.CurrentPercent != config.BasePercent {
		t.Fatalf("unexpected disabled snapshot: %+v", snapshot)
	}
}

func TestControllerRejectsUnsafeConfiguration(t *testing.T) {
	config := testConfig()
	config.MinimumPercent = 200
	if _, err := NewController(config, metricsStub{}, NewPercentSource(100), zap.NewNop()); err == nil {
		t.Fatal("expected invalid bounds to fail")
	}
	config = testConfig()
	if _, err := NewController(config, nil, NewPercentSource(100), zap.NewNop()); err == nil {
		t.Fatal("expected enabled controller without metrics to fail")
	}
}

func mustController(t *testing.T, metrics MetricsReader, source *PercentSource) *Controller {
	t.Helper()
	controller, err := NewController(testConfig(), metrics, source, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return fixedNow }
	return controller
}

func testConfig() Config {
	return Config{
		Enabled: true, BasePercent: 100, MinimumPercent: 0, MaximumPercent: 500,
		MaximumStepPercent: 25, Interval: time.Minute, Window: 30 * time.Minute,
		MinimumHTTPRequests: 100, MinimumCheckoutOutcomes: 20, MinimumHTTPSuccessPercent: 95,
	}
}
