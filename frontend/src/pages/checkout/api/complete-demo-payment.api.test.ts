import { jest } from '@jest/globals';
import { ZodError } from 'zod';

const apiClientMock = jest.fn<(path: string, init?: RequestInit) => Promise<unknown>>();

jest.unstable_mockModule('@/shared/api', () => ({
  ApiError: class ApiErrorMock extends Error {},
  apiClient: apiClientMock,
}));

const { completeDemoPayment } = await import('./complete-demo-payment.api');

const productId = '11111111-1111-4111-8111-111111111111';
const attemptId = '22222222-2222-4222-8222-222222222222';
const userId = '00000000-0000-4000-8000-000000000002';
const idempotencyKey = '33333333-3333-4333-8333-333333333333';
const purchasedAttempt = {
  attempt_id: attemptId,
  created_at: '2026-08-09T08:00:00Z',
  message_code: 'purchased',
  next_action: 'none',
  product_id: productId,
  purchased_at: '2026-08-09T08:01:00Z',
  queue_sequence: 1,
  state: 'purchased',
  terminal_at: '2026-08-09T08:01:00Z',
  updated_at: '2026-08-09T08:01:00Z',
};

describe('complete demo payment API', () => {
  beforeEach(() => {
    apiClientMock.mockReset();
  });

  it('posts the user and idempotency key and validates the purchased attempt', async () => {
    apiClientMock.mockResolvedValue(purchasedAttempt);

    await expect(
      completeDemoPayment(productId, attemptId, userId, idempotencyKey),
    ).resolves.toEqual(purchasedAttempt);
    expect(apiClientMock).toHaveBeenCalledWith(
      `/api/v1/products/${productId}/queue-attempts/${attemptId}/demo-payment`,
      {
        headers: {
          'Idempotency-Key': idempotencyKey,
          'X-User-ID': userId,
        },
        method: 'POST',
      },
    );
  });

  it('rejects an invalid backend response', async () => {
    apiClientMock.mockResolvedValue({ ...purchasedAttempt, state: 'paid' });

    await expect(
      completeDemoPayment(productId, attemptId, userId, idempotencyKey),
    ).rejects.toBeInstanceOf(ZodError);
  });
});
