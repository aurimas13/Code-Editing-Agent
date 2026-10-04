import type { Metadata, Viewport } from "next";
import "@fontsource-variable/bricolage-grotesque";
import "@fontsource-variable/figtree";
import "@fontsource-variable/jetbrains-mono";
import "./globals.css";
import { SiteFooter, SiteHeader } from "@/components/Chrome";
import { site } from "@/lib/site";

export const metadata: Metadata = {
  metadataBase: new URL(site.url),
  title: { default: `${site.name}: ${site.tagline}`, template: `%s · ${site.name}` },
  description:
    "A working code-editing agent in Go: an LLM, a loop and three tools. Try it in a sandbox, watch every step, and build it yourself with a guide that shows exactly where each line goes.",
  authors: [{ name: site.author, url: site.authorUrl }],
  openGraph: {
    type: "website",
    siteName: site.name,
    title: `${site.name}: ${site.tagline}`,
    description: "Try a sandboxed code-editing agent, watch every step it takes, and build it yourself line by line.",
  },
};

export const viewport: Viewport = { themeColor: "#F9FBE7", colorScheme: "light" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <a className="skip" href="#main">
          Skip to content
        </a>
        <SiteHeader />
        <main id="main">{children}</main>
        <SiteFooter />
      </body>
    </html>
  );
}
