import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Reposhift",
  description: "ADO to GitHub migration dashboard",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-zinc-950 text-zinc-50 font-sans antialiased">
        {children}
      </body>
    </html>
  );
}
