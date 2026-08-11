import { Button } from '@mantine/core';
import { notifications } from '@mantine/notifications';

import { useCompleteDemoPayment } from '../model/use-complete-demo-payment';

interface CompleteDemoPaymentButtonProps {
  attemptId: string;
  productId: string;
  userId: string | null;
}

export function CompleteDemoPaymentButton({
  attemptId,
  productId,
  userId,
}: CompleteDemoPaymentButtonProps) {
  const paymentMutation = useCompleteDemoPayment({ attemptId, productId, userId });

  const handlePayment = () => {
    if (paymentMutation.isPending) {
      return;
    }

    paymentMutation.mutate(undefined, {
      onError: () => {
        notifications.show({
          color: 'red',
          message: 'Проверьте срок резерва и попробуйте ещё раз.',
          title: 'Не удалось завершить демо-оплату',
        });
      },
    });
  };

  return (
    <Button
      disabled={userId === null || paymentMutation.isPending}
      loading={paymentMutation.isPending}
      onClick={handlePayment}
      size="md"
      w="fit-content"
    >
      Оплатить и завершить покупку
    </Button>
  );
}
