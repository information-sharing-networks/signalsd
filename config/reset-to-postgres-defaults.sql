-- Reset PostgreSQL configuration to true default values
ALTER SYSTEM SET shared_buffers = '128MB';           -- Default is typically 128MB
ALTER SYSTEM SET effective_cache_size = '4GB';       -- Default is 4GB
ALTER SYSTEM SET work_mem = '4MB';                   -- Default is 4MB
ALTER SYSTEM SET maintenance_work_mem = '64MB';      -- Default is 64MB

-- Reload the configuration
SELECT pg_reload_conf();