"use client";

import { useState } from "react";
import { PageShell } from "@/components/shared/page-shell";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Send, Loader2 } from "lucide-react";
import { toast } from "sonner";

function PageContent() {
  const [to, setTo] = useState("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [sending, setSending] = useState(false);

  const handleSend = async () => {
    if (!to.trim() || !subject.trim()) {
      toast.error("To address and subject are required");
      return;
    }
    setSending(true);
    try {
      const res = await api.post("/v1/tools/test-email", { to: to.trim(), subject: subject.trim(), body: body.trim() });
      if (res.success) {
        toast.success("Test email sent");
        setTo(""); setSubject(""); setBody("");
      } else {
        toast.error(res.error || "Failed to send");
      }
    } catch {
      toast.error("Failed to send test email");
    } finally {
      setSending(false);
    }
  };

  return (
    <PageShell title="Test Email" description="Send a test email to verify delivery">
      <div className="rounded-lg border border-border bg-card p-4 max-w-md space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="to" className="text-[12px] text-muted-foreground font-normal">To</Label>
          <Input
            id="to"
            type="email"
            placeholder="recipient@example.com"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className="h-8 text-[13px] bg-background/50 border-border placeholder:text-muted-foreground/40"
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="subject" className="text-[12px] text-muted-foreground font-normal">Subject</Label>
          <Input
            id="subject"
            placeholder="Test email from admin"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            className="h-8 text-[13px] bg-background/50 border-border placeholder:text-muted-foreground/40"
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="body" className="text-[12px] text-muted-foreground font-normal">Body</Label>
          <Textarea
            id="body"
            placeholder="This is a test email."
            value={body}
            onChange={(e) => setBody(e.target.value)}
            rows={5}
            className="text-[13px] resize-none bg-background/50 border-border placeholder:text-muted-foreground/40"
          />
        </div>
        <Button onClick={handleSend} disabled={sending} size="sm" className="h-8 text-[12px] gap-1.5 w-full">
          {sending ? (
            <><Loader2 className="h-3.5 w-3.5 animate-spin" />Sending...</>
          ) : (
            <><Send className="h-3.5 w-3.5" />Send Test Email</>
          )}
        </Button>
      </div>
    </PageShell>
  );
}

export default function Page() {
  return <PageContent />;
}
