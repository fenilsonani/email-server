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
