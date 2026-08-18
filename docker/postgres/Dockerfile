# Source PostgreSQL with logical replication enabled
FROM postgres:16-alpine

# Copy configuration for logical replication
COPY postgresql.conf /etc/postgresql/postgresql.conf
COPY pg_hba.conf /etc/postgresql/pg_hba.conf
COPY seed.sql /docker-entrypoint-initdb.d/01-seed.sql

# Use custom config
CMD ["postgres", "-c", "config_file=/etc/postgresql/postgresql.conf", "-c", "hba_file=/etc/postgresql/pg_hba.conf"]