-- Очистка таблиц калькулятора (освобождение места в БД).
-- Запуск: в Yandex Cloud Console → Managed Service for PostgreSQL → SQL,
-- либо: psql "$POSTGRES_DSN" -f scripts/clean-calculator-db.sql
--
-- Таблицы: diff_bets (валуи), odds_snapshots и odds_snapshot_history (прогрузы).
-- После очистки калькулятор при следующем старте создаст данные заново.

BEGIN;

TRUNCATE TABLE diff_bets RESTART IDENTITY;
TRUNCATE TABLE odds_snapshots RESTART IDENTITY;
TRUNCATE TABLE odds_snapshot_history RESTART IDENTITY;

COMMIT;

-- Опционально: полная утилизация места на диске (блокирует таблицы на время выполнения).
-- Раскомментируйте, если нужно максимально освободить место:
-- VACUUM FULL diff_bets;
-- VACUUM FULL odds_snapshots;
-- VACUUM FULL odds_snapshot_history;
