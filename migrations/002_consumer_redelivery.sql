CREATE TABLE consumer_delivery_attempts (
    group_id UUID NOT NULL REFERENCES consumer_groups(id) ON DELETE CASCADE,
    partition INT NOT NULL,
    event_id BYTEA NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    lease_owner TEXT NOT NULL,
    lease_generation BIGINT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('issued', 'retry_scheduled', 'dead_lettered')),
    nack_attempts INT NOT NULL DEFAULT 0 CHECK (nack_attempts >= 0),
    next_delivery_at TIMESTAMPTZ,
    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, partition, event_id)
);

CREATE TABLE consumer_partition_states (
    group_id UUID NOT NULL REFERENCES consumer_groups(id) ON DELETE CASCADE,
    partition INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'halted')),
    halted_event_id BYTEA REFERENCES events(id) ON DELETE SET NULL,
    halt_reason TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, partition)
);

CREATE INDEX idx_consumer_delivery_attempts_due
    ON consumer_delivery_attempts (group_id, partition, next_delivery_at)
    WHERE state = 'retry_scheduled';

CREATE INDEX idx_consumer_delivery_attempts_issued
    ON consumer_delivery_attempts (group_id, partition, lease_owner, lease_generation)
    WHERE state = 'issued';