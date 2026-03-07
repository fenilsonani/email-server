import type { Metadata } from "next";
import { DM_Sans, JetBrains_Mono } from "next/font/google";
import { Toaster } from "sonner";
import "./globals.css";

const sans = DM_Sans({
  subsets: ["latin"],
  variable: "--font-sans",
});

const mono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-geist-mono",
});

export const metadata: Metadata = {
  title: "Mail Server Admin",
  description: "Administration panel for the mail server",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className="dark">
      <body className={`${sans.variable} ${mono.variable} font-sans antialiased`}>
        {children}
        <div className="grain-overlay" aria-hidden="true" />
        <Toaster
          theme="dark"
          position="bottom-right"
          visibleToasts={3}
          toastOptions={{
            className: "text-[13px] !bg-card !border-border !text-foreground [&_[data-icon]]:text-foreground",
            duration: 3000,
          }}
        />
      </body>
    </html>
  );
}
