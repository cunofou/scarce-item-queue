package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Woolfer0097/GoodQueue/internal/pkg/domain"
	"github.com/google/uuid"
)

const (
	demoTestProductID = "11111111-1111-4111-8111-111111111111"
	demoTestAttemptID = "22222222-2222-4222-8222-222222222222"
	demoTestUserID    = "00000000-0000-4000-8000-000000000001"
)

type paymentProcessorStub struct {
	commands []domain.PaymentCommand
	result   domain.PaymentResult
	err      error
}

func (stub *paymentProcessorStub) ProcessPayment(
	_ context.Context,
	command domain.PaymentCommand,
) (domain.PaymentResult, error) {
	stub.commands = append(stub.commands, command)
	return stub.result, stub.err
}

type currentAttemptFinderStub struct {
	results []domain.CurrentQueueResult
	err     error
	calls   int
}

func (stub *currentAttemptFinderStub) FindCurrent(
	context.Context,
	domain.ProductID,
	domain.ExternalUserID,
) (domain.CurrentQueueResult, error) {
	stub.calls++
	if stub.err != nil {
		return domain.CurrentQueueResult{}, stub.err
	}
	index := stub.calls - 1
	if index >= len(stub.results) {
		index = len(stub.results) - 1
	}
	return stub.results[index], nil
}

func TestCompleteDemoAcceptsOwnedCheckoutAndBuildsDeterministicPayment(t *testing.T) {
	processor := &paymentProcessorStub{result: domain.PaymentResult{HTTPStatus: 200, Code: "accepted"}}
	finder := &currentAttemptFinderStub{results: []domain.CurrentQueueResult{
		{Attempt: demoAttempt(domain.QueueAttemptCheckout)},
		{Attempt: demoAttempt(domain.QueueAttemptPurchased)},
	}}
	useCase := NewPaymentUseCase(processor, finder)

	result, err := useCase.CompleteDemo(
		context.Background(), demoProductID(), demoAttemptID(), domain.ExternalUserID(demoTestUserID), "pay-1",
	)
	if err != nil {
		t.Fatalf("complete demo payment: %v", err)
	}
	if result.Attempt.State != domain.QueueAttemptPurchased || result.Processing {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(processor.commands) != 1 {
		t.Fatalf("payment commands: got %d, want 1", len(processor.commands))
	}
	command := processor.commands[0]
	if command.Provider != demoPaymentProvider || command.Outcome != domain.PaymentSucceeded ||
		command.AttemptID != demoAttemptID() || command.EventID == "" || command.PaymentReference == "" {
		t.Fatalf("unexpected payment command: %+v", command)
	}

	secondProcessor := &paymentProcessorStub{result: domain.PaymentResult{HTTPStatus: 200, Code: "accepted"}}
	secondFinder := &currentAttemptFinderStub{results: finder.results}
	_, err = NewPaymentUseCase(secondProcessor, secondFinder).CompleteDemo(
		context.Background(), demoProductID(), demoAttemptID(), domain.ExternalUserID(demoTestUserID), "pay-1",
	)
	if err != nil {
		t.Fatalf("repeat deterministic payment: %v", err)
	}
	if secondProcessor.commands[0].EventID != command.EventID ||
		secondProcessor.commands[0].PaymentReference != command.PaymentReference {
		t.Fatalf("same idempotency key generated different payment identity")
	}
}

func TestCompleteDemoReplaysPurchasedAttemptWithoutAnotherPayment(t *testing.T) {
	processor := &paymentProcessorStub{}
	finder := &currentAttemptFinderStub{results: []domain.CurrentQueueResult{{
		Attempt: demoAttempt(domain.QueueAttemptPurchased),
	}}}
	result, err := NewPaymentUseCase(processor, finder).CompleteDemo(
		context.Background(), demoProductID(), demoAttemptID(), domain.ExternalUserID(demoTestUserID), "pay-replay",
	)
	if err != nil || result.Attempt.State != domain.QueueAttemptPurchased {
		t.Fatalf("purchased replay: result=%+v err=%v", result, err)
	}
	if len(processor.commands) != 0 {
		t.Fatalf("purchased replay submitted %d payment commands", len(processor.commands))
	}
}

func TestCompleteDemoRejectsForeignOrInactiveAttempt(t *testing.T) {
	otherAttempt := demoAttempt(domain.QueueAttemptCheckout)
	otherAttempt.ID = domain.AttemptID(uuid.MustParse("33333333-3333-4333-8333-333333333333"))

	tests := []struct {
		name    string
		attempt domain.QueueAttempt
		want    error
	}{
		{name: "different attempt", attempt: otherAttempt, want: domain.ErrAttemptNotFound},
		{name: "waiting", attempt: demoAttempt(domain.QueueAttemptWaiting), want: domain.ErrInvalidTransition},
		{name: "expired", attempt: demoAttempt(domain.QueueAttemptCheckoutExpired), want: domain.ErrAttemptGone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &paymentProcessorStub{}
			finder := &currentAttemptFinderStub{results: []domain.CurrentQueueResult{{Attempt: test.attempt}}}
			_, err := NewPaymentUseCase(processor, finder).CompleteDemo(
				context.Background(), demoProductID(), demoAttemptID(), domain.ExternalUserID(demoTestUserID), "pay-invalid",
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
			if len(processor.commands) != 0 {
				t.Fatalf("invalid attempt submitted %d payment commands", len(processor.commands))
			}
		})
	}
}

func TestCompleteDemoReportsConcurrentProcessingWithoutDuplicatingState(t *testing.T) {
	processor := &paymentProcessorStub{result: domain.PaymentResult{HTTPStatus: 202, Code: "processing"}}
	finder := &currentAttemptFinderStub{results: []domain.CurrentQueueResult{
		{Attempt: demoAttempt(domain.QueueAttemptCheckout)},
		{Attempt: demoAttempt(domain.QueueAttemptCheckout)},
	}}
	result, err := NewPaymentUseCase(processor, finder).CompleteDemo(
		context.Background(), demoProductID(), demoAttemptID(), domain.ExternalUserID(demoTestUserID), "pay-processing",
	)
	if err != nil || !result.Processing || result.Attempt.State != domain.QueueAttemptCheckout {
		t.Fatalf("processing result=%+v err=%v", result, err)
	}
}

func demoAttempt(state domain.QueueAttemptState) domain.QueueAttempt {
	now := time.Now().UTC()
	return domain.QueueAttempt{
		ID: demoAttemptID(), ProductID: demoProductID(), ExternalUserID: demoTestUserID,
		IdempotencyKey: "join-1", QueueSequence: 1, State: state, CreatedAt: now, UpdatedAt: now,
	}
}

func demoProductID() domain.ProductID {
	return domain.ProductID(uuid.MustParse(demoTestProductID))
}

func demoAttemptID() domain.AttemptID {
	return domain.AttemptID(uuid.MustParse(demoTestAttemptID))
}
