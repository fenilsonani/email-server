"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { AuthGuard } from "@/components/layout/auth-guard";
import { api } from "@/lib/api";
import type { User } from "@/lib/types";
import { PageShell } from "@/components/shared/page-shell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { ArrowLeft, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

function EditUserContent() {
  const params = useParams();
  const router = useRouter();
  const userId = params.id as string;

  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [showDelete, setShowDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    api.get<User>(`/v1/users/${userId}`).then((res) => {
      if (res.success && res.data) {
        setUser(res.data);
        setIsAdmin(res.data.is_admin);
      } else {
        toast.error("User not found");
        router.push("/users/");
      }
      setLoading(false);
    });
  }, [userId, router]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    const newErrors: Record<string, string> = {};
    if (password && password.length < 8)
      newErrors.password = "Password must be at least 8 characters";
    setErrors(newErrors);
    if (Object.keys(newErrors).length > 0) return;

    setSaving(true);
    const body: Record<string, unknown> = { is_admin: isAdmin };
    if (password) body.password = password;

    const res = await api.put(`/v1/users/${userId}`, body);
    if (res.success) {
      toast.success("User updated successfully");
      setPassword("");
    } else {
      toast.error(res.error || "Failed to update user");
    }
    setSaving(false);
  };

  const handleDelete = async () => {
    setDeleting(true);
    const res = await api.delete(`/v1/users/${userId}`);
    if (res.success) {
      toast.success(`User ${user?.email} deleted`);
      router.push("/users/");
    } else {
      toast.error(res.error || "Failed to delete user");
    }
    setDeleting(false);
  };

  if (loading) {
    return (
      <PageShell title="Edit User">
        <div className="max-w-lg space-y-4">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-64 w-full" />
        </div>
      </PageShell>
    );
  }

  if (!user) return null;

  return (
    <PageShell
      title="Edit User"
      description={user.email}
      actions={
        user.is_admin ? <Badge variant="default" className="text-[10px]">Admin</Badge> : undefined
      }
    >
      <div className="max-w-lg">
        <Link href="/users/" className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground/60 hover:text-foreground transition-colors mb-4">
          <ArrowLeft className="h-3.5 w-3.5" />Back to Users
        </Link>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium">
            Account Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSave} className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Email Address</Label>
              <Input
                value={user.email}
                disabled
                className="text-[13px] opacity-60"
              />
              <p className="text-[12px] text-muted-foreground">
                Email address cannot be changed
              </p>
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px]">New Password</Label>
              <Input
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  if (errors.password)
                    setErrors((prev) => ({ ...prev, password: "" }));
                }}
                placeholder="Leave empty to keep current"
                className="text-[13px]"
                aria-invalid={!!errors.password}
              />
              {errors.password && (
                <p className="text-[12px] text-destructive">
                  {errors.password}
                </p>
              )}
            </div>

            <div className="flex items-center justify-between py-1">
              <div>
                <Label className="text-[13px]">Administrator</Label>
                <p className="text-[12px] text-muted-foreground mt-0.5">
                  Admins can manage users, domains, and server settings
                </p>
              </div>
              <Switch
                checked={isAdmin}
                onCheckedChange={setIsAdmin}
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px] text-muted-foreground">
                Created
              </Label>
              <p className="text-[13px]">
                {formatDistanceToNow(new Date(user.created_at), {
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
              <Link href="/users/">
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
              <p className="text-[13px] font-medium">Delete this user</p>
              <p className="text-[12px] text-muted-foreground mt-0.5">
                Permanently remove this account and all associated data
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
            <DialogTitle>Delete User</DialogTitle>
            <DialogDescription className="text-[13px]">
              Are you sure you want to delete <strong>{user.email}</strong>? This
              action cannot be undone.
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
              {deleting ? "Deleting..." : "Delete User"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}

export default function EditUserClient() {
  return (
    <AuthGuard>
      <EditUserContent />
    </AuthGuard>
  );
}
