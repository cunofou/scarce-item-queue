package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	baseURLEnv  = "GOODQUEUE_E2E_BASE_URL"
	databaseEnv = "GOODQUEUE_E2E_DATABASE_URL"

	productOne   = "11111111-1111-1111-1111-111111111111"
	productTwo   = "22222222-2222-2222-2222-222222222222"
	productThree = "33333333-3333-3333-3333-333333333333"

	userOne   = "00000000-0000-4000-8000-000000000001"
	userTwo   = "00000000-0000-4000-8000-000000000002"
	userThree = "00000000-0000-4000-8000-000000000003"
)

type suite struct {
	baseURL string
	client  *http.Client
	db      *sql.DB
}

type queueEntry struct {
	AttemptID     string     `json:"attempt_id"`
	ProductID     string     `json:"product_id"`
	State         string     `json:"state"`
	QueueSequence int64      `json:"queue_sequence"`
	Position      *int64     `json:"position"`
	PositionAhead *int64     `json:"position_ahead"`
	DeadlineAt    *time.Time `json:"deadline_at"`
	NextAction    string     `json:"next_action"`
	MessageCode   string     `json:"message_code"`
}

type product struct {
	ID               string `json:"id"`
	AllocatableStock int32  `json:"allocatable_stock"`
	Reserved         int32  `json:"reserved"`
}

type errorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func TestPurchaseJourneyIsIdempotentAndDoesNotOversell(t *testing.T) {
	testSuite := newSuite(t)
	testSuite.reset(t, 1, 3, 0)

	buyer := testSuite.join(t, productOne, userOne, "e2e-purchase-buyer", http.StatusCreated)
	assertQueueState(t, buyer, "checkout", "complete_payment")
	if buyer.DeadlineAt == nil {
		t.Fatal("buyer checkout does not expose a deadline")
	}

	replay := testSuite.join(t, productOne, userOne, "e2e-purchase-buyer", http.StatusOK)
	if replay.AttemptID != buyer.AttemptID || replay.QueueSequence != buyer.QueueSequence {
		t.Fatalf("idempotent join changed attempt: first=%+v replay=%+v", buyer, replay)
	}

	waiter := testSuite.join(t, productOne, userTwo, "e2e-purchase-waiter", http.StatusCreated)
	assertQueueState(t, waiter, "waiting", "wait")
	assertPosition(t, waiter, 1, 0)

	demoPaymentPath := "/api/v1/products/" + productOne + "/queue-attempts/" + buyer.AttemptID + "/demo-payment"
	testSuite.request(t, http.MethodPost, demoPaymentPath, userTwo, "e2e-demo-payment", nil, http.StatusNotFound)
	firstPayment := testSuite.request(t, http.MethodPost, demoPaymentPath, userOne, "e2e-demo-payment", nil, http.StatusOK)
	replayedPayment := testSuite.request(t, http.MethodPost, demoPaymentPath, userOne, "e2e-demo-payment", nil, http.StatusOK)
	if !bytes.Equal(firstPayment, replayedPayment) {
		t.Fatalf("payment replay changed response: first=%s replay=%s", firstPayment, replayedPayment)
	}

	bought := testSuite.current(t, productOne, userOne, http.StatusOK)
	assertQueueState(t, bought, "purchased", "none")
	soldOut := testSuite.current(t, productOne, userTwo, http.StatusOK)
	assertQueueState(t, soldOut, "sold_out", "none")

	var alternatives []product
	testSuite.requestJSON(t, http.MethodGet, "/api/v1/products/"+productOne+"/alternatives", "", "", nil, http.StatusOK, &alternatives)
	if len(alternatives) == 0 || alternatives[0].ID == productOne || alternatives[0].AllocatableStock <= 0 {
		t.Fatalf("sold-out journey did not return a purchasable alternative: %+v", alternatives)
	}

	var stock, reserved, purchases, inboxEvents int
	err := testSuite.db.QueryRow(`
		SELECT p.allocatable_stock, p.reserved,
			(SELECT count(*) FROM queue_attempts WHERE product_id=p.id AND state='purchased'),
			(SELECT count(*) FROM payment_inbox WHERE provider='goodqueue-demo' AND attempt_id=$2)
		FROM products p WHERE p.id=$1`, productOne, buyer.AttemptID).Scan(&stock, &reserved, &purchases, &inboxEvents)
	if err != nil {
		t.Fatalf("read purchase invariants: %v", err)
	}
	if stock != 0 || reserved != 0 || purchases != 1 || inboxEvents != 1 {
		t.Fatalf("purchase invariants changed: stock=%d reserved=%d purchases=%d inbox=%d", stock, reserved, purchases, inboxEvents)
	}
}

