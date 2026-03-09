"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { Loader2 } from "lucide-react";

export function TwoFactorInput({
  onComplete,
  isLoading,
}: {
  onComplete: (code: string) => void;
  isLoading?: boolean;
}) {
  const [digits, setDigits] = useState<string[]>(Array(6).fill(""));
  const inputRefs = useRef<(HTMLInputElement | null)[]>([]);

  useEffect(() => {
    inputRefs.current[0]?.focus();
  }, []);

  const handleChange = useCallback(
    (index: number, value: string) => {
      if (!/^\d*$/.test(value)) return;

      const next = [...digits];
      next[index] = value.slice(-1);
      setDigits(next);

      if (value && index < 5) {
        inputRefs.current[index + 1]?.focus();
      }

      const code = next.join("");
      if (code.length === 6 && next.every((d) => d !== "")) {
        onComplete(code);
      }
    },
    [digits, onComplete]
  );

  const handleKeyDown = useCallback(
    (index: number, e: React.KeyboardEvent) => {
      if (e.key === "Backspace" && !digits[index] && index > 0) {
        inputRefs.current[index - 1]?.focus();
      }
    },
    [digits]
  );

  const handlePaste = useCallback(
    (e: React.ClipboardEvent) => {
      e.preventDefault();
      const pasted = e.clipboardData.getData("text").replace(/\D/g, "").slice(0, 6);
      if (!pasted) return;
      const next = Array(6).fill("");
      for (let i = 0; i < pasted.length; i++) next[i] = pasted[i];
      setDigits(next);
      if (pasted.length === 6) {
        onComplete(pasted);
      } else {
        inputRefs.current[pasted.length]?.focus();
      }
    },
    [onComplete]
  );

  return (
    <div className="flex flex-col items-center gap-4">
      <div className="flex gap-2" onPaste={handlePaste}>
        {digits.map((digit, i) => (
          <input
            key={i}
            ref={(el) => { inputRefs.current[i] = el; }}
            type="text"
            inputMode="numeric"
            maxLength={1}
            value={digit}
            onChange={(e) => handleChange(i, e.target.value)}
            onKeyDown={(e) => handleKeyDown(i, e)}
            disabled={isLoading}
            className="w-10 h-12 text-center text-lg font-medium rounded-md border border-border bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-indigo-500 disabled:opacity-50"
          />
        ))}
      </div>
      {isLoading && <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />}
    </div>
  );
}
