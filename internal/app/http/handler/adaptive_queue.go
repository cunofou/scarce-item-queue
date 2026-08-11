package handler

import (
	"net/http"
	"time"

	"github.com/Woolfer0097/GoodQueue/internal/adaptivequeue"
	"github.com/gin-gonic/gin"
)

type AdaptiveQueueStatusReader interface {
	Snapshot() adaptivequeue.Snapshot
}

type AdaptiveQueueStatusResponse struct {
	Enabled                   bool       `json:"enabled"`
	BasePercent               int        `json:"base_percent"`
	CurrentPercent            int        `json:"current_percent"`
	TargetPercent             *int       `json:"target_percent"`
	MinimumPercent            int        `json:"minimum_percent"`
	MaximumPercent            int        `json:"maximum_percent"`
	MaximumStepPercent        int        `json:"maximum_step_percent"`
	Window                    string     `json:"window"`
	WindowSeconds             int64      `json:"window_seconds"`
	MinimumHTTPRequests       int        `json:"minimum_http_requests"`
	MinimumCheckoutOutcomes   int        `json:"minimum_checkout_outcomes"`
	MinimumHTTPSuccessPercent float64    `json:"minimum_http_success_percent"`
	HTTPRequests              float64    `json:"http_requests"`
	HTTPSuccessPercent        float64    `json:"http_success_percent"`
	CheckoutOutcomes          float64    `json:"checkout_outcomes"`
	PurchaseSuccessPercent    float64    `json:"purchase_success_percent"`
	Reason                    string     `json:"reason"`
	EvaluatedAt               *time.Time `json:"evaluated_at"`
	AppliedAt                 *time.Time `json:"applied_at"`
}

type AdaptiveQueueHandler struct {
	status AdaptiveQueueStatusReader
}

func NewAdaptiveQueueHandler(status AdaptiveQueueStatusReader) *AdaptiveQueueHandler {
	return &AdaptiveQueueHandler{status: status}
}

// Status godoc
//
//	@Summary	Adaptive queue controller status
//	@Tags		internal
//	@Produce	json
//	@Success	200	{object}	AdaptiveQueueStatusResponse
//	@Router		/internal/v1/adaptive-queue/status [get]
func (handler *AdaptiveQueueHandler) Status(c *gin.Context) {
	snapshot := handler.status.Snapshot()
	c.JSON(http.StatusOK, AdaptiveQueueStatusResponse{
		Enabled: snapshot.Enabled, BasePercent: snapshot.BasePercent,
		CurrentPercent: snapshot.CurrentPercent, TargetPercent: snapshot.TargetPercent,
		MinimumPercent: snapshot.MinimumPercent, MaximumPercent: snapshot.MaximumPercent,
		MaximumStepPercent: snapshot.MaximumStepPercent,
		Window:             snapshot.Window.String(), WindowSeconds: int64(snapshot.Window.Seconds()),
		MinimumHTTPRequests:       snapshot.MinimumHTTPRequests,
		MinimumCheckoutOutcomes:   snapshot.MinimumCheckoutOutcomes,
		MinimumHTTPSuccessPercent: snapshot.MinimumHTTPSuccessPercent,
		HTTPRequests:              snapshot.HTTPRequests, HTTPSuccessPercent: snapshot.HTTPSuccessPercent,
		CheckoutOutcomes:       snapshot.CheckoutOutcomes,
		PurchaseSuccessPercent: snapshot.PurchaseSuccessPercent,
		Reason:                 snapshot.Reason, EvaluatedAt: snapshot.EvaluatedAt, AppliedAt: snapshot.AppliedAt,
	})
}