func TestPurchasedUserCanBuySameProductAgain(t *testing.T) {
	testSuite := newSuite(t)
	testSuite.reset(t, 2, 3, 0)

	first := testSuite.join(t, productOne, userOne, "e2e-repeat-first", http.StatusCreated)
	assertQueueState(t, first, "checkout", "complete_payment")
	testSuite.request(
		t, http.MethodPost,
		"/api/v1/products/"+productOne+"/queue-attempts/"+first.AttemptID+"/demo-payment",
		userOne, "e2e-repeat-first-payment", nil, http.StatusOK,
	)

	second := testSuite.join(t, productOne, userOne, "e2e-repeat-second", http.StatusCreated)
	assertQueueState(t, second, "checkout", "complete_payment")
	if second.AttemptID == first.AttemptID || second.QueueSequence <= first.QueueSequence {
		t.Fatalf("repeat purchase did not create a new ordered attempt: first=%+v second=%+v", first, second)
	}

	activeReplay := testSuite.join(t, productOne, userOne, "e2e-repeat-concurrent-key", http.StatusOK)
	if activeReplay.AttemptID != second.AttemptID {
		t.Fatalf("second active attempt was created: second=%+v replay=%+v", second, activeReplay)
	}

	testSuite.request(
		t, http.MethodPost,
		"/api/v1/products/"+productOne+"/queue-attempts/"+second.AttemptID+"/demo-payment",
		userOne, "e2e-repeat-second-payment", nil, http.StatusOK,
	)

	var stock, reserved, purchased, active int
	if err := testSuite.db.QueryRow(`
		SELECT p.allocatable_stock, p.reserved,
			(SELECT count(*) FROM queue_attempts WHERE product_id=p.id AND external_user_id=$2 AND state='purchased'),
			(SELECT count(*) FROM queue_attempts WHERE product_id=p.id AND external_user_id=$2 AND state IN ('waiting','invited','checkout'))
		FROM products p WHERE p.id=$1`, productOne, userOne).Scan(&stock, &reserved, &purchased, &active); err != nil {
		t.Fatalf("read repeat purchase invariants: %v", err)
	}
	if stock != 0 || reserved != 0 || purchased != 2 || active != 0 {
		t.Fatalf("repeat purchase invariants: stock=%d reserved=%d purchased=%d active=%d", stock, reserved, purchased, active)
	}
}

