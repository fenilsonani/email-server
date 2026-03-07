"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { useMailStore } from "@/lib/store";
import { emails as allEmails } from "@/lib/mock-data";
import { ContactAvatar } from "@/components/shared/avatar-stack";
import { cn } from "@/lib/utils";
import { formatDistanceToNowStrict } from "date-fns";
import type { Contact, Email, FolderSlug } from "@/lib/types";
import {
  Archive,
  Calendar,
  Clock,
  FileText,
  Hash,
  Inbox,
  Mail,
  Paperclip,
  Search,
  Send,
  Sparkles,
  Star,
  Tag,
  Trash2,
  User,
  X,
} from "lucide-react";

// ── Filter chips ──
type FilterType = "from" | "to" | "has" | "in" | "label" | "date";
interface SearchFilter {
  type: FilterType;
  value: string;
  display: string;
}

const FILTER_SUGGESTIONS: { type: FilterType; prefix: string; icon: React.ElementType; examples: string[] }[] = [
  { type: "from", prefix: "from:", icon: User, examples: ["alice", "bob", "github"] },
  { type: "to", prefix: "to:", icon: Send, examples: ["me", "team"] },
  { type: "has", prefix: "has:", icon: Paperclip, examples: ["attachment", "star", "unread", "link"] },
  { type: "in", prefix: "in:", icon: Inbox, examples: ["inbox", "sent", "archive", "trash", "drafts"] },
  { type: "label", prefix: "label:", icon: Tag, examples: ["Work", "Personal", "Finance"] },
  { type: "date", prefix: "date:", icon: Calendar, examples: ["today", "week", "month"] },
];

const RECENT_SEARCHES = [
  "Project Veil",
  "from:alice attachment",
  "receipt stripe",
  "has:attachment",
  "Zustand",
];

// ── Unique contacts from emails ──
function getUniqueContacts(emailList: Email[]): Contact[] {
  const map = new Map<string, Contact>();
  for (const e of emailList) {
    map.set(e.from.email, e.from);
    for (const r of e.to) map.set(r.email, r);
  }
  return Array.from(map.values()).filter((c) => c.email !== "fenil@fenilsonani.com");
}

// ── Search logic ──
function matchesFilter(email: Email, filter: SearchFilter): boolean {
  switch (filter.type) {
    case "from":
      return email.from.name.toLowerCase().includes(filter.value) || email.from.email.toLowerCase().includes(filter.value);
    case "to":
      return filter.value === "me"
        ? email.to.some((c) => c.email === "fenil@fenilsonani.com")
        : email.to.some((c) => c.name.toLowerCase().includes(filter.value) || c.email.toLowerCase().includes(filter.value));
    case "has":
      if (filter.value === "attachment") return email.attachments.length > 0;
      if (filter.value === "star") return email.starred;
      if (filter.value === "unread") return !email.read;
      if (filter.value === "link") return email.body.includes("href=");
      return false;
    case "in":
      return email.folder === filter.value as FolderSlug;
    case "label":
      return email.labels.some((l) => l.toLowerCase() === filter.value);
    case "date": {
      const now = new Date();
      const emailDate = new Date(email.date);
      const diffMs = now.getTime() - emailDate.getTime();
      const diffDays = diffMs / (1000 * 60 * 60 * 24);
      if (filter.value === "today") return diffDays < 1;
      if (filter.value === "week") return diffDays < 7;
      if (filter.value === "month") return diffDays < 30;
      return false;
    }
    default:
      return false;
  }
}

function searchEmails(query: string, filters: SearchFilter[]): Email[] {
  let results = [...allEmails];

  // Apply filters
  for (const filter of filters) {
    results = results.filter((e) => matchesFilter(e, filter));
  }

  // Apply text query
  if (query.trim()) {
    const q = query.toLowerCase();
    results = results.filter(
      (e) =>
        e.subject.toLowerCase().includes(q) ||
        e.from.name.toLowerCase().includes(q) ||
        e.from.email.toLowerCase().includes(q) ||
        e.snippet.toLowerCase().includes(q) ||
        e.body.toLowerCase().includes(q) ||
        e.to.some((c) => c.name.toLowerCase().includes(q) || c.email.toLowerCase().includes(q))
    );
  }

  // Sort by date
  results.sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());

  return results;
}

