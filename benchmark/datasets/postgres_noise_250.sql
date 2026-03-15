-- Deterministic Postgres noise pack for benchmark stress runs.
-- Creates exactly 250 noise tables under schema "bench_noise".
-- Safe to rerun: CREATE ... IF NOT EXISTS is used for tables and indexes.

CREATE SCHEMA IF NOT EXISTS bench_noise;

DO $$
DECLARE
  i INT;
  p TEXT;
  s TEXT;
  tbl TEXT;
  prefixes TEXT[] := ARRAY[
    'usr_meta_v',
    'temp_log_final_v',
    'temp_log_old_v',
    'acct_shadow_v',
    'cache_rollup_v',
    'ops_snapshot_v',
    'report_buffer_v',
    'billing_temp_v',
    'session_archive_v',
    'event_stash_v'
  ];
  suffixes TEXT[] := ARRAY[
    'alpha',
    'beta',
    'gamma',
    'delta',
    'omega',
    'legacy',
    'mirror',
    'backup',
    'draft',
    'candidate'
  ];
BEGIN
  FOR i IN 1..250 LOOP
    p := prefixes[1 + ((i - 1) % array_length(prefixes, 1))];
    s := suffixes[1 + ((i * 3) % array_length(suffixes, 1))];
    tbl := lower(format('%s%s_%s', p, s, lpad(i::TEXT, 3, '0')));

    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS bench_noise.%I (
        id BIGSERIAL PRIMARY KEY,
        tenant_id INT NOT NULL,
        ref_uuid UUID NOT NULL,
        status TEXT NOT NULL DEFAULT ''new'',
        stage SMALLINT NOT NULL DEFAULT 0,
        event_code INT NOT NULL DEFAULT 0,
        amount NUMERIC(12,2) NOT NULL DEFAULT 0,
        score DOUBLE PRECISION NOT NULL DEFAULT 0,
        is_active BOOLEAN NOT NULL DEFAULT false,
        created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        payload JSONB NOT NULL DEFAULT ''{}''::jsonb,
        tags JSONB NOT NULL DEFAULT ''[]''::jsonb
      )',
      tbl
    );

    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS %I ON bench_noise.%I (tenant_id, created_at DESC)',
      format('bn_%s_tenant_created_idx', lpad(i::TEXT, 3, '0')),
      tbl
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS %I ON bench_noise.%I (status, event_code)',
      format('bn_%s_status_event_idx', lpad(i::TEXT, 3, '0')),
      tbl
    );
    EXECUTE format(
      'CREATE INDEX IF NOT EXISTS %I ON bench_noise.%I USING gin (payload)',
      format('bn_%s_payload_gin_idx', lpad(i::TEXT, 3, '0')),
      tbl
    );

    EXECUTE format(
      'COMMENT ON TABLE bench_noise.%I IS %L',
      tbl,
      format('Deterministic benchmark noise table #%s (label=noise).', i)
    );
  END LOOP;
END $$;

-- Verify after running:
-- SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname = 'bench_noise';
