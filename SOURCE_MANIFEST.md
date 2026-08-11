# Манифест авторского кода

Файлы ниже отобраны по Git-истории командного проекта. Для файлов из актуального `main` на момент отбора `git blame` показывает 100% строк за автором `mr.flexx163@mail.ru` — Git-идентичностью аккаунта `cunofou`.

## Адаптивная очередь

Источник: [`54b278d`](https://github.com/Woolfer0097/GoodQueue/commit/54b278ddffd01bc980226bf4244cf62cb39a3528).

- `internal/adaptivequeue/controller.go`
- `internal/adaptivequeue/controller_test.go`
- `internal/pkg/domain/waiting_buffer.go`
- `internal/app/http/handler/adaptive_queue.go`
- `internal/app/http/handler/adaptive_queue_test.go`

## AI-рекомендации и fallback

Основные источники: [`230f863`](https://github.com/Woolfer0097/GoodQueue/commit/230f863c15b8a060fd873b2d032cec92a8be879e), [`4494beb`](https://github.com/Woolfer0097/GoodQueue/commit/4494beba923f71b3761464e6465a3e2727d7c45a), [`368765d`](https://github.com/Woolfer0097/GoodQueue/commit/368765d85a9d24546bb523daf8f0dc665be3a982).

- `internal/recommendation/openai/embedder.go`
- `internal/recommendation/openai/embedder_test.go`
- `internal/pkg/domain/recommendation.go`
- `internal/pkg/repository/recommendation.go`
- `internal/usecase/embedding.go`
- `internal/repository/postgres/recommendation.go`
- `internal/repository/postgres/recommendation_integration_test.go`
- `migrations/00006_ai_recommendations.sql`
- `migrations/00007_seed_recommendation_catalog.sql`
- `migrations/00010_local_product_media.sql`
- `internal/usecase/product_test.go`

## Demo-покупка и повторный сценарий

Источники: [`2f3a34b`](https://github.com/Woolfer0097/GoodQueue/commit/2f3a34b9ba99000c3d314cb5698cb386dfcef49f), [`2bf5f1a`](https://github.com/Woolfer0097/GoodQueue/commit/2bf5f1acfe35c8251ab05ded710b8f18320c187b).

- `internal/app/http/handler/demo_payment.go`
- `internal/usecase/payment_test.go`
- `frontend/src/pages/checkout/api/complete-demo-payment.api.ts`
- `frontend/src/pages/checkout/api/complete-demo-payment.api.test.ts`
- `frontend/src/pages/checkout/model/create-demo-payment-idempotency-key.ts`
- `frontend/src/pages/checkout/model/use-complete-demo-payment.ts`
- `frontend/src/pages/checkout/ui/CompleteDemoPaymentButton.tsx` — версия из авторского коммита `2f3a34b` до последующих командных правок.
- `migrations/00011_allow_repeat_purchases.sql`

## E2E, acceptance и HTTP infrastructure

Основные источники: [`ee5935e`](https://github.com/Woolfer0097/GoodQueue/commit/ee5935e4c9f9f93839e24f71548f6a205fb174c3), [`a89e9f6`](https://github.com/Woolfer0097/GoodQueue/commit/a89e9f61f121d7158eec9d5da28d3692e3b84c31).

- `internal/e2e/acceptance_test.go`
- `internal/e2e/e2e_test.go`
- `internal/app/http/middleware/cors.go`
- `internal/app/http/middleware/cors_test.go`
- `scripts/queue_load.go`

## Grafana

Источник: [`af756bb`](https://github.com/Woolfer0097/GoodQueue/commit/af756bb0d7f5e06ee45ee15218c7790f91e85a75).

- `internal/loadtest/grafana_test.go` — первоначальная авторская версия.
- `loadtest/grafana/dashboards/goodqueue-loadtest.json` — первоначальная авторская версия.
- `loadtest/grafana/provisioning/dashboards/goodqueue.yaml`
- `loadtest/grafana/provisioning/datasources/prometheus.yaml`

Изменения общих application/repository файлов не копировались целиком. Их точный объём остаётся проверяемым по ссылкам на авторские коммиты выше.
