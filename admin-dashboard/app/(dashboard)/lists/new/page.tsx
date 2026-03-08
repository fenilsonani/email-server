"use client";

import { useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ArrowLeft, Loader2 } from "lucide-react";
import { toast } from "sonner";

function CreateListContent() {
  const [loading, setLoading] = useState(false);

  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [description, setDescription] = useState("");

  const [errors, setErrors] = useState<Record<string, string>>({});

  const validate = () => {
    const newErrors: Record<string, string> = {};
    if (!name.trim()) newErrors.name = "Name is required";
    if (!address.trim()) newErrors.address = "Address is required";
    else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(address))
      newErrors.address = "Please enter a valid email address";
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!validate()) return;

    setLoading(true);
    const res = await api.post("/v1/lists", {
      name: name.trim(),
      address: address.trim(),
      description: description.trim(),
    });

    if (res.success) {
      toast.success("Mailing list created successfully");
      window.location.href = "/admin/lists/";
    } else {
      toast.error(res.error || "Failed to create mailing list");
    }
    setLoading(false);
  };

  return (
    <div className="space-y-4 max-w-lg">
      <div className="flex items-center gap-3">
        <Link href="/lists/">
          <Button variant="ghost" size="icon-xs">
            <ArrowLeft className="h-4 w-4" />
          </Button>
        </Link>
        <div>
          <h1 className="text-lg font-semibold">Create Mailing List</h1>
          <p className="text-[13px] text-muted-foreground mt-0.5">
            Add a new mailing list
          </p>
        </div>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium">
            List Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-[13px]">Name</Label>
              <Input
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  if (errors.name)
                    setErrors((prev) => ({ ...prev, name: "" }));
                }}
                placeholder="Engineering Updates"
                className="text-[13px]"
                aria-invalid={!!errors.name}
              />
              {errors.name && (
                <p className="text-[12px] text-destructive">{errors.name}</p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label className="text-[13px]">Address</Label>
              <Input
                type="email"
                value={address}
                onChange={(e) => {
                  setAddress(e.target.value);
                  if (errors.address)
                    setErrors((prev) => ({ ...prev, address: "" }));
                }}
                placeholder="engineering@example.com"
                className="text-[13px] font-mono"
                aria-invalid={!!errors.address}
              />
              {errors.address && (
                <p className="text-[12px] text-destructive">
                  {errors.address}
                </p>
              )}
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

            <div className="flex items-center gap-2 pt-2">
              <Button
                type="submit"
                size="sm"
                className="text-[13px]"
                disabled={loading}
              >
                {loading && <Loader2 className="h-4 w-4 animate-spin" />}
                Create List
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
    </div>
  );
}

export default function Page() {
  return (
      <CreateListContent />
  );
}
