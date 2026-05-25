-- +goose Up
-- +goose StatementBegin

-- Fix Pro tariff price (prepaid = base price)
UPDATE tariff_versions
SET base_price_rub = 10000.00,
    prepaid_amount_rub = 10000.00
WHERE tariff_id = (SELECT id FROM tariffs WHERE code = 'pro')
  AND valid_from = '2024-01-01';

-- Fix Basic overage price to match frontend (3 RUB per operation)
UPDATE tariff_service_prices
SET included_price_rub = 3.00,
    overage_price_rub = 3.00
WHERE tariff_version_id = (SELECT id FROM tariff_versions WHERE tariff_id = (SELECT id FROM tariffs WHERE code = 'basic') AND valid_from = '2024-01-01')
  AND service_id = 'passport_rf';

-- Fix Pro included / overage prices (included slightly less than overage)
UPDATE tariff_service_prices
SET included_price_rub = 2.50,
    overage_price_rub = 3.00
WHERE tariff_version_id = (SELECT id FROM tariff_versions WHERE tariff_id = (SELECT id FROM tariffs WHERE code = 'pro') AND valid_from = '2024-01-01')
  AND service_id = 'passport_rf';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Revert Pro tariff price
UPDATE tariff_versions
SET base_price_rub = 20000.00,
    prepaid_amount_rub = 20000.00
WHERE tariff_id = (SELECT id FROM tariffs WHERE code = 'pro')
  AND valid_from = '2024-01-01';

-- Revert Basic price
UPDATE tariff_service_prices
SET included_price_rub = 7.00,
    overage_price_rub = 7.00
WHERE tariff_version_id = (SELECT id FROM tariff_versions WHERE tariff_id = (SELECT id FROM tariffs WHERE code = 'basic') AND valid_from = '2024-01-01')
  AND service_id = 'passport_rf';

-- Revert Pro prices
UPDATE tariff_service_prices
SET included_price_rub = 1.00,
    overage_price_rub = 5.00
WHERE tariff_version_id = (SELECT id FROM tariff_versions WHERE tariff_id = (SELECT id FROM tariffs WHERE code = 'pro') AND valid_from = '2024-01-01')
  AND service_id = 'passport_rf';

-- +goose StatementEnd
