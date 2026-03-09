export type AuthMethod = "email" | "google" | "passkey";

export interface AuthUser {
  id: string;
  name: string;
  email: string;
  avatar?: string;
  authMethod: AuthMethod;
  twoFactorEnabled: boolean;
  passkeys: { id: string; name: string; createdAt: string }[];
}

export type Category = "all" | "primary" | "updates" | "newsletters" | "promotions";
export type FolderSlug = "inbox" | "starred" | "sent" | "drafts" | "archive" | "trash";

export interface Contact {
  name: string;
  email: string;
  avatar?: string;
}

export interface Attachment {
  id: string;
  name: string;
  size: number;
  type: string;
}

export interface Email {
  id: string;
  threadId: string;
  from: Contact;
  to: Contact[];
  cc?: Contact[];
  bcc?: Contact[];
  subject: string;
  body: string;
  snippet: string;
  date: string; // ISO string
  read: boolean;
  starred: boolean;
  folder: FolderSlug;
  category: Category;
  labels: string[];
  attachments: Attachment[];
  aiSummary?: string;
  aiPriority?: "urgent" | "action" | "info";
  suggestedReplies?: string[];
  replyTo?: string; // email id this replies to
}

export interface Thread {
  id: string;
  subject: string;
  emails: Email[];
  participants: Contact[];
  lastDate: string;
  unreadCount: number;
  starred: boolean;
  category: Category;
  folder: FolderSlug;
  labels: string[];
  aiSummary?: string;
  aiPriority?: "urgent" | "action" | "info";
}

export interface Label {
  id: string;
  name: string;
  color: string;
}

export interface ComposeState {
  id: string;
  to: Contact[];
  cc: Contact[];
  bcc: Contact[];
  subject: string;
  body: string;
  attachments: Attachment[];
  replyToId?: string;
  minimized: boolean;
}
