"use client";

import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import { useAuthStore } from "@/lib/auth-store";
import { api } from "@/lib/api";
import { AdminSidebar } from "./sidebar";
import { AdminHeader } from "./header";
import { Loader2, X } from "lucide-react";

export function AuthGuard({ children }: { children: React.ReactNode }) {
  const { authenticated, loading, checkSession } = useAuthStore();
  const [setupChecked, setSetupChecked] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const pathname = usePathname();

  // Close mobile menu on navigation
  useEffect(() => {
    setMobileMenuOpen(false);
  }, [pathname]);

  // Check setup status first — redirect to wizard before anything else
  useEffect(() => {
    api.get<{ needs_setup: boolean }>("/v1/setup/status").then((res) => {
      if (res.success && res.data?.needs_setup) {
        window.location.href = "/admin/setup/";
        return;
      }
      setSetupChecked(true);
    }).catch(() => setSetupChecked(true));
  }, []);

  useEffect(() => {
    if (setupChecked) checkSession();
  }, [setupChecked, checkSession]);

  useEffect(() => {
    if (setupChecked && !loading && !authenticated) {
      window.location.href = "/admin/login/";
    }
  }, [setupChecked, loading, authenticated]);

  if (!setupChecked || loading || !authenticated) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground/50" />
      </div>
    );
  }

  return (
    <div className="flex h-screen bg-background">
      {/* Skip to content — accessibility */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:z-[100] focus:top-2 focus:left-2 focus:px-3 focus:py-1.5 focus:rounded-md focus:bg-primary focus:text-primary-foreground focus:text-[13px] focus:font-medium"
      >
        Skip to content
      </a>

      {/* Desktop sidebar */}
      <AdminSidebar />

      {/* Mobile sidebar overlay */}
      {mobileMenuOpen && (
        <>
          <div
            className="fixed inset-0 z-40 bg-black/50 md:hidden"
            onClick={() => setMobileMenuOpen(false)}
          />
          <div className="fixed inset-y-0 left-0 z-50 w-52 md:hidden animate-in slide-in-from-left duration-200">
            <AdminSidebar mobile />
            <button
              onClick={() => setMobileMenuOpen(false)}
              className="absolute top-2 right-2 h-7 w-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors"
              aria-label="Close navigation menu"
            >
              <X className="h-4 w-4" strokeWidth={1.5} />
            </button>
          </div>
        </>
      )}

      <div className="flex flex-1 flex-col min-w-0">
        <AdminHeader onMenuToggle={() => setMobileMenuOpen(true)} />
        <main id="main-content" className="flex-1 overflow-auto">
          <div className="animate-in fade-in duration-150">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
