import DomainDetailClient from "./domain-detail-client";

// Required for static export — Go SPA catch-all serves this for all domain IDs
export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function Page() {
  return <DomainDetailClient />;
}
