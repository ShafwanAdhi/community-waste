BEGIN;

CREATE TABLE households (
    id UUID PRIMARY KEY,
    owner_name TEXT NOT NULL CHECK (length(btrim(owner_name)) > 0),
    address TEXT NOT NULL CHECK (length(btrim(address)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE pickups (
    id UUID PRIMARY KEY,
    household_id UUID NOT NULL REFERENCES households(id) ON DELETE RESTRICT,
    type TEXT NOT NULL CHECK (type IN ('organic', 'plastic', 'paper', 'electronic')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'scheduled', 'completed', 'canceled')),
    pickup_date TIMESTAMPTZ,
    safety_check BOOLEAN,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pickups_electronic_safety_required CHECK (type <> 'electronic' OR safety_check IS NOT NULL),
    CONSTRAINT pickups_id_household_unique UNIQUE (id, household_id)
);

CREATE TABLE payments (
    id UUID PRIMARY KEY,
    household_id UUID NOT NULL REFERENCES households(id) ON DELETE RESTRICT,
    waste_id UUID NOT NULL UNIQUE,
    amount NUMERIC(12,2) NOT NULL CHECK (amount > 0 AND amount <> 'NaN'::numeric),
    payment_date TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'failed')),
    proof_file_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payments_pickup_household_fk FOREIGN KEY (waste_id, household_id)
        REFERENCES pickups(id, household_id) ON DELETE RESTRICT
);

CREATE INDEX households_created_at_id_idx ON households (created_at DESC, id DESC);
CREATE INDEX pickups_household_status_idx ON pickups (household_id, status);
CREATE INDEX pickups_status_idx ON pickups (status);
CREATE INDEX payments_household_status_idx ON payments (household_id, status);
CREATE INDEX payments_status_idx ON payments (status);
CREATE INDEX payments_payment_date_idx ON payments (payment_date);

COMMIT;
