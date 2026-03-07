"use client";

import { useMemo } from "react";

/**
 * Extract a dynamic route parameter from the browser URL.
 * In static export mode, useParams() returns the pre-rendered placeholder ("_")
 * instead of the actual URL value, so we parse window.location.pathname directly.
 *
 * @param segment - The path segment before the dynamic param (e.g. "users", "domains", "lists")
 * @returns The actual ID from the URL
 */
export function useRouteId(segment: string): string {
  return useMemo(() => {
    if (typeof window === "undefined") return "_";
    // URL: /admin/users/123/ → extract "123"
    // URL: /admin/lists/5/members/ → for segment "lists", extract "5"
    const path = window.location.pathname.replace(/\/$/, "");
    const parts = path.split("/");
    const idx = parts.findIndex((p) => p === segment);
    if (idx >= 0 && idx + 1 < parts.length) {
      return parts[idx + 1];
    }
    return "_";
  }, [segment]);
}
