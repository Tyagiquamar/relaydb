-- RelayDB metadata schema
-- This schema stores all CDC state: events, checkpoints, consumer offsets, replay sessions, DLQ

-- Schema migration tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sources: PostgreSQL instances we capture from
CREATE TABLE sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    
    -- Connection (encrypted)
    credential_blob BYTEA NOT NULL,  -- AES-GCM encrypted connection string
    credential_version INT NOT NULL DEFAULT 1,
    
    -- Replication identity
    replication_slot TEXT NOT NULL,
    publication TEXT NOT NULL,
    
    -- Status
    status TEXT NOT NULL DEFAULT 'registered' 
        CHECK (status IN ('registered', 'connecting', 'streaming', 'degraded', 'paused', 'error')),
    status_message TEXT,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Source checkpoints: LSN state machine with fencing
CREATE TABLE source_checkpoints (
    source_id UUID PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    
    -- LSN tracking (three distinct concepts)
    received_lsn pg_lsn NOT NULL DEFAULT '0/0',
    persisted_lsn pg_lsn NOT NULL DEFAULT '0/0',  -- flushed to metadata DB
    acknowledged_lsn pg_lsn NOT NULL DEFAULT '0/0',  -- sent to source
    
    -- Ownership fencing
    capture_owner TEXT NOT NULL,
    owner_generation BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ NOT NULL,
    
    -- Metadata
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Relation versions: schema fingerprint history for replay-idempotent decoding
CREATE TABLE relation_versions (
    id BIGSERIAL PRIMARY KEY,
    source_id UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    relation_oid OID NOT NULL,
    schema_name TEXT NOT NULL,
    table_name TEXT NOT NULL,
    fingerprint TEXT NOT NULL,  -- hash of column definitions
    column_definitions JSONB NOT NULL,  -- [{name, type, nullable, position}, ...]
    replica_identity TEXT NOT NULL CHECK (replica_identity IN ('default', 'nothing', 'full', 'index')),
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE(source_id, relation_oid, fingerprint)
);

-- CDC transactions: transaction-level grouping
CREATE TABLE cdc_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    
    -- Transaction identity
    xid xid8 NOT NULL,  -- PostgreSQL transaction ID (wraps, use with LSN)
    commit_end_lsn pg_lsn NOT NULL,
    commit_timestamp TIMESTAMPTZ NOT NULL,
    
    -- Stats
    event_count INT NOT NULL DEFAULT 0,
    total_bytes BIGINT NOT NULL DEFAULT 0,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE(source_id, commit_end_lsn)
);

-- Events: the core CDC event store
CREATE TABLE events (
    -- Event identity: ULID for sortability
    id BYTEA PRIMARY KEY,  -- 16-byte ULID
    
    -- Source and transaction
    source_id UUID NOT NULL REFERENCES sources(id) ON DELETE RESTRICT,
    transaction_id UUID NOT NULL REFERENCES cdc_transactions(id) ON DELETE RESTRICT,
    
    -- Ordering: (commit_end_lsn, sequence_number) is canonical
    commit_end_lsn pg_lsn NOT NULL,
    sequence_number INT NOT NULL,
    
    -- Event metadata
    schema_name TEXT NOT NULL,
    table_name TEXT NOT NULL,
    operation TEXT NOT NULL CHECK (operation IN ('insert', 'update', 'delete')),
    
    -- Schema version at decode time
    relation_version_id BIGINT NOT NULL REFERENCES relation_versions(id),
    
    -- Payload with column-state markers
    -- JSONB with special markers: {"_state": "unchanged_toast"} for TOAST columns
    before JSONB,  -- null for INSERT, partial for REPLICA IDENTITY DEFAULT, full for FULL
    after JSONB,   -- null for DELETE
    key_columns JSONB,  -- primary/replica key values for partitioning
    
    -- Payload hash for idempotency verification
    payload_hash TEXT NOT NULL,  -- SHA-256 of canonical JSON
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    -- Idempotency: stable identity under WAL replay
    UNIQUE(source_id, commit_end_lsn, sequence_number)
);

-- Consumers and groups
CREATE TABLE consumers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE consumer_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_id UUID NOT NULL REFERENCES consumers(id) ON DELETE CASCADE,
    source_id UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    
    -- Partition topology (versioned, fixed at creation)
    partition_count INT NOT NULL CHECK (partition_count > 0),
    partition_hash_version INT NOT NULL DEFAULT 1,
    
    -- Poison event policy
    poison_event_policy TEXT NOT NULL DEFAULT 'dlq' 
        CHECK (poison_event_policy IN ('dlq', 'halt')),
    max_attempts INT NOT NULL DEFAULT 5,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE(consumer_id, source_id, name)
);

-- Consumer offsets: per-group, per-partition progress
CREATE TABLE consumer_offsets (
    group_id UUID NOT NULL REFERENCES consumer_groups(id) ON DELETE CASCADE,
    partition INT NOT NULL,
    
    -- Canonical position: (commit_end_lsn, sequence_number)
    commit_end_lsn pg_lsn NOT NULL,
    sequence_number INT NOT NULL,
    
    -- Event ID of last acknowledged event
    last_event_id BYTEA,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    PRIMARY KEY (group_id, partition)
);

