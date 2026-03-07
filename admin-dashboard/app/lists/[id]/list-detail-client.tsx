"use client";

import { useEffect, useState } from "react";
import { useRouteId } from "@/lib/use-route-id";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { api } from "@/lib/api";
import type { MailingList } from "@/lib/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { ArrowLeft, Loader2, Trash2, Users, Mail, Archive } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

function ListDetailContent() {
  const listId = useRouteId("lists");

  const [list, setList] = useState<MailingList | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [showDelete, setShowDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [isActive, setIsActive] = useState(true);

  useEffect(() => {
    api.get<MailingList>(`/v1/lists/${listId}`).then((res) => {
      if (res.success && res.data) {
        setList(res.data);
        setName(res.data.name);
        setDescription(res.data.description);
        setIsActive(res.data.is_active);
      } else {
        toast.error("Mailing list not found");
        window.location.href = "/admin/lists/";
      }
      setLoading(false);
    });
  }, [listId]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      toast.error("Name is required");
      return;
    }

    setSaving(true);
    const res = await api.put(`/v1/lists/${listId}`, {
      name: name.trim(),
      description: description.trim(),
      is_active: isActive,
    });
    if (res.success) {
      toast.success("Mailing list updated successfully");
    } else {
      toast.error(res.error || "Failed to update mailing list");
    }
    setSaving(false);
  };

  const handleDelete = async () => {
    setDeleting(true);
    const res = await api.delete(`/v1/lists/${listId}`);
    if (res.success) {
      toast.success(`Mailing list "${list?.name}" deleted`);
      window.location.href = "/admin/lists/";
    } else {
      toast.error(res.error || "Failed to delete mailing list");
    }
    setDeleting(false);
  };

  if (loading) {
    return (
      <div className="space-y-4 max-w-lg">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!list) return null;

  return (
    <div className="space-y-4 max-w-lg">
      <div className="flex items-center gap-3">
        <Link href="/lists/">
          <Button variant="ghost" size="icon-xs">
            <ArrowLeft className="h-4 w-4" />
          </Button>
        </Link>
        <div className="flex-1">
          <h1 className="text-lg font-semibold">Edit Mailing List</h1>
          <p className="text-[12px] font-mono text-muted-foreground mt-0.5">
            {list.address}
          </p>
        </div>
        <Badge
          variant={list.is_active ? "default" : "outline"}
          className="text-[10px]"
        >
          {list.is_active ? "Active" : "Inactive"}
        </Badge>
      </div>

      {/* Quick links */}
      <div className="flex items-center gap-2">
        <Link href={`/lists/${listId}/members/`}>
          <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5">
            <Users className="h-3.5 w-3.5" />
            Members ({list.member_count})
          </Button>
        </Link>
        <Link href={`/lists/${listId}/moderation/`}>
          <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5">
            <Mail className="h-3.5 w-3.5" />
            Moderation
            {list.pending_moderation > 0 && (
              <Badge variant="destructive" className="text-[10px] ml-1">
                {list.pending_moderation}
              </Badge>
            )}
          </Button>
        </Link>
        <Link href={`/lists/${listId}/archives/`}>
          <Button variant="outline" size="sm" className="h-8 text-[12px] gap-1.5">
            <Archive className="h-3.5 w-3.5" />
            Archives
          </Button>
        </Link>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium">
            List Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSave} className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Address</Label>
              <Input
                value={list.address}
                disabled
                className="text-[13px] font-mono opacity-60"
              />
              <p className="text-[12px] text-muted-foreground">
                List address cannot be changed
              </p>
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px]">Name</Label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="List name"
                className="text-[13px]"
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px]">Description</Label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="A brief description of this mailing list"
                className="text-[13px] min-h-[80px]"
              />
            </div>

            <div className="flex items-center justify-between py-1">
              <div>
                <Label className="text-[13px]">Active</Label>
                <p className="text-[12px] text-muted-foreground mt-0.5">
                  Inactive lists will not accept or deliver messages
                </p>
              </div>
              <Switch checked={isActive} onCheckedChange={setIsActive} />
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px] text-muted-foreground">
                Created
              </Label>
              <p className="text-[13px]">
                {formatDistanceToNow(new Date(list.created_at), {
                  addSuffix: true,
                })}
              </p>
            </div>

            <div className="flex items-center gap-2 pt-2">
              <Button
                type="submit"
                size="sm"
                className="text-[13px]"
                disabled={saving}
              >
                {saving && <Loader2 className="h-4 w-4 animate-spin" />}
                Save Changes
              </Button>
              <Link href="/lists/">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="text-[13px]"
                >
                  Cancel
                </Button>
              </Link>
            </div>
          </form>
        </CardContent>
      </Card>

      <Separator />

      <Card className="border-destructive/30">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium text-destructive">
            Danger Zone
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-[13px] font-medium">Delete this mailing list</p>
              <p className="text-[12px] text-muted-foreground mt-0.5">
                Permanently remove this list, all members, and archived messages
              </p>
            </div>
            <Button
              variant="destructive"
              size="sm"
              className="text-[13px]"
              onClick={() => setShowDelete(true)}
            >
              <Trash2 className="h-4 w-4" />
              Delete
            </Button>
          </div>
        </CardContent>
      </Card>

      <Dialog open={showDelete} onOpenChange={setShowDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Mailing List</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to delete <strong>{list.name}</strong> (
              <span className="font-mono">{list.address}</span>)? This action
              cannot be undone and will remove all members and archived messages.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              className="text-[13px]"
              onClick={() => setShowDelete(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              className="text-[13px]"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting ? "Deleting..." : "Delete List"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export default function ListDetailClient() {
  return (
    <AuthGuard>
      <ListDetailContent />
    </AuthGuard>
  );
}
