"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import type { MailingList } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatDistanceToNow } from "date-fns";
import { Plus, Users, MoreHorizontal } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { ColumnDef } from "@tanstack/react-table";

const columns: ColumnDef<MailingList, unknown>[] = [
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => (
      <Link href={`/lists/${row.original.id}/`} className="text-[13px] font-medium hover:underline">
        {row.original.name}
      </Link>
    ),
  },
  {
    accessorKey: "address",
    header: "Address",
    cell: ({ row }) => (
      <span className="text-[13px] font-mono text-muted-foreground">{row.original.address}</span>
    ),
  },
  {
    accessorKey: "is_active",
    header: "Status",
    cell: ({ row }) => (
      <Badge
        variant={row.original.is_active ? "default" : "outline"}
        className="text-[10px]"
      >
        {row.original.is_active ? "Active" : "Inactive"}
      </Badge>
    ),
  },
  {
    accessorKey: "member_count",
    header: "Members",
    cell: ({ row }) => (
      <span className="inline-flex items-center gap-1 text-[13px] tabular-nums">
        <Users className="h-3 w-3 text-muted-foreground/50" />
        {row.original.member_count}
      </span>
    ),
  },
  {
    accessorKey: "pending_moderation",
    header: "Pending",
    cell: ({ row }) =>
      row.original.pending_moderation > 0 ? (
        <Badge variant="destructive" className="text-[10px]">
          {row.original.pending_moderation}
        </Badge>
      ) : (
        <span className="text-[13px] text-muted-foreground/50">0</span>
      ),
  },
  {
    accessorKey: "created_at",
    header: "Created",
    cell: ({ row }) => (
      <span className="text-[12px] text-muted-foreground/60">
        {formatDistanceToNow(new Date(row.original.created_at), { addSuffix: true })}
      </span>
    ),
  },
  {
    id: "actions",
    enableSorting: false,
    cell: ({ row }) => (
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 hover:bg-accent hover:text-foreground transition-colors" />
          }
        >
          <MoreHorizontal className="h-3.5 w-3.5" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem className="text-[13px]">
            <Link href={`/lists/${row.original.id}/members/`} className="w-full">Members</Link>
          </DropdownMenuItem>
          <DropdownMenuItem className="text-[13px]">
            <Link href={`/lists/${row.original.id}/`} className="w-full">Edit</Link>
          </DropdownMenuItem>
          <DropdownMenuItem className="text-[13px] text-destructive">Delete</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    ),
  },
];

function PageContent() {
  const [lists, setLists] = useState<MailingList[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get<MailingList[]>("/v1/lists").then((res) => {
      if (res.success && Array.isArray(res.data)) setLists(res.data);
      setLoading(false);
    });
  }, []);

  return (
    <PageShell
      title="Mailing Lists"
      description="Manage mailing lists and subscribers"
      actions={
        <Link href="/lists/new/">
          <Button size="sm" className="h-8 text-[12px] gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            New List
          </Button>
        </Link>
      }
    >
      <DataTable
        columns={columns}
        data={lists}
        loading={loading}
        searchKey="name"
        searchPlaceholder="Search lists..."
        emptyMessage="No mailing lists found."
      />
    </PageShell>
  );
}

export default function Page() {
  return <AuthGuard><PageContent /></AuthGuard>;
}
