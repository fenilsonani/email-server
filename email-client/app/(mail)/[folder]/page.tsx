"use client";

import { useMailStore } from "@/lib/store";
import { useParams } from "next/navigation";
import { useEffect } from "react";
import type { FolderSlug } from "@/lib/types";

const validFolders: FolderSlug[] = ["inbox", "starred", "sent", "drafts", "archive", "trash"];

export default function FolderPage() {
  const params = useParams();
  const setActiveFolder = useMailStore((s) => s.setActiveFolder);

  useEffect(() => {
    const folder = params.folder as string;
    if (validFolders.includes(folder as FolderSlug)) {
      setActiveFolder(folder as FolderSlug);
    }
  }, [params.folder, setActiveFolder]);

  return null;
}