-- Partition leases: consumer group membership with fencing
CREATE TABLE partition_leases (
    group_id UUID NOT NULL REFERENCES consumer_groups(id) ON DELETE CASCADE,
    partition INT NOT NULL,
    
    -- Lease ownership
    lease_owner TEXT NOT NULL,  -- consumer member ID
    lease_generation BIGINT NOT NULL DEFAULT 0,  -- fencing token
    lease_expires_at TIMESTAMPTZ NOT NULL,
    
    -- Last heartbeat
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    PRIMARY KEY (group_id, partition)
);

-- Webhook sinks
CREATE TABLE webhook_sinks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    
    -- Target
    url TEXT NOT NULL,
    secret_encrypted BYTEA NOT NULL,  -- AES-GCM encrypted HMAC secret
    
    -- Filtering
    source_id UUID REFERENCES sources(id) ON DELETE CASCADE,
    schema_filter TEXT,  -- regex or exact match
    table_filter TEXT,
    operation_filter TEXT,  -- comma-separated: insert,update,delete
    
    -- Retry policy
    max_attempts INT NOT NULL DEFAULT 5,
    retry_initial_delay INTERVAL NOT NULL DEFAULT '1 second',
    retry_max_delay INTERVAL NOT NULL DEFAULT '5 minutes',
    
    -- Circuit breaker
    circuit_breaker_enabled BOOLEAN NOT NULL DEFAULT true,
    
    -- Status
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Delivery attempts: webhook delivery history
CREATE TABLE delivery_attempts (
    id BIGSERIAL PRIMARY KEY,
    
    -- Target
    sink_id UUID NOT NULL REFERENCES webhook_sinks(id) ON DELETE CASCADE,
    event_id BYTEA NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    
    -- Attempt tracking
    attempt_number INT NOT NULL,
    
    -- Result
    status TEXT NOT NULL CHECK (status IN ('pending', 'success', 'retryable', 'permanent_failure')),
    http_status INT,
    response_body TEXT,  -- truncated
    error_message TEXT,
    
    -- Timing
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    next_retry_at TIMESTAMPTZ,
    
    -- Idempotency
    idempotency_key TEXT NOT NULL,
    
    UNIQUE(sink_id, event_id, attempt_number)
);

-- Dead letter events: poison events that exhausted retries
CREATE TABLE dead_letter_events (
    id BIGSERIAL PRIMARY KEY,
    
    -- Original event
    event_id BYTEA NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    source_id UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    
    -- Failure context
    sink_id UUID REFERENCES webhook_sinks(id) ON DELETE SET NULL,
    consumer_group_id UUID REFERENCES consumer_groups(id) ON DELETE SET NULL,
    
    -- Failure details
    failure_reason TEXT NOT NULL,
    attempt_history JSONB NOT NULL,  -- [{attempt, timestamp, error}, ...]
    
    -- Status
    status TEXT NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'retried', 'discarded')),
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    
    -- One active DLQ entry per event per sink/group
    UNIQUE(event_id, sink_id, consumer_group_id)
);

-- Replay sessions: historical re-processing
CREATE TABLE replay_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    
    -- Source and range
    source_id UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    
    -- Start position (one of these must be set)
    start_timestamp TIMESTAMPTZ,
    start_lsn pg_lsn,
    start_event_id BYTEA,
    
    -- End position (optional, null = now)
    end_timestamp TIMESTAMPTZ,
    end_lsn pg_lsn,
    end_event_id BYTEA,
    
    -- Filter
    schema_filter TEXT,
    table_filter TEXT,
    operation_filter TEXT,
    
    -- Destination
    destination_type TEXT NOT NULL 
        CHECK (destination_type IN ('consumer', 'webhook', 'jsonl')),
    destination_config JSONB,  -- webhook sink ID, file path, etc.
    
    -- Progress
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'paused', 'completed', 'cancelled', 'expired', 'failed')),
    events_processed BIGINT NOT NULL DEFAULT 0,
    events_total BIGINT,
    last_processed_lsn pg_lsn,
    
    -- Error
    error_message TEXT,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

-- Indexes for common queries
CREATE INDEX idx_events_source_lsn ON events(source_id, commit_end_lsn, sequence_number);
CREATE INDEX idx_events_table ON events(source_id, schema_name, table_name);
CREATE INDEX idx_events_operation ON events(source_id, operation);
CREATE INDEX idx_events_created ON events(created_at);

CREATE INDEX idx_cdc_transactions_source ON cdc_transactions(source_id, commit_end_lsn);
CREATE INDEX idx_cdc_transactions_timestamp ON cdc_transactions(commit_timestamp);

CREATE INDEX idx_delivery_attempts_pending ON delivery_attempts(status, next_retry_at) 
    WHERE status = 'pending' OR status = 'retryable';
CREATE INDEX idx_delivery_attempts_sink ON delivery_attempts(sink_id, started_at);

CREATE INDEX idx_dlq_status ON dead_letter_events(status, created_at);
CREATE INDEX idx_replay_status ON replay_sessions(status, created_at);

-- Record migration
INSERT INTO schema_migrations (version) VALUES (1) ON CONFLICT DO NOTHING;