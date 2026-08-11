package adaptivequeue

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Woolfer0097/GoodQueue/internal/loadtest"
	"go.uber.org/zap"
)

const (
	ReasonDisabled                    = "disabled"
	ReasonNotEvaluated                = "not_evaluated"
	ReasonApplied                     = "applied"
	ReasonStable                      = "stable"
	ReasonMetricsUnavailable          = "metrics_unavailable"
	ReasonInsufficientHTTPSamples     = "insufficient_http_samples"
	ReasonHTTPReliabilityBelowMinimum = "http_reliability_below_minimum"
	ReasonInsufficientPurchaseSamples = "insufficient_purchase_samples"
)

type MetricsReader interface {
	RequestSuccessStats(context.Context, time.Duration) (loadtest.RequestSuccessStats, error)
	PurchaseSuccessStats(context.Context, time.Duration) (loadtest.PurchaseSuccessStats, error)
}

type Config struct {
	Enabled                   bool
	BasePercent               int
	MinimumPercent            int
	MaximumPercent            int
	MaximumStepPercent        int
	Interval                  time.Duration
	Window                    time.Duration
	MinimumHTTPRequests       int
	MinimumCheckoutOutcomes   int
	MinimumHTTPSuccessPercent float64
}

type Snapshot struct {
	Enabled                   bool
	BasePercent               int
	CurrentPercent            int
	TargetPercent             *int
	MinimumPercent            int
	MaximumPercent            int
	MaximumStepPercent        int
	Window                    time.Duration
	MinimumHTTPRequests       int
	MinimumCheckoutOutcomes   int
	MinimumHTTPSuccessPercent float64
	HTTPRequests              float64
	HTTPSuccessPercent        float64
	CheckoutOutcomes          float64
	PurchaseSuccessPercent    float64
	Reason                    string
	EvaluatedAt               *time.Time
	AppliedAt                 *time.Time
}

type PercentSource struct {
	value atomic.Int64
}

func NewPercentSource(initial int) *PercentSource {
	source := &PercentSource{}
	source.value.Store(int64(initial))
	return source
}

func (source *PercentSource) CurrentWaitingBufferPercent() int {
	return int(source.value.Load())
}

func (source *PercentSource) store(percent int) {
	source.value.Store(int64(percent))
}

type Controller struct {
	config  Config
	metrics MetricsReader
	source  *PercentSource
	log     *zap.Logger
	now     func() time.Time

	mu        sync.RWMutex
	refreshMu sync.Mutex
	snapshot  Snapshot
}

func NewController(config Config, metrics MetricsReader, source *PercentSource, log *zap.Logger) (*Controller, error) {
	if source == nil {
		return nil, fmt.Errorf("adaptive queue percent source is required")
	}
	if log == nil {
		log = zap.NewNop()
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.Enabled && metrics == nil {
		return nil, fmt.Errorf("adaptive queue metrics reader is required when enabled")
	}
	reason := ReasonNotEvaluated
	if !config.Enabled {
		reason = ReasonDisabled
	}
	snapshot := Snapshot{
		Enabled: config.Enabled, BasePercent: config.BasePercent,
		CurrentPercent: source.CurrentWaitingBufferPercent(),
		MinimumPercent: config.MinimumPercent, MaximumPercent: config.MaximumPercent,
		MaximumStepPercent: config.MaximumStepPercent, Window: config.Window,
		MinimumHTTPRequests:       config.MinimumHTTPRequests,
		MinimumCheckoutOutcomes:   config.MinimumCheckoutOutcomes,
		MinimumHTTPSuccessPercent: config.MinimumHTTPSuccessPercent,
		Reason:                    reason,
	}
	return &Controller{config: config, metrics: metrics, source: source, log: log, now: time.Now, snapshot: snapshot}, nil
}

func validateConfig(config Config) error {
	if config.BasePercent < 0 || config.MinimumPercent < 0 || config.MaximumPercent < 0 ||
		config.MinimumPercent > config.BasePercent || config.BasePercent > config.MaximumPercent {
		return fmt.Errorf("adaptive queue percentages must satisfy 0 <= minimum <= base <= maximum")
	}
	if config.MaximumStepPercent <= 0 || config.MaximumStepPercent > config.MaximumPercent {
		return fmt.Errorf("adaptive queue maximum step must be between 1 and maximum percent")
	}
	if config.Interval <= 0 || config.Window <= 0 {
		return fmt.Errorf("adaptive queue interval and window must be positive")
	}
	if config.MinimumHTTPRequests <= 0 || config.MinimumCheckoutOutcomes <= 0 {
		return fmt.Errorf("adaptive queue sample thresholds must be positive")
	}
	if config.MinimumHTTPSuccessPercent < 0 || config.MinimumHTTPSuccessPercent > 100 {
		return fmt.Errorf("adaptive queue HTTP success threshold must be between 0 and 100")
	}
	return nil
}

func (controller *Controller) Run(ctx context.Context) {
	if !controller.config.Enabled {
		<-ctx.Done()
		return
	}
	controller.Refresh(ctx)
	ticker := time.NewTicker(controller.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			controller.Refresh(ctx)
		}
	}
}

func (controller *Controller) Refresh(ctx context.Context) {
	if !controller.config.Enabled {
		return
	}
	controller.refreshMu.Lock()
	defer controller.refreshMu.Unlock()
	evaluatedAt := controller.now().UTC()
	httpStats, err := controller.metrics.RequestSuccessStats(ctx, controller.config.Window)
	if err != nil {
		controller.recordSkipped(evaluatedAt, ReasonMetricsUnavailable, 0, 0, 0, 0, err)
		return
	}
	httpSuccessPercent := percentage(httpStats.Successful, httpStats.Total)
	if httpStats.Total < float64(controller.config.MinimumHTTPRequests) {
		controller.recordSkipped(evaluatedAt, ReasonInsufficientHTTPSamples,
			httpStats.Total, httpSuccessPercent, 0, 0, nil)
		return
	}
	if httpSuccessPercent < controller.config.MinimumHTTPSuccessPercent {
		controller.recordSkipped(evaluatedAt, ReasonHTTPReliabilityBelowMinimum,
			httpStats.Total, httpSuccessPercent, 0, 0, nil)
		return
	}
	purchaseStats, err := controller.metrics.PurchaseSuccessStats(ctx, controller.config.Window)
	if err != nil {
		controller.recordSkipped(evaluatedAt, ReasonMetricsUnavailable,
			httpStats.Total, httpSuccessPercent, 0, 0, err)
		return
	}
	checkoutOutcomes := purchaseStats.Purchased + purchaseStats.Cancelled + purchaseStats.CheckoutExpired
	purchaseSuccessPercent := percentage(purchaseStats.Purchased, checkoutOutcomes)
	if checkoutOutcomes < float64(controller.config.MinimumCheckoutOutcomes) {
		controller.recordSkipped(evaluatedAt, ReasonInsufficientPurchaseSamples,
			httpStats.Total, httpSuccessPercent, checkoutOutcomes, purchaseSuccessPercent, nil)
		return
	}

	target := controller.targetPercent(purchaseStats.Purchased, checkoutOutcomes)
	current := controller.source.CurrentWaitingBufferPercent()
	next := boundedStep(current, target, controller.config.MaximumStepPercent)
	reason := ReasonStable
	var appliedAt *time.Time
	if next != current {
		controller.source.store(next)
		reason = ReasonApplied
		appliedAt = &evaluatedAt
		controller.log.Info("adaptive queue buffer updated",
			zap.Int("previous_percent", current), zap.Int("current_percent", next), zap.Int("target_percent", target),
			zap.Float64("http_success_percent", httpSuccessPercent),
			zap.Float64("purchase_success_percent", purchaseSuccessPercent),
			zap.Float64("http_requests", httpStats.Total), zap.Float64("checkout_outcomes", checkoutOutcomes))
	}
	controller.mu.Lock()
	if appliedAt == nil {
		appliedAt = controller.snapshot.AppliedAt
	}
	controller.snapshot.CurrentPercent = next
	controller.snapshot.TargetPercent = intPointer(target)
	controller.snapshot.HTTPRequests = httpStats.Total
	controller.snapshot.HTTPSuccessPercent = httpSuccessPercent
	controller.snapshot.CheckoutOutcomes = checkoutOutcomes
	controller.snapshot.PurchaseSuccessPercent = purchaseSuccessPercent
	controller.snapshot.Reason = reason
	controller.snapshot.EvaluatedAt = &evaluatedAt
	controller.snapshot.AppliedAt = appliedAt
	controller.mu.Unlock()
}

func (controller *Controller) Snapshot() Snapshot {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	snapshot := controller.snapshot
	if snapshot.TargetPercent != nil {
		target := *snapshot.TargetPercent
		snapshot.TargetPercent = &target
	}
	return snapshot
}

func (controller *Controller) recordSkipped(
	evaluatedAt time.Time,
	reason string,
	httpRequests float64,
	httpSuccessPercent float64,
	checkoutOutcomes float64,
	purchaseSuccessPercent float64,
	err error,
) {
	controller.mu.Lock()
	controller.snapshot.CurrentPercent = controller.source.CurrentWaitingBufferPercent()
	controller.snapshot.TargetPercent = nil
	controller.snapshot.HTTPRequests = httpRequests
	controller.snapshot.HTTPSuccessPercent = httpSuccessPercent
	controller.snapshot.CheckoutOutcomes = checkoutOutcomes
	controller.snapshot.PurchaseSuccessPercent = purchaseSuccessPercent
	controller.snapshot.Reason = reason
	controller.snapshot.EvaluatedAt = &evaluatedAt
	controller.mu.Unlock()
	if err != nil {
		controller.log.Warn("adaptive queue evaluation skipped", zap.String("reason", reason), zap.Error(err))
	}
}

func (controller *Controller) targetPercent(purchased, total float64) int {
	target := controller.config.MaximumPercent
	if purchased > 0 {
		// Expected failed invitations per successful purchase: (1-p) / p.
		target = int(math.Ceil(100 * (total - purchased) / purchased))
	}
	return clamp(target, controller.config.MinimumPercent, controller.config.MaximumPercent)
}

func percentage(successful, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round(successful/total*10000) / 100
}

func boundedStep(current, target, maximumStep int) int {
	if target > current+maximumStep {
		return current + maximumStep
	}
	if target < current-maximumStep {
		return current - maximumStep
	}
	return target
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func intPointer(value int) *int { return &value }
