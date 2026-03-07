"use client";

import { useState, useRef } from "react";
import type { Contact } from "@/lib/types";
import { allContacts } from "@/lib/mock-data";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

export function RecipientInput({
  label,
  contacts,
  onChange,
}: {
  label: string;
  contacts: Contact[];
  onChange: (contacts: Contact[]) => void;
}) {
  const [query, setQuery] = useState("");
  const [showSuggestions, setShowSuggestions] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const suggestions = query
    ? allContacts.filter(
        (c) =>
          (c.name.toLowerCase().includes(query.toLowerCase()) ||
            c.email.toLowerCase().includes(query.toLowerCase())) &&
          !contacts.some((existing) => existing.email === c.email)
      )
    : [];

  const addContact = (contact: Contact) => {
    onChange([...contacts, contact]);
    setQuery("");
    setShowSuggestions(false);
    inputRef.current?.focus();
  };

  const removeContact = (email: string) => {
    onChange(contacts.filter((c) => c.email !== email));
  };

  return (
    <div className="relative flex items-center gap-2">
      <span className="text-xs text-muted-foreground shrink-0 w-6">{label}</span>
      <div className="flex flex-1 flex-wrap items-center gap-1">
        {contacts.map((contact) => (
          <span
            key={contact.email}
            className="flex items-center gap-1 rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary"
          >
            {contact.name}
            <button
              onClick={() => removeContact(contact.email)}
              className="text-primary/60 hover:text-primary"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setShowSuggestions(true);
          }}
          onFocus={() => query && setShowSuggestions(true)}
          onBlur={() => setTimeout(() => setShowSuggestions(false), 150)}
          placeholder={contacts.length === 0 ? "Add recipients..." : ""}
          className="flex-1 min-w-[120px] bg-transparent text-sm focus:outline-none placeholder:text-muted-foreground"
        />
      </div>

      {/* Autocomplete dropdown */}
      {showSuggestions && suggestions.length > 0 && (
        <div className="absolute top-full left-6 z-50 mt-1 w-64 rounded-lg border border-border bg-popover p-1 shadow-lg">
          {suggestions.slice(0, 5).map((contact) => (
            <button
              key={contact.email}
              onClick={() => addContact(contact)}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
            >
              <div>
                <p className="font-medium">{contact.name}</p>
                <p className="text-xs text-muted-foreground">{contact.email}</p>
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
