"use client";

import { useEditor, EditorContent } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Link from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import { AiCompose } from "@/components/mail/ai/ai-compose";
import { Button } from "@/components/ui/button";
import { Bold, Italic, Underline, List, ListOrdered, Link2, Code, Minus } from "lucide-react";
import { cn } from "@/lib/utils";
import { Separator } from "@/components/ui/separator";

export function ComposeEditor({
  content,
  onChange,
}: {
  content: string;
  onChange: (content: string) => void;
}) {
  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit.configure({
        heading: false,
      }),
      Link.configure({
        openOnClick: false,
      }),
      Placeholder.configure({
        placeholder: "Write your message...",
      }),
    ],
    content,
    onUpdate: ({ editor }) => {
      onChange(editor.getHTML());
    },
    editorProps: {
      attributes: {
        class: "prose prose-sm max-w-none px-4 py-3 min-h-[200px] focus:outline-none text-sm",
      },
    },
  });

  if (!editor) return null;

  const tools = [
    { icon: Bold, action: () => editor.chain().focus().toggleBold().run(), active: editor.isActive("bold") },
    { icon: Italic, action: () => editor.chain().focus().toggleItalic().run(), active: editor.isActive("italic") },
    { icon: Code, action: () => editor.chain().focus().toggleCode().run(), active: editor.isActive("code") },
    { icon: List, action: () => editor.chain().focus().toggleBulletList().run(), active: editor.isActive("bulletList") },
    { icon: ListOrdered, action: () => editor.chain().focus().toggleOrderedList().run(), active: editor.isActive("orderedList") },
    { icon: Minus, action: () => editor.chain().focus().setHorizontalRule().run(), active: false },
  ];

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center gap-1 border-b border-border px-4 py-1.5">
        {tools.map(({ icon: Icon, action, active }, i) => (
          <Button
            key={i}
            variant="ghost"
            size="icon"
            onClick={action}
            className={cn("h-7 w-7", active && "bg-accent")}
          >
            <Icon className="h-3.5 w-3.5" />
          </Button>
        ))}
        <Separator orientation="vertical" className="mx-1 h-4" />
        <AiCompose
          onInsert={(text) => {
            editor.chain().focus().insertContent(text).run();
          }}
        />
      </div>

      {/* Editor */}
      <EditorContent editor={editor} className="flex-1 overflow-y-auto" />
    </div>
  );
}
