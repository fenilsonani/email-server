"use client";

import { useEffect, useState } from "react";
import { useRouteId } from "@/lib/use-route-id";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { PageShell } from "@/components/shared/page-shell";
import { DataTable } from "@/components/shared/data-table";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { formatDistanceToNow } from "date-fns";
import { ArrowLeft, Plus, Trash2, Loader2 } from "lucide-react";
import { toast } from "sonner";
import type { ColumnDef } from "@tanstack/react-table";

interface ListMember {
  id: number;
  email: string;
  role: "member" | "moderator" | "admin";
  joined_at: string;
}

const roleBadgeVariant = (role: string) => {
  switch (role) {
    case "admin":
      return "default" as const;
    case "moderator":
      return "outline" as const;
    default:
      return "outline" as const;
  }
};

function MembersContent() {
  const listId = useRouteId("lists");

  const [members, setMembers] = useState<ListMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [addEmail, setAddEmail] = useState("");
  const [addRole, setAddRole] = useState<"member" | "moderator" | "admin">("member");
  const [adding, setAdding] = useState(false);
  const [removingId, setRemovingId] = useState<number | null>(null);

  const fetchMembers = () => {
    setLoading(true);
    api.get<ListMember[]>(`/v1/lists/${listId}/members`).then((res) => {
      if (res.success && Array.isArray(res.data)) setMembers(res.data);
      setLoading(false);
    });
  };

  useEffect(() => {
    fetchMembers();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listId]);

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!addEmail.trim()) {
      toast.error("Email is required");
      return;
    }

    setAdding(true);
    const res = await api.post(`/v1/lists/${listId}/members`, {
      email: addEmail.trim(),
      role: addRole,
    });
    if (res.success) {
      toast.success(`Added ${addEmail} to the list`);
      setAddEmail("");
      setAddRole("member");
      setShowAdd(false);
      fetchMembers();
    } else {
      toast.error(res.error || "Failed to add member");
    }
    setAdding(false);
  };

  const handleRemove = async (member: ListMember) => {
    setRemovingId(member.id);
    const res = await api.delete(`/v1/lists/${listId}/members/${member.id}`);
    if (res.success) {
      toast.success(`Removed ${member.email} from the list`);
      setMembers((prev) => prev.filter((m) => m.id !== member.id));
    } else {
      toast.error(res.error || "Failed to remove member");
    }
    setRemovingId(null);
  };

  const columns: ColumnDef<ListMember, unknown>[] = [
    {
      accessorKey: "email",
      header: "Email",
      cell: ({ row }) => (
        <span className="font-mono text-[12px]">{row.original.email}</span>
      ),
    },
    {
      accessorKey: "role",
      header: "Role",
      cell: ({ row }) => (
        <Badge
          variant={roleBadgeVariant(row.original.role)}
          className="text-[10px]"
        >
          {row.original.role}
        </Badge>
      ),
    },
    {
      accessorKey: "joined_at",
      header: "Joined",
      cell: ({ row }) => (
        <span className="text-[12px] text-muted-foreground/60">
          {formatDistanceToNow(new Date(row.original.joined_at), {
            addSuffix: true,
          })}
        </span>
      ),
    },
    {
      id: "actions",
      enableSorting: false,
      cell: ({ row }) => (
        <Button
          variant="ghost"
          size="icon-xs"
          className="text-muted-foreground/50 hover:text-destructive"
          onClick={() => handleRemove(row.original)}
          disabled={removingId === row.original.id}
        >
          {removingId === row.original.id ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Trash2 className="h-3.5 w-3.5" />
          )}
        </Button>
      ),
    },
  ];

  return (
    <PageShell
      title="Members"
      description="Manage members of this mailing list"
      actions={
        <div className="flex items-center gap-2">
          <Link href={`/lists/${listId}/`}>
            <Button variant="ghost" size="icon-xs">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
          <Button
            size="sm"
            className="h-8 text-[12px] gap-1.5"
            onClick={() => setShowAdd(true)}
          >
            <Plus className="h-3.5 w-3.5" />
            Add Member
          </Button>
        </div>
      }
    >
      <DataTable
        columns={columns}
        data={members}
        loading={loading}
        searchKey="email"
        searchPlaceholder="Search members..."
        emptyMessage="No members found."
      />

      <Dialog open={showAdd} onOpenChange={setShowAdd}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Member</DialogTitle>
            <DialogDescription className="text-[13px]">
              Add a new member to this mailing list.
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleAdd} className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Email Address</Label>
              <Input
                type="email"
                value={addEmail}
                onChange={(e) => setAddEmail(e.target.value)}
                placeholder="user@example.com"
                className="text-[13px] font-mono"
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-[13px]">Role</Label>
              <select
                value={addRole}
                onChange={(e) =>
                  setAddRole(e.target.value as "member" | "moderator" | "admin")
                }
                className="h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-[13px] text-foreground outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
              >
                <option value="member">Member</option>
                <option value="moderator">Moderator</option>
                <option value="admin">Admin</option>
              </select>
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="text-[13px]"
                onClick={() => setShowAdd(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                className="text-[13px]"
                disabled={adding}
              >
                {adding && <Loader2 className="h-4 w-4 animate-spin" />}
                Add Member
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}

export default function MembersClient() {
  return (
    <AuthGuard>
      <MembersContent />
    </AuthGuard>
  );
}