// ── Highlight match ──
function Highlight({ text, query }: { text: string; query: string }) {
  if (!query.trim()) return <>{text}</>;
  const parts = text.split(new RegExp(`(${query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")})`, "gi"));
  return (
    <>
      {parts.map((part, i) =>
        part.toLowerCase() === query.toLowerCase() ? (
          <mark key={i} className="bg-primary/25 text-foreground rounded-sm px-0.5">{part}</mark>
        ) : (
          <span key={i}>{part}</span>
        )
      )}
    </>
  );
}

// ── Main component ──
export function SearchOverlay({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const setSelectedEmailId = useMailStore((s) => s.setSelectedEmailId);
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<SearchFilter[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [showFilterHints, setShowFilterHints] = useState(false);

  const contacts = useMemo(() => getUniqueContacts(allEmails), []);
  const results = useMemo(() => searchEmails(query, filters), [query, filters]);
  const hasInput = query.trim().length > 0 || filters.length > 0;

  // Check if typing a filter prefix
  const activePrefix = useMemo(() => {
    const lower = query.toLowerCase();
    return FILTER_SUGGESTIONS.find((f) => lower.startsWith(f.prefix));
  }, [query]);

  // Contact suggestions when typing "from:" or bare text
  const contactSuggestions = useMemo(() => {
    if (!activePrefix || activePrefix.type !== "from") return [];
    const partial = query.slice(activePrefix.prefix.length).toLowerCase();
    return contacts.filter(
      (c) => c.name.toLowerCase().includes(partial) || c.email.toLowerCase().includes(partial)
    ).slice(0, 5);
  }, [query, activePrefix, contacts]);

  // Focus input on open
  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 50);
      setQuery("");
      setFilters([]);
      setSelectedIndex(0);
    }
  }, [open]);

  // Reset selection on results change
  useEffect(() => {
    setSelectedIndex(0);
  }, [query, filters]);

  const addFilter = useCallback((type: FilterType, value: string, display?: string) => {
    setFilters((prev) => [...prev, { type, value: value.toLowerCase(), display: display || value }]);
    setQuery("");
    inputRef.current?.focus();
  }, []);

  const removeFilter = useCallback((index: number) => {
    setFilters((prev) => prev.filter((_, i) => i !== index));
    inputRef.current?.focus();
  }, []);

  const selectEmail = useCallback((emailId: string) => {
    setSelectedEmailId(emailId);
    onClose();
  }, [setSelectedEmailId, onClose]);

  // Keyboard navigation
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
        return;
      }

      if (e.key === "Backspace" && query === "" && filters.length > 0) {
        removeFilter(filters.length - 1);
        return;
      }

      // Tab to accept filter
      if (e.key === "Tab" && activePrefix) {
        e.preventDefault();
        const value = query.slice(activePrefix.prefix.length).trim();
        if (value) {
          addFilter(activePrefix.type, value);
        }
        return;
      }

      // Enter: accept filter or select result
      if (e.key === "Enter") {
        e.preventDefault();
        if (activePrefix) {
          const value = query.slice(activePrefix.prefix.length).trim();
          if (value) {
            addFilter(activePrefix.type, value);
            return;
          }
        }
        if (contactSuggestions.length > 0 && selectedIndex < contactSuggestions.length) {
          addFilter("from", contactSuggestions[selectedIndex].email, contactSuggestions[selectedIndex].name);
          return;
        }
        const emailResults = hasInput ? results : [];
        if (emailResults[selectedIndex]) {
          selectEmail(emailResults[selectedIndex].id);
        }
        return;
      }

      const totalItems = hasInput
        ? contactSuggestions.length > 0
          ? contactSuggestions.length
          : results.length
        : RECENT_SEARCHES.length;

      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIndex((i) => Math.min(i + 1, totalItems - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIndex((i) => Math.max(i - 1, 0));
      }
    },
    [query, filters, activePrefix, contactSuggestions, results, hasInput, selectedIndex, onClose, addFilter, removeFilter, selectEmail]
  );

  // Group results
  const groupedResults = useMemo(() => {
    if (!hasInput) return [];
    const groups: { label: string; icon: React.ElementType; emails: Email[] }[] = [];

    const withAttachments = results.filter((e) => e.attachments.length > 0);
    const people = results.filter((e) => e.from.email !== "fenil@fenilsonani.com");
    const fromMe = results.filter((e) => e.from.email === "fenil@fenilsonani.com");

    // If we have a small set, just show "Results"
    if (results.length <= 8) {
      return [{ label: "Results", icon: Mail, emails: results }];
    }

    if (fromMe.length > 0) {
      groups.push({ label: "Sent", icon: Send, emails: fromMe.slice(0, 3) });
    }
    if (withAttachments.length > 0) {
      groups.push({ label: "With attachments", icon: Paperclip, emails: withAttachments.slice(0, 3) });
    }
    const remaining = people.filter((e) => !withAttachments.includes(e)).slice(0, 10);
    if (remaining.length > 0) {
      groups.unshift({ label: "Messages", icon: Mail, emails: remaining });
    }

    return groups;
  }, [results, hasInput]);

  // Flat list for keyboard nav
  const flatResults = useMemo(() => groupedResults.flatMap((g) => g.emails), [groupedResults]);

  return (
    <AnimatePresence>
      {open && (
      <motion.div
        key="search-backdrop"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        transition={{ duration: 0.15 }}
        className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm"
        onClick={onClose}
      >
        <motion.div
          key="search-card"
          initial={{ opacity: 0, y: -20, scale: 0.92, filter: "blur(4px)" }}
          animate={{ opacity: 1, y: 0, scale: 1, filter: "blur(0px)" }}
          exit={{ opacity: 0, y: -10, scale: 0.95, filter: "blur(2px)" }}
          transition={{ type: "spring", stiffness: 500, damping: 32, mass: 0.8 }}
          onClick={(e) => e.stopPropagation()}
          className="mx-auto mt-[12vh] w-full max-w-xl rounded-2xl border border-border bg-card shadow-2xl overflow-hidden md:mt-[15vh]"
        >
          {/* ── Search bar ── */}
          <div className="flex items-center gap-2 px-4 py-3 border-b border-border/50">
            <Search className="h-4 w-4 text-muted-foreground shrink-0" />

            {/* Filter chips */}
            <div className="flex flex-1 items-center gap-1.5 flex-wrap min-w-0">
              {filters.map((f, i) => (
                <span
                  key={i}
                  className="flex items-center gap-1 rounded-md bg-primary/15 px-2 py-0.5 text-[12px] text-primary shrink-0"
                >
                  <span className="text-primary/60">{f.type}:</span>
                  {f.display}
                  <button onClick={() => removeFilter(i)} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </span>
              ))}

              <input
                ref={inputRef}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={handleKeyDown}
                onFocus={() => setShowFilterHints(true)}
                placeholder={filters.length > 0 ? "Add more..." : "Search emails, people, attachments..."}
                className="flex-1 min-w-[120px] bg-transparent text-[13px] outline-none placeholder:text-muted-foreground/50"
              />
            </div>

            {(query || filters.length > 0) && (
              <button
                onClick={() => { setQuery(""); setFilters([]); }}
                className="flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent transition-colors shrink-0"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}

            <kbd className="hidden sm:inline-flex h-5 items-center rounded border border-border/50 bg-muted/50 px-1.5 text-[10px] font-mono text-muted-foreground/50">
              ESC
            </kbd>
          </div>

          {/* ── Filter hint bar ── */}
          {!hasInput && (
            <div className="flex items-center gap-1 overflow-x-auto px-4 py-2 border-b border-border/30 scrollbar-none">
              {FILTER_SUGGESTIONS.map((f) => (
                <button
                  key={f.type}
                  onClick={() => setQuery(f.prefix)}
                  className="flex items-center gap-1 shrink-0 rounded-md bg-muted/50 px-2 py-1 text-[11px] text-muted-foreground hover:bg-accent/50 transition-colors"
                >
                  <f.icon className="h-3 w-3" />
                  {f.prefix}
                </button>
              ))}
            </div>
          )}

          {/* ── Content ── */}
          <div className="max-h-[55vh] overflow-y-auto">

            {/* Contact suggestions for from: filter */}
            {activePrefix?.type === "from" && contactSuggestions.length > 0 && (
              <div className="py-1">
                <p className="px-4 py-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/50">
                  People
                </p>
                {contactSuggestions.map((contact, i) => (
                  <button
                    key={contact.email}
                    onMouseEnter={() => setSelectedIndex(i)}
                    onClick={() => addFilter("from", contact.email, contact.name)}
                    className={cn(
                      "flex w-full items-center gap-3 px-4 py-2 text-left transition-colors",
                      selectedIndex === i ? "bg-accent" : "hover:bg-accent/30"
                    )}
                  >
                    <ContactAvatar contact={contact} size="sm" />
                    <div className="flex-1 min-w-0">
                      <p className="text-[13px] truncate">{contact.name}</p>
                      <p className="text-[11px] text-muted-foreground/60 truncate">{contact.email}</p>
                    </div>
                  </button>
                ))}
              </div>
            )}

            {/* Filter autocomplete for other types */}
            {activePrefix && activePrefix.type !== "from" && (
              <div className="py-1">
                <p className="px-4 py-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/50">
                  {activePrefix.type} options
                </p>
                {activePrefix.examples
                  .filter((ex) => ex.toLowerCase().includes(query.slice(activePrefix.prefix.length).toLowerCase()))
                  .map((example, i) => (
                    <button
                      key={example}
                      onMouseEnter={() => setSelectedIndex(i)}
                      onClick={() => addFilter(activePrefix.type, example)}
                      className={cn(
                        "flex w-full items-center gap-3 px-4 py-2 text-left text-[13px] transition-colors",
                        selectedIndex === i ? "bg-accent" : "hover:bg-accent/30"
                      )}
                    >
                      <activePrefix.icon className="h-3.5 w-3.5 text-muted-foreground" />
                      <span>{activePrefix.prefix}<span className="text-foreground font-medium">{example}</span></span>
                    </button>
                  ))}
              </div>
            )}

            {/* Recent searches (when empty) */}
            {!hasInput && !activePrefix && (
              <div className="py-1">
                <p className="px-4 py-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/50">
                  Recent
                </p>
                {RECENT_SEARCHES.map((search, i) => (
                  <button
                    key={search}
                    onMouseEnter={() => setSelectedIndex(i)}
                    onClick={() => setQuery(search)}
                    className={cn(
                      "flex w-full items-center gap-3 px-4 py-2 text-left transition-colors",
                      selectedIndex === i ? "bg-accent" : "hover:bg-accent/30"
                    )}
                  >
                    <Clock className="h-3.5 w-3.5 text-muted-foreground/40" />
                    <span className="text-[13px] text-muted-foreground">{search}</span>
                  </button>
                ))}

                <div className="border-t border-border/30 mt-1 pt-1">
                  <p className="px-4 py-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/50">
                    Quick filters
                  </p>
                  {[
                    { label: "Unread", icon: Mail, action: () => addFilter("has", "unread", "unread") },
                    { label: "Starred", icon: Star, action: () => addFilter("has", "star", "star") },
                    { label: "Attachments", icon: Paperclip, action: () => addFilter("has", "attachment", "attachment") },
                    { label: "This week", icon: Calendar, action: () => addFilter("date", "week", "this week") },
                  ].map((item) => (
                    <button
                      key={item.label}
                      onClick={item.action}
                      className="flex w-full items-center gap-3 px-4 py-2 text-left text-[13px] text-muted-foreground hover:bg-accent/30 transition-colors"
                    >
                      <item.icon className="h-3.5 w-3.5 text-muted-foreground/40" />
                      {item.label}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {/* ── Search results ── */}
            {hasInput && !activePrefix && (
              <>
                {results.length === 0 ? (
                  <div className="flex flex-col items-center justify-center py-12 gap-3">
                    <Search className="h-8 w-8 text-muted-foreground/20" />
                    <div className="text-center">
                      <p className="text-[13px] text-muted-foreground">No results found</p>
                      <p className="text-[11px] text-muted-foreground/50 mt-1">Try different keywords or filters</p>
                    </div>
                  </div>
                ) : (
                  <>
                    {/* Result count */}
                    <div className="px-4 py-2 border-b border-border/30">
                      <p className="text-[11px] text-muted-foreground/50">
                        {results.length} result{results.length !== 1 ? "s" : ""}
                      </p>
                    </div>

                    {groupedResults.map((group) => (
                      <div key={group.label} className="py-1">
                        <p className="px-4 py-1.5 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/50 flex items-center gap-1.5">
                          <group.icon className="h-3 w-3" />
                          {group.label}
                        </p>
                        {group.emails.map((email) => {
                          const globalIdx = flatResults.indexOf(email);
                          const isActive = globalIdx === selectedIndex;
                          return (
                            <button
                              key={email.id}
                              onMouseEnter={() => setSelectedIndex(globalIdx)}
                              onClick={() => selectEmail(email.id)}
                              className={cn(
                                "flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors",
                                isActive ? "bg-accent" : "hover:bg-accent/30"
                              )}
                            >
                              <ContactAvatar contact={email.from} size="sm" className="shrink-0" />
                              <div className="flex-1 min-w-0">
                                <div className="flex items-center gap-2">
                                  <span className={cn("text-[13px] truncate", !email.read && "font-medium")}>
                                    <Highlight text={email.from.name} query={query} />
                                  </span>
                                  {email.attachments.length > 0 && (
                                    <Paperclip className="h-3 w-3 text-muted-foreground/40 shrink-0" />
                                  )}
                                  {email.starred && (
                                    <Star className="h-3 w-3 fill-amber-400/80 text-amber-400/80 shrink-0" />
                                  )}
                                  <span className="ml-auto text-[11px] text-muted-foreground/50 shrink-0">
                                    {formatDistanceToNowStrict(new Date(email.date), { addSuffix: false })}
                                  </span>
                                </div>
                                <p className="text-[12px] text-muted-foreground truncate">
                                  <Highlight text={email.subject} query={query} />
                                </p>
                                <p className="text-[11px] text-muted-foreground/40 truncate">
                                  <Highlight text={email.snippet} query={query} />
                                </p>
                              </div>
                            </button>
                          );
                        })}
                      </div>
                    ))}

                    {/* AI summary hint */}
                    {results.length > 0 && (
                      <div className="border-t border-border/30 px-4 py-3 flex items-center gap-2">
                        <Sparkles className="h-3.5 w-3.5 text-violet-400" />
                        <p className="text-[11px] text-muted-foreground/50">
                          <span className="text-violet-400">AI:</span> {results.length} email{results.length !== 1 ? "s" : ""} match{results.length === 1 ? "es" : ""} your search
                          {filters.some((f) => f.type === "from") && ` from ${filters.find((f) => f.type === "from")?.display}`}
                        </p>
                      </div>
                    )}
                  </>
                )}
              </>
            )}
          </div>

          {/* ── Footer ── */}
          <div className="flex items-center justify-between border-t border-border/30 px-4 py-2">
            <div className="flex items-center gap-3 text-[10px] text-muted-foreground/40">
              <span className="flex items-center gap-1">
                <kbd className="rounded border border-border/30 px-1 py-0.5 font-mono">↑↓</kbd>
                navigate
              </span>
              <span className="flex items-center gap-1">
                <kbd className="rounded border border-border/30 px-1 py-0.5 font-mono">↵</kbd>
                select
              </span>
              <span className="flex items-center gap-1">
                <kbd className="rounded border border-border/30 px-1 py-0.5 font-mono">tab</kbd>
                filter
              </span>
            </div>
            <div className="flex items-center gap-1 text-[10px] text-muted-foreground/30">
              <Search className="h-3 w-3" />
              Veil Search
            </div>
          </div>
        </motion.div>
      </motion.div>
      )}
    </AnimatePresence>
  );
}
