export type VpsStatus =
	| 'requested'
	| 'provisioning'
	| 'bootstrapping'
	| 'installing_agent'
	| 'active'
	| 'restarting'
	| 'suspending'
	| 'suspended'
	| 'resuming'
	| 'terminating'
	| 'terminated'
	| 'error';

export type ProvisioningStep =
	| 'select_provider'
	| 'create_server'
	| 'wait_server_ready'
	| 'bootstrap'
	| 'install_agent'
	| 'activate';

export type PlanTier = 'standard';

export type SubscriptionStatus = 'active' | 'past_due' | 'canceled' | 'suspended';

export interface VpsInstance {
	id: string;
	name: string;
	status: VpsStatus;
	step?: ProvisioningStep;
	provider: string;
	ipv4: string | null;
	root_password?: string | null;
	region: string;
	last_heartbeat_at: string | null;
	created_at: string;
}

export interface Subscription {
	plan: PlanTier;
	status: SubscriptionStatus;
	current_period_end: string;
}

export interface DashboardState {
	instances: VpsInstance[];
	subscription: Subscription | null;
	pending_jobs: number;
}

export interface AgentConfig {
	id: string;
	vps_instance_id: string;
	config: Record<string, unknown>;
	version: number;
}

export interface Snapshot {
	id: string;
	instance_id: string;
	name: string;
	created_at: string;
	size_gb: number;
}

export interface PlanInfo {
	tier: PlanTier;
	name: string;
	price_monthly: number;
	features: string[];
}
