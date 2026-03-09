import ModerationClient from "./moderation-client";

// Required for static export — Go SPA catch-all serves this for all list IDs
export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function Page() {
  return <ModerationClient />;
}
