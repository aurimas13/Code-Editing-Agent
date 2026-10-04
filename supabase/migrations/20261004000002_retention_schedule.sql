-- Run the retention purge every night at 03:17 UTC.
--
-- Kept in its own migration because it needs the pg_cron extension. If your
-- project does not have pg_cron, skip this file and call
-- `select public.purge_expired();` from any scheduler you already run.

create extension if not exists pg_cron;

select cron.schedule(
  'purge-expired-agent-data',
  '17 3 * * *',
  $$select public.purge_expired()$$
);
