"use client";

import { useMailStore } from "@/lib/store";
import { useParams } from "next/navigation";
import { useEffect } from "react";
import type { FolderSlug } from "@/lib/types";

export default function EmailPage() {
  const params = useParams();
  const setActiveFolder = useMailStore((s) => s.setActiveFolder);
  const setSelectedEmailId = useMailStore((s) => s.setSelectedEmailId);

  useEffect(() => {
    const folder = params.folder as string;
    const id = params.id as string;
    setActiveFolder(folder as FolderSlug);
    setSelectedEmailId(id);
  }, [params.folder, params.id, setActiveFolder, setSelectedEmailId]);

  return null;
}
