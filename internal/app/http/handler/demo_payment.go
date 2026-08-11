package handler

import (
	"context"
	"net/http"

	httpmiddleware "github.com/Woolfer0097/GoodQueue/internal/app/http/middleware"
	"github.com/Woolfer0097/GoodQueue/internal/pkg/domain"
	"github.com/gin-gonic/gin"
)

type DemoPaymentService interface {
	CompleteDemo(
		context.Context,
		domain.ProductID,
		domain.AttemptID,
		domain.ExternalUserID,
		domain.IdempotencyKey,
	) (domain.DemoPaymentResult, error)
}

type DemoPaymentHandler struct{ payments DemoPaymentService }

func NewDemoPaymentHandler(payments DemoPaymentService) *DemoPaymentHandler {
	return &DemoPaymentHandler{payments: payments}
}

// Complete godoc
//
//	@Summary		Complete a checkout with the safe demo payment flow
//	@Description	Simulates a successful payment for the authenticated demo user. The operation is idempotent and only accepts that user's active checkout.
//	@Tags			checkout
//	@Produce		json
//	@Param			X-User-ID				header		string	true	"Canonical lowercase external user UUID"	format(uuid)
//	@Param			Idempotency-Key			header		string	true	"Payment attempt idempotency key"			minlength(1)	maxlength(128)
//	@Param			productID				path		string	true	"Product UUID"								format(uuid)
//	@Param			attemptID				path		string	true	"Queue attempt UUID"						format(uuid)
//	@Success		200						{object}	QueueEntryResponse
//	@Success		202						{object}	QueueEntryResponse
//	@Failure		400,401,404,409,410,500	{object}	middleware.ErrorResponse
//	@Router			/api/v1/products/{productID}/queue-attempts/{attemptID}/demo-payment [post]
func (handler *DemoPaymentHandler) Complete(c *gin.Context) {
	productID, userID, ok := parseProductAndIdentity(c)
	if !ok {
		return
	}
	attemptID, err := domain.ParseAttemptID(c.Param("attemptID"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	key, err := domain.ParseIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		_ = c.Error(err)
		return
	}

	result, err := handler.payments.CompleteDemo(c.Request.Context(), productID, attemptID, userID, key)
	if err != nil {
		_ = c.Error(err)
		return
	}
	status := http.StatusOK
	if result.Processing {
		status = http.StatusAccepted
	}
	c.JSON(status, mapQueueAttempt(result.Attempt, 0))
}

var _ = httpmiddleware.ErrorResponse{}
