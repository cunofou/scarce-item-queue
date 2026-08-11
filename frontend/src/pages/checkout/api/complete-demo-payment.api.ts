import { queueAttemptSchema } from '@/entities/queue-attempt';
import { apiClient } from '@/shared/api';

export const completeDemoPayment = async (
  productId: string,
  attemptId: string,
  userId: string,
  idempotencyKey: string,
) => {
  const response = await apiClient(
    `/api/v1/products/${encodeURIComponent(productId)}/queue-attempts/${encodeURIComponent(attemptId)}/demo-payment`,
    {
      headers: {
        'Idempotency-Key': idempotencyKey,
        'X-User-ID': userId,
      },
      method: 'POST',
    },
  );

  return queueAttemptSchema.parse(response);
};
