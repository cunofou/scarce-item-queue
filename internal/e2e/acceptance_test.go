package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
)

const concurrentContenders = 20

type concurrentJoinResult struct {
	status int
	entry  queueEntry
	code   string
	err    error
}

func TestACOneUnitGrantsExactlyOneConcurrentPurchaseRight(t *testing.T) {
	testSuite := newSuite(t)
	testSuite.reset(t, 1, 3, 0)

	start := make(chan struct{})
	results := make(chan concurrentJoinResult, concurrentContenders)
	var requests sync.WaitGroup
	requests.Add(concurrentContenders)
	for contender := 0; contender < concurrentContenders; contender++ {
		go func(index int) {
			defer requests.Done()
			<-start
			userID := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+100)
			results <- testSuite.concurrentJoin(t, productOne, userID, fmt.Sprintf("ac-concurrent-%d", index))
		}(contender)
	}
	close(start)
	requests.Wait()
	close(results)

	rights, waiting, rejected := 0, 0, 0
	sequences := make(map[int64]struct{})
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent join failed: %v", result.err)
		}
		switch result.status {
		case http.StatusCreated:
			if _, duplicate := sequences[result.entry.QueueSequence]; duplicate {
				t.Fatalf("duplicate queue sequence %d", result.entry.QueueSequence)
			}
			sequences[result.entry.QueueSequence] = struct{}{}
			switch result.entry.State {
			case "checkout", "invited":
				rights++
			case "waiting":
				waiting++
			default:
				t.Fatalf("unexpected successful concurrent state %q", result.entry.State)
			}
		case http.StatusConflict:
			if result.code != "queue_full" {
				t.Fatalf("concurrent rejection code=%q, want queue_full", result.code)
			}
			rejected++
		default:
			t.Fatalf("unexpected concurrent status %d", result.status)
		}
	}
	if rights != 1 || waiting != 1 || rejected != concurrentContenders-2 {
		t.Fatalf("concurrent allocation rights=%d waiting=%d rejected=%d", rights, waiting, rejected)
	}

	var stock, reserved, storedRights int
	err := testSuite.db.QueryRow(`
		SELECT allocatable_stock, reserved,
			(SELECT count(*) FROM queue_attempts WHERE product_id=products.id AND state IN ('invited','checkout'))
		FROM products WHERE id=$1`, productOne).Scan(&stock, &reserved, &storedRights)
	if err != nil {
		t.Fatalf("read concurrent acceptance invariants: %v", err)
	}
	if stock != 1 || reserved != 1 || storedRights != 1 {
		t.Fatalf("concurrent acceptance invariant stock=%d reserved=%d rights=%d", stock, reserved, storedRights)
	}
}

func TestACQueueCannotBeBypassedAndRightIsPersonal(t *testing.T) {
	testSuite := newSuite(t)
	testSuite.reset(t, 1, 3, 0)

	holder := testSuite.join(t, productOne, userOne, "ac-personal-holder", http.StatusCreated)
	assertQueueState(t, holder, "checkout", "complete_payment")
	waiter := testSuite.join(t, productOne, userTwo, "ac-personal-waiter", http.StatusCreated)
	assertQueueState(t, waiter, "waiting", "wait")

	testSuite.request(t, http.MethodPost, "/api/v1/queue-attempts/"+holder.AttemptID+"/checkout", userTwo, "", nil, http.StatusNotFound)
	testSuite.request(t, http.MethodPost, "/api/v1/queue-attempts/"+waiter.AttemptID+"/checkout", userTwo, "", nil, http.StatusConflict)

	var paymentResult struct {
		Code string `json:"code"`
	}
	testSuite.requestJSON(t, http.MethodPost, "/internal/v1/payment-events", "", "", map[string]any{
		"provider": "ac", "event_id": "ac-bypass-payment", "attempt_id": waiter.AttemptID,
		"outcome": "succeeded", "payment_reference": "ac-bypass-reference",
	}, http.StatusOK, &paymentResult)
	if paymentResult.Code != "compensation_required" {
		t.Fatalf("bypass payment code=%q, want compensation_required", paymentResult.Code)
	}
	assertQueueState(t, testSuite.current(t, productOne, userTwo, http.StatusOK), "waiting", "wait")
	var compensationEvents int
	if err := testSuite.db.QueryRow(`
		SELECT count(*) FROM notification_outbox
		WHERE event_type='payment.compensation_required' AND attempt_id=$1`, waiter.AttemptID).Scan(&compensationEvents); err != nil {
		t.Fatalf("read bypass compensation: %v", err)
	}
	if compensationEvents != 1 {
		t.Fatalf("bypass compensation events=%d, want 1", compensationEvents)
	}

	var stock, reserved int
	var holderState, waiterState string
	err := testSuite.db.QueryRow(`
		SELECT p.allocatable_stock, p.reserved,
			(SELECT state FROM queue_attempts WHERE id=$2),
			(SELECT state FROM queue_attempts WHERE id=$3)
		FROM products p WHERE p.id=$1`, productOne, holder.AttemptID, waiter.AttemptID).
		Scan(&stock, &reserved, &holderState, &waiterState)
	if err != nil {
		t.Fatalf("read personal-right acceptance invariants: %v", err)
	}
	if stock != 1 || reserved != 1 || holderState != "checkout" || waiterState != "waiting" {
		t.Fatalf("bypass mutated allocation: stock=%d reserved=%d holder=%s waiter=%s",
			stock, reserved, holderState, waiterState)
	}
}

