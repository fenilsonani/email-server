import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import type { Contact } from "@/lib/types";
import { cn } from "@/lib/utils";

function getInitials(name: string) {
  return name
    .split(" ")
    .map((n) => n[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

const colors = [
  "bg-blue-500/20 text-blue-400",
  "bg-emerald-500/20 text-emerald-400",
  "bg-amber-500/20 text-amber-400",
  "bg-rose-500/20 text-rose-400",
  "bg-purple-500/20 text-purple-400",
  "bg-cyan-500/20 text-cyan-400",
];

function colorForName(name: string) {
  let hash = 0;
  for (let i = 0; i < name.length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash);
  return colors[Math.abs(hash) % colors.length];
}

export function ContactAvatar({
  contact,
  size = "md",
  className,
}: {
  contact: Contact;
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  const sizeClass = size === "sm" ? "h-6 w-6 text-[10px]" : size === "lg" ? "h-10 w-10 text-sm" : "h-8 w-8 text-xs";
  return (
    <Avatar className={cn(sizeClass, className)}>
      <AvatarFallback className={cn(sizeClass, colorForName(contact.name))}>
        {getInitials(contact.name)}
      </AvatarFallback>
    </Avatar>
  );
}

export function AvatarStack({ contacts, max = 3 }: { contacts: Contact[]; max?: number }) {
  const shown = contacts.slice(0, max);
  const overflow = contacts.length - max;
  return (
    <div className="flex -space-x-2">
      {shown.map((c, i) => (
        <ContactAvatar key={c.email + i} contact={c} size="sm" className="ring-2 ring-background" />
      ))}
      {overflow > 0 && (
        <div className="flex h-6 w-6 items-center justify-center rounded-full bg-muted text-[10px] font-medium text-muted-foreground ring-2 ring-background">
          +{overflow}
        </div>
      )}
    </div>
  );
}
