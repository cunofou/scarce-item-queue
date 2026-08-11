-- +goose Up
UPDATE products
SET title = 'Редкая фигурка Лунный исследователь',
    description = 'Коллекционная виниловая фигурка исследователя Луны, один запечатанный экземпляр',
    image_url = '/product-images/rare-collectible.webp',
    category = 'collectibles'
WHERE id = '11111111-1111-1111-1111-111111111111';

UPDATE products
SET title = 'Набор фигурок Космическая миссия',
    description = 'Набор коллекционных фигурок астронавтов и марсохода из ограниченной серии',
    image_url = '/product-images/collectible-set.webp',
    category = 'collectibles'
WHERE id = '22222222-2222-2222-2222-222222222222';

UPDATE products
SET title = 'Коллекционная модель Марсоход',
    description = 'Детализированная масштабная модель марсохода в защитной витрине, тираж распродан',
    image_url = '/product-images/sold-out-collectible.webp',
    category = 'collectibles'
WHERE id = '33333333-3333-3333-3333-333333333333';

UPDATE products SET image_url = '/product-images/astronaut-figure.webp'
WHERE id = '44444444-4444-4444-4444-444444444444';

UPDATE products SET image_url = '/product-images/retro-robot.webp'
WHERE id = '55555555-5555-5555-5555-555555555555';

UPDATE products SET image_url = '/product-images/rocket-model.webp'
WHERE id = '66666666-6666-6666-6666-666666666666';

UPDATE products SET image_url = '/product-images/limited-red-sneakers.webp'
WHERE id = '77777777-7777-7777-7777-777777777777';

UPDATE products SET image_url = '/product-images/street-black-sneakers.webp'
WHERE id = '88888888-8888-8888-8888-888888888888';

UPDATE products SET image_url = '/product-images/heritage-watch.webp'
WHERE id = '99999999-9999-9999-9999-999999999999';

UPDATE products SET image_url = '/product-images/active-smartwatch.webp'
WHERE id = 'aaaaaaaa-1111-4111-8111-111111111111';

UPDATE products
SET image_url = '/product-images/first-press-vinyl.webp', category = 'audio'
WHERE id = 'bbbbbbbb-2222-4222-8222-222222222222';

UPDATE products
SET image_url = '/product-images/portable-speaker.webp', category = 'audio'
WHERE id = 'cccccccc-3333-4333-8333-333333333333';

-- +goose Down
UPDATE products
SET title = 'Дефицитный товар (1 шт.)',
    description = 'Очень редкая вещь, только один экземпляр',
    image_url = 'https://via.placeholder.com/300',
    category = 'collectibles'
WHERE id = '11111111-1111-1111-1111-111111111111';

UPDATE products
SET title = 'Популярный товар (3 шт.)',
    description = 'Товар со средним спросом',
    image_url = 'https://via.placeholder.com/300',
    category = 'collectibles'
WHERE id = '22222222-2222-2222-2222-222222222222';

UPDATE products
SET title = 'Раскупленный товар',
    description = 'Уже всё продано',
    image_url = 'https://via.placeholder.com/300',
    category = 'collectibles'
WHERE id = '33333333-3333-3333-3333-333333333333';

UPDATE products SET image_url = 'https://via.placeholder.com/300'
WHERE id IN (
    '44444444-4444-4444-4444-444444444444',
    '55555555-5555-5555-5555-555555555555',
    '66666666-6666-6666-6666-666666666666',
    '77777777-7777-7777-7777-777777777777',
    '88888888-8888-8888-8888-888888888888',
    '99999999-9999-9999-9999-999999999999',
    'aaaaaaaa-1111-4111-8111-111111111111',
    'bbbbbbbb-2222-4222-8222-222222222222',
    'cccccccc-3333-4333-8333-333333333333'
);

UPDATE products SET category = 'music'
WHERE id = 'bbbbbbbb-2222-4222-8222-222222222222';

UPDATE products SET category = 'electronics'
WHERE id = 'cccccccc-3333-4333-8333-333333333333';