func TestACUserJourneyAlwaysReturnsGuidanceAndAlternatives(t *testing.T) {
	testSuite := newSuite(t)
	testSuite.reset(t, 1, 1, 0)

	holder := testSuite.join(t, productTwo, userOne, "ac-guidance-holder", http.StatusCreated)
	assertGuidance(t, holder, "checkout", "complete_payment")
	waiter := testSuite.join(t, productTwo, userTwo, "ac-guidance-waiter", http.StatusCreated)
	assertGuidance(t, waiter, "waiting", "wait")

	testSuite.request(t, http.MethodDelete, "/api/v1/products/"+productTwo+"/queue-entry", userOne, "", nil, http.StatusNoContent)
	assertGuidance(t, testSuite.current(t, productTwo, userOne, http.StatusOK), "cancelled", "join_queue")
	invited := testSuite.current(t, productTwo, userTwo, http.StatusOK)
	assertGuidance(t, invited, "invited", "start_checkout")
	checkout := testSuite.startCheckout(t, invited.AttemptID, userTwo, http.StatusOK)
	assertGuidance(t, checkout, "checkout", "complete_payment")

	testSuite.request(t, http.MethodPost, "/internal/v1/payment-events", "", "", map[string]any{
		"provider": "ac", "event_id": "ac-guidance-failed", "attempt_id": checkout.AttemptID,
		"outcome": "failed", "payment_reference": "",
	}, http.StatusOK)
	assertGuidance(t, testSuite.current(t, productTwo, userTwo, http.StatusOK), "payment_failed", "join_queue")

	winner := testSuite.join(t, productTwo, userThree, "ac-guidance-winner", http.StatusCreated)
	assertGuidance(t, winner, "checkout", "complete_payment")
	finalWaiter := testSuite.join(t, productTwo, userOne, "ac-guidance-final-waiter", http.StatusCreated)
	assertGuidance(t, finalWaiter, "waiting", "wait")
	testSuite.request(t, http.MethodPost, "/internal/v1/payment-events", "", "", map[string]any{
		"provider": "ac", "event_id": "ac-guidance-success", "attempt_id": winner.AttemptID,
		"outcome": "succeeded", "payment_reference": "ac-guidance-reference",
	}, http.StatusOK)
	assertGuidance(t, testSuite.current(t, productTwo, userThree, http.StatusOK), "purchased", "none")
	assertGuidance(t, testSuite.current(t, productTwo, userOne, http.StatusOK), "sold_out", "none")

	var alternatives []product
	testSuite.requestJSON(t, http.MethodGet, "/api/v1/products/"+productTwo+"/alternatives", "", "", nil, http.StatusOK, &alternatives)
	if len(alternatives) == 0 {
		t.Fatal("sold-out acceptance journey returned no alternatives")
	}
	for _, alternative := range alternatives {
		if alternative.ID == productTwo || alternative.AllocatableStock <= alternative.Reserved {
			t.Fatalf("invalid purchasable alternative: %+v", alternative)
		}
	}
}

func (testSuite *suite) concurrentJoin(t *testing.T, productID, userID, key string) concurrentJoinResult {
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		testSuite.baseURL+"/api/v1/products/"+productID+"/queue-entries", http.NoBody)
	if err != nil {
		return concurrentJoinResult{err: fmt.Errorf("create concurrent request: %w", err)}
	}
	request.Header.Set("X-User-ID", userID)
	request.Header.Set("Idempotency-Key", key)
	response, err := testSuite.client.Do(request)
	if err != nil {
		return concurrentJoinResult{err: fmt.Errorf("perform concurrent request: %w", err)}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return concurrentJoinResult{err: fmt.Errorf("read concurrent response: %w", err)}
	}
	result := concurrentJoinResult{status: response.StatusCode}
	if response.StatusCode == http.StatusCreated {
		if err := json.Unmarshal(body, &result.entry); err != nil {
			result.err = fmt.Errorf("decode concurrent queue entry: %w", err)
		}
		return result
	}
	var publicError errorResponse
	if err := json.Unmarshal(body, &publicError); err != nil {
		result.err = fmt.Errorf("decode concurrent error response: %w", err)
		return result
	}
	result.code = publicError.Error.Code
	return result
}

func assertGuidance(t *testing.T, entry queueEntry, state, nextAction string) {
	t.Helper()
	assertQueueState(t, entry, state, nextAction)
	if entry.MessageCode == "" || entry.MessageCode == "unknown_state" {
		t.Fatalf("state %s has no stable message code: %+v", state, entry)
	}
	if (state == "waiting" && entry.Position == nil) ||
		((state == "invited" || state == "checkout") && entry.DeadlineAt == nil) {
		t.Fatalf("state %s lacks required client guidance: %+v", state, entry)
	}
}