func TestCancellationAndFailedPaymentPromoteFIFO(t *testing.T) {
	testSuite := newSuite(t)
	testSuite.reset(t, 1, 1, 0)

	first := testSuite.join(t, productTwo, userOne, "e2e-fifo-first", http.StatusCreated)
	assertQueueState(t, first, "checkout", "complete_payment")
	second := testSuite.join(t, productTwo, userTwo, "e2e-fifo-second", http.StatusCreated)
	assertPosition(t, second, 1, 0)

	testSuite.request(t, http.MethodPost, "/api/v1/queue-attempts/"+first.AttemptID+"/checkout", userThree, "", nil, http.StatusNotFound)
	testSuite.request(t, http.MethodDelete, "/api/v1/products/"+productTwo+"/queue-entry", userOne, "", nil, http.StatusNoContent)

	invitedSecond := testSuite.current(t, productTwo, userTwo, http.StatusOK)
	assertQueueState(t, invitedSecond, "invited", "start_checkout")
	if invitedSecond.DeadlineAt == nil {
		t.Fatal("promoted FIFO head does not expose invitation deadline")
	}
	stillWaiting := testSuite.join(t, productTwo, userThree, "e2e-fifo-third", http.StatusCreated)
	assertQueueState(t, stillWaiting, "waiting", "wait")
	assertPosition(t, stillWaiting, 1, 0)

	checkout := testSuite.startCheckout(t, invitedSecond.AttemptID, userTwo, http.StatusOK)
	assertQueueState(t, checkout, "checkout", "complete_payment")
	testSuite.request(t, http.MethodPost, "/internal/v1/payment-events", "", "", map[string]any{
		"provider": "e2e", "event_id": "e2e-failed-event", "attempt_id": checkout.AttemptID,
		"outcome": "failed", "payment_reference": "",
	}, http.StatusOK)

	failed := testSuite.current(t, productTwo, userTwo, http.StatusOK)
	assertQueueState(t, failed, "payment_failed", "join_queue")
	invitedThird := testSuite.current(t, productTwo, userThree, http.StatusOK)
	assertQueueState(t, invitedThird, "invited", "start_checkout")

	var stock, reserved int
	var invitedUser string
	err := testSuite.db.QueryRow(`
		SELECT p.allocatable_stock, p.reserved,
			(SELECT external_user_id FROM queue_attempts WHERE product_id=p.id AND state='invited')
		FROM products p WHERE p.id=$1`, productTwo).Scan(&stock, &reserved, &invitedUser)
	if err != nil {
		t.Fatalf("read FIFO invariants: %v", err)
	}
	if stock != 1 || reserved != 1 || invitedUser != userThree {
		t.Fatalf("FIFO promotion changed: stock=%d reserved=%d invited=%s", stock, reserved, invitedUser)
	}
}

func TestExpiredInvitationMovesRightToNextUser(t *testing.T) {
	testSuite := newSuite(t)
	testSuite.reset(t, 1, 1, 0)

	holder := testSuite.join(t, productTwo, userOne, "e2e-expiry-holder", http.StatusCreated)
	winner := testSuite.join(t, productTwo, userTwo, "e2e-expiry-winner", http.StatusCreated)
	testSuite.request(t, http.MethodDelete, "/api/v1/products/"+productTwo+"/queue-entry", userOne, "", nil, http.StatusNoContent)
	invited := testSuite.current(t, productTwo, userTwo, http.StatusOK)
	assertQueueState(t, invited, "invited", "start_checkout")

	next := testSuite.join(t, productTwo, userThree, "e2e-expiry-next", http.StatusCreated)
	assertQueueState(t, next, "waiting", "wait")

	result, err := testSuite.db.Exec(`
		UPDATE queue_attempts SET invitation_deadline=clock_timestamp()
		WHERE id=$1 AND state='invited'`, invited.AttemptID)
	if err != nil {
		t.Fatalf("force invitation expiry: %v", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		t.Fatalf("force invitation expiry affected %d rows: %v", affected, err)
	}

	promoted := testSuite.current(t, productTwo, userThree, http.StatusOK)
	assertQueueState(t, promoted, "invited", "start_checkout")
	expired := testSuite.current(t, productTwo, userTwo, http.StatusOK)
	assertQueueState(t, expired, "invite_expired", "join_queue")

	if promoted.QueueSequence <= winner.QueueSequence || winner.QueueSequence <= holder.QueueSequence {
		t.Fatalf("queue sequence is not monotonic: holder=%d winner=%d promoted=%d",
			holder.QueueSequence, winner.QueueSequence, promoted.QueueSequence)
	}
}

func newSuite(t *testing.T) *suite {
	t.Helper()
	baseURL := strings.TrimRight(os.Getenv(baseURLEnv), "/")
	databaseURL := os.Getenv(databaseEnv)
	if baseURL == "" || databaseURL == "" {
		t.Skipf("set %s and %s to run backend E2E tests", baseURLEnv, databaseEnv)
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open E2E database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping E2E database: %v", err)
	}
	return &suite{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}, db: database}
}

func (testSuite *suite) reset(t *testing.T, firstStock, secondStock, thirdStock int) {
	t.Helper()
	testSuite.applyFixtures(t, firstStock, secondStock, thirdStock)
	t.Cleanup(func() { testSuite.applyFixtures(t, 1, 3, 0) })
}

