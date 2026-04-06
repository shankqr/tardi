export type VpsStatus =
	| 'requested'
	| 'provisioning'
	| 'bootstrapping'
	| 'installing_agent'
	| 'active'
	| 'restarting'
	| 'snapshotting'
	| 'restoring'
	| 'upgrading'
	| 'downgrading'
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

export type AgentFramework = 'openclaw' | 'hermes';

export type PlanTier = 'standard' | 'pro';

export type SubscriptionStatus = 'active' | 'past_due' | 'canceled' | 'suspended';

export interface VpsInstance {
	id: string;
	name: string;
	framework: AgentFramework;
	status: VpsStatus;
	step?: ProvisioningStep;
	provider: string;
	ipv4: string | null;
	root_password?: string | null;
	region: string;
	agent_status: string | null;
	agent_error?: string | null;
	last_heartbeat_at: string | null;
	dashboard_url: string | null;
	openclaw_auth_token?: string | null;
	openclaw_version?: string | null;
	openclaw_update_status?: string | null;
	preview_url?: string | null;
	created_at: string;
}

export interface Subscription {
	plan: PlanTier;
	status: SubscriptionStatus;
	current_period_end: string;
	cancel_at_period_end: boolean;
}

export interface DashboardState {
	instances: VpsInstance[];
	subscription: Subscription | null;
	pending_jobs: number;
	snapshots: Snapshot[];
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
	status: 'creating' | 'ready' | 'deleting' | 'error';
	created_at: string;
	size_gb?: number;
}

export interface ModelInfo {
	id: string;
	display_name: string;
	provider: string;
	tier: 'free' | 'paid';
	is_default: boolean;
	tags?: string[];
	description?: string;
	context_length?: number;
	prompt_price?: string;
	completion_price?: string;
}

export type AIProvider = 'openrouter' | 'anthropic' | 'openai';

export interface AIProviderConfig {
	provider: AIProvider;
	model: string;
	openrouter_api_key?: string;
	anthropic_api_key?: string;
	openai_api_key?: string;
}

export interface GoogleOAuthStatus {
	connected: boolean;
	email?: string;
	scopes?: string[];
}

export interface PlanInfo {
	tier: PlanTier;
	name: string;
	price_monthly: number;
	features: string[];
}
