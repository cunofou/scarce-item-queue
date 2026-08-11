package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Woolfer0097/GoodQueue/internal/adaptivequeue"
	"github.com/gin-gonic/gin"
)

type adaptiveQueueStatusStub struct {
	snapshot adaptivequeue.Snapshot
}

func (stub adaptiveQueueStatusStub) Snapshot() adaptivequeue.Snapshot { return stub.snapshot }

func TestAdaptiveQueueStatus(t *testing.T) {
	evaluatedAt := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	target := 67
	handler := NewAdaptiveQueueHandler(adaptiveQueueStatusStub{snapshot: adaptivequeue.Snapshot{
		Enabled: true, BasePercent: 100, CurrentPercent: 75, TargetPercent: &target,
		MinimumPercent: 0, MaximumPercent: 500, MaximumStepPercent: 25,
		Window: 30 * time.Minute, MinimumHTTPRequests: 100, MinimumCheckoutOutcomes: 20,
		MinimumHTTPSuccessPercent: 95, HTTPRequests: 1000, HTTPSuccessPercent: 99.5,
		CheckoutOutcomes: 100, PurchaseSuccessPercent: 60, Reason: adaptivequeue.ReasonApplied,
		EvaluatedAt: &evaluatedAt, AppliedAt: &evaluatedAt,
	}})
	router := gin.New()
	router.GET("/internal/v1/adaptive-queue/status", handler.Status)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/internal/v1/adaptive-queue/status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	want := `"current_percent":75`
	if body := recorder.Body.String(); !strings.Contains(body, want) || !strings.Contains(body, `"reason":"applied"`) || !strings.Contains(body, `"window_seconds":1800`) {
		t.Fatalf("unexpected response: %s", body)
	}
}