func (testSuite *suite) applyFixtures(t *testing.T, firstStock, secondStock, thirdStock int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := testSuite.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin E2E fixture reset: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `TRUNCATE notification_outbox, payment_inbox, inventory_adjustments, queue_attempts`); err != nil {
		t.Fatalf("truncate E2E fixtures: %v", err)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE products SET reserved=0, next_queue_sequence=1, queue_enabled=true,
			allocatable_stock=CASE id
				WHEN $1::uuid THEN $4
				WHEN $2::uuid THEN $5
				WHEN $3::uuid THEN $6
				ELSE allocatable_stock
			END
		WHERE id IN ($1::uuid,$2::uuid,$3::uuid);`,
		productOne, productTwo, productThree, firstStock, secondStock, thirdStock)
	if err != nil {
		t.Fatalf("update E2E fixtures: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit E2E fixture reset: %v", err)
	}
}

func (testSuite *suite) join(t *testing.T, productID, userID, key string, status int) queueEntry {
	t.Helper()
	var entry queueEntry
	testSuite.requestJSON(t, http.MethodPost, "/api/v1/products/"+productID+"/queue-entries", userID, key, nil, status, &entry)
	return entry
}

func (testSuite *suite) current(t *testing.T, productID, userID string, status int) queueEntry {
	t.Helper()
	var entry queueEntry
	testSuite.requestJSON(t, http.MethodGet, "/api/v1/products/"+productID+"/queue-entry", userID, "", nil, status, &entry)
	return entry
}

func (testSuite *suite) startCheckout(t *testing.T, attemptID, userID string, status int) queueEntry {
	t.Helper()
	var entry queueEntry
	testSuite.requestJSON(t, http.MethodPost, "/api/v1/queue-attempts/"+attemptID+"/checkout", userID, "", nil, status, &entry)
	return entry
}

func (testSuite *suite) requestJSON(
	t *testing.T,
	method, path, userID, idempotencyKey string,
	body any,
	status int,
	target any,
) {
	t.Helper()
	responseBody := testSuite.request(t, method, path, userID, idempotencyKey, body, status)
	if target == nil || len(responseBody) == 0 {
		return
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		t.Fatalf("decode %s %s response %q: %v", method, path, responseBody, err)
	}
}

func (testSuite *suite) request(
	t *testing.T,
	method, path, userID, idempotencyKey string,
	body any,
	status int,
) []byte {
	t.Helper()
	var bodyReader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode %s %s request: %v", method, path, err)
		}
		bodyReader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, testSuite.baseURL+path, bodyReader)
	if err != nil {
		t.Fatalf("create %s %s request: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if userID != "" {
		request.Header.Set("X-User-ID", userID)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := testSuite.client.Do(request)
	if err != nil {
		t.Fatalf("perform %s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	if response.StatusCode != status {
		var publicError errorResponse
		_ = json.Unmarshal(responseBody, &publicError)
		t.Fatalf("%s %s status=%d want=%d error=%s body=%s",
			method, path, response.StatusCode, status, publicError.Error.Code, responseBody)
	}
	if response.Header.Get("X-Request-ID") == "" {
		t.Fatalf("%s %s response has no X-Request-ID", method, path)
	}
	return responseBody
}

func assertQueueState(t *testing.T, entry queueEntry, state, nextAction string) {
	t.Helper()
	if entry.State != state || entry.NextAction != nextAction {
		t.Fatalf("queue state=%q action=%q, want state=%q action=%q: %+v",
			entry.State, entry.NextAction, state, nextAction, entry)
	}
}

func assertPosition(t *testing.T, entry queueEntry, position, ahead int64) {
	t.Helper()
	if entry.Position == nil || entry.PositionAhead == nil || *entry.Position != position || *entry.PositionAhead != ahead {
		t.Fatalf("position=%v ahead=%v, want position=%d ahead=%d: %+v",
			formatOptionalInt(entry.Position), formatOptionalInt(entry.PositionAhead), position, ahead, entry)
	}
}

func formatOptionalInt(value *int64) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *value)
}
