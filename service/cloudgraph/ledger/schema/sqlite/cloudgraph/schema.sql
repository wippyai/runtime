CREATE TABLE IF NOT EXISTS deploys (
	id TEXT PRIMARY KEY,
	scope TEXT NOT NULL,
	mode TEXT NOT NULL DEFAULT 'full' CHECK (mode IN ('full')),
	status TEXT NOT NULL CHECK (status IN ('planning','executing','succeeded','failed','partial')),
	error TEXT,
	finalized_seq INTEGER,
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deploys_scope_finalized ON deploys(scope, status, finalized_seq DESC);

CREATE TABLE IF NOT EXISTS resource_specs (
	deploy_id TEXT NOT NULL REFERENCES deploys(id),
	resource_id TEXT NOT NULL,
	type TEXT NOT NULL,
	provider_type TEXT NOT NULL,
	desired TEXT NOT NULL,
	dependencies TEXT NOT NULL DEFAULT '{}',
	PRIMARY KEY (deploy_id, resource_id)
);

CREATE TABLE IF NOT EXISTS plans (
	id TEXT PRIMARY KEY,
	deploy_id TEXT NOT NULL REFERENCES deploys(id),
	operations TEXT NOT NULL,
	edges TEXT NOT NULL,
	waves TEXT NOT NULL,
	spec_hash TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_plans_deploy ON plans(deploy_id);

CREATE TABLE IF NOT EXISTS operations (
	id TEXT PRIMARY KEY,
	deploy_id TEXT NOT NULL REFERENCES deploys(id),
	resource_id TEXT NOT NULL,
	action TEXT NOT NULL CHECK (action IN ('create')),
	status TEXT NOT NULL CHECK (status IN ('planned','dispatched','in_progress','committed','failed')),
	current_actor_incarnation INTEGER NOT NULL DEFAULT 0,
	last_signal_seq INTEGER NOT NULL DEFAULT 0,
	actor_pid TEXT,
	error TEXT,
	result TEXT,
	created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_one_active_per_resource
ON operations(resource_id)
WHERE status IN ('dispatched','in_progress');

CREATE INDEX IF NOT EXISTS idx_ops_deploy ON operations(deploy_id);

CREATE TABLE IF NOT EXISTS operation_signals (
	operation_id TEXT NOT NULL REFERENCES operations(id),
	incarnation INTEGER NOT NULL,
	seq INTEGER NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('phase','output','checkpoint','failed')),
	phase TEXT CHECK (phase IS NULL OR phase IN
		('allocate_started','allocate_committed','configure_started',
		 'configure_committed','verify_started','verify_passed',
		 'verify_failed','delete_started','delete_committed')),
	payload TEXT NOT NULL,
	fencing_token INTEGER NOT NULL DEFAULT 0,
	resource_generation INTEGER NOT NULL DEFAULT 0,
	accepted INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (operation_id, incarnation, seq)
);

CREATE TABLE IF NOT EXISTS resource_instances (
	resource_id TEXT PRIMARY KEY,
	scope TEXT NOT NULL,
	type TEXT NOT NULL,
	provider_type TEXT NOT NULL,
	lifecycle TEXT NOT NULL CHECK (lifecycle IN ('absent','allocating','configuring','verifying','active','deleting','deleted')),
	generation INTEGER NOT NULL DEFAULT 0,
	desired TEXT,
	outputs TEXT,
	output_epoch INTEGER NOT NULL DEFAULT 0,
	created_by_deploy TEXT,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS dependency_edges (
	deploy_id TEXT NOT NULL,
	source_resource_id TEXT NOT NULL,
	target_resource_id TEXT NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('create','configure','health','ordering','placement','attachment','ownership','traffic','soft')),
	PRIMARY KEY (deploy_id, source_resource_id, target_resource_id, kind)
);

CREATE TABLE IF NOT EXISTS counters (
	name TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);
