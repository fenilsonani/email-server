export interface User {
  id: number;
  username: string;
  domain: string;
  email: string;
  is_admin: boolean;
  quota_bytes: number;
  used_bytes: number;
  created_at: string;
}

export interface Domain {
  id: number;
  name: string;
  mail_hostname: string;
  is_primary: boolean;
  user_count: number;
  dkim_enabled: boolean;
  created_at: string;
}

export interface DashboardStats {
  total_users: number;
  total_domains: number;
  total_messages: number;
  queue_pending: number;
  queue_failed: number;
  total_lists: number;
  total_list_members: number;
  pending_moderation: number;
  uptime_seconds: number;
  uptime_human: string;
  recent_activity: ActivityItem[];
  server_hostname: string;
  failed_logins_24h: number;
  bounced_24h: number;
}

export interface ActivityItem {
  time: string;
  type: string;
  description: string;
  status: string;
}

export interface AuthLog {
  username: string;
  remote_ip: string;
  protocol: string;
  success: boolean;
  created_at: string;
}

export interface DeliveryLog {
  sender: string;
  recipient: string;
  status: string;
  message: string;
  created_at: string;
}

export interface AuditLogEntry {
  username: string;
  event: string;
  target: string;
  details: string;
  ip_address: string;
  created_at: string;
}

export interface MailingList {
  id: number;
  address: string;
  name: string;
  description: string;
  is_active: boolean;
  member_count: number;
  pending_moderation: number;
  created_at: string;
}

export interface FeaturesOverview {
  screener_count: number;
  alias_count: number;
  vip_count: number;
  scheduled_count: number;
  snoozed_count: number;
}

export interface Organization {
  id: number;
  name: string;
  slug: string;
  owner_user_id: number;
  preset: string;
  settings: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface OrgMember {
  id: number;
  org_id: number;
  user_id: number;
  role: string;
  username: string;
  email: string;
  created_at: string;
}

export interface APIKey {
  id: number;
  domain_id: number;
  key_prefix: string;
  name: string;
  scopes: string;
  is_active: boolean;
  rate_limit_per_hour: number;
  last_used_at: string | null;
  created_at: string;
  expires_at: string | null;
  domain_name: string;
}

export interface WebhookConfig {
  id: number;
  domain_id: number;
  url: string;
  events: string;
  is_active: boolean;
  failure_count: number;
  last_triggered_at: string | null;
  created_at: string;
  domain_name: string;
}

export interface EmailTemplate {
  id: number;
  domain_id: number;
  slug: string;
  name: string;
  subject: string;
  html_body: string | null;
  text_body: string | null;
  variables: string | null;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  domain_name: string;
}

export interface SentEmail {
  id: number;
  from_email: string;
  to_email: string;
  subject: string | null;
  status: string;
  opened_count: number;
  clicked_count: number;
  created_at: string;
  delivered_at: string | null;
  bounced_at: string | null;
  template_slug: string | null;
}

export interface APIStats {
  sent_today: number;
  sent_week: number;
  sent_month: number;
  active_api_keys: number;
  active_webhooks: number;
  active_templates: number;
  delivery_rate: number;
  open_rate: number;
}

export interface SystemInfo {
  hostname: string;
  domain: string;
  uptime_seconds: number;
  uptime_human: string;
  version: string;
  go_version: string;
  config: {
    imap_port: number;
    imaps_port: number;
    smtp_port: number;
    smtps_port: number;
    admin_port: number;
    storage_path: string;
  };
}
