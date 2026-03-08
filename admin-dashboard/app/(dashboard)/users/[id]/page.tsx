import EditUserClient from "./edit-user-client";

// Required for static export — Go SPA catch-all serves this for all user IDs
export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function Page() {
  return <EditUserClient />;
}
