"use client";

import { useAuthStore } from "@/lib/auth-store";
import { LogOut, CircleUser } from "lucide-react";

export function AdminHeader() {
  const { username, email, logout } = useAuthStore();

  const handleLogout = async () => {
    await logout();
    window.location.href = "/admin/login/";
  };

  return (
    <header className="flex h-12 items-center justify-between border-b border-border px-4 shrink-0">
      <div />
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-1.5 text-[12px] text-muted-foreground">
          <CircleUser className="h-3.5 w-3.5" strokeWidth={1.5} />
          <span>{email || username || "admin"}</span>
        </div>
        <div className="h-3 w-px bg-border" />
        <button
          onClick={handleLogout}
          className="flex items-center gap-1 rounded-md px-1.5 py-1 text-[12px] text-muted-foreground/70 transition-colors duration-100 hover:text-foreground hover:bg-accent/50"
        >
          <LogOut className="h-3 w-3" strokeWidth={1.5} />
          <span>Logout</span>
        </button>
      </div>
    </header>
  );
}
