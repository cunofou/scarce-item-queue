import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useRef } from 'react';

import { queueAttemptQueryKeys } from '@/entities/queue-attempt';

import { completeDemoPayment } from '../api/complete-demo-payment.api';
import { createDemoPaymentIdempotencyKey } from './create-demo-payment-idempotency-key';

interface UseCompleteDemoPaymentParams {
  attemptId: string;
  productId: string;
  userId: string | null;
}

export const useCompleteDemoPayment = ({
  attemptId,
  productId,
  userId,
}: UseCompleteDemoPaymentParams) => {
  const queryClient = useQueryClient();
  const idempotencyKeyRef = useRef<string | null>(null);

  return useMutation({
    mutationFn: () => {
      if (userId === null) {
        return Promise.reject(new Error('Demo user is not selected'));
      }

      idempotencyKeyRef.current ??= createDemoPaymentIdempotencyKey();
      return completeDemoPayment(productId, attemptId, userId, idempotencyKeyRef.current);
    },
    onSuccess: async (attempt) => {
      await queryClient.invalidateQueries({
        queryKey: queueAttemptQueryKeys.all,
        refetchType: 'none',
      });
      queryClient.setQueryData(queueAttemptQueryKeys.current(productId, userId), attempt);
      if (attempt.state === 'purchased') {
        idempotencyKeyRef.current = null;
      }
    },
  });
};
