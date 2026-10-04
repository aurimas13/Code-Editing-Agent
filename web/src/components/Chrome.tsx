import Link from "next/link";
import { site } from "@/lib/site";

const NAV = [
  { href: "/playground", label: "Playground" },
  { href: "/guide", label: "Build guide" },
  { href: "/research", label: "Research" },
  { href: "/evals", label: "Evals" },
  { href: "/architecture", label: "Architecture" },
];

export function SiteHeader() {
  return (
    <header className="site-header">
      <div className="wrap site-header-row">
        <Link href="/" className="brand" aria-label={`${site.name} home`}>
          <svg className="brand-mark" viewBox="0 0 32 32" aria-hidden="true">
            <path d="M16 5a11 11 0 1 0 11 11" fill="none" strokeWidth="3.5" strokeLinecap="round" style={{ stroke: "var(--ink)" }} />
            <circle cx="16" cy="5" r="4" style={{ fill: "var(--sun)" }} />
            <circle cx="27" cy="16" r="4" style={{ fill: "var(--leaf)" }} />
          </svg>
          <span>Code-Editing Agent</span>
        </Link>
        <nav className="site-nav" aria-label="Main">
          {NAV.map((item) => (
            <Link key={item.href} href={item.href}>
              {item.label}
            </Link>
          ))}
          <a href={site.repo} className="site-nav-repo" target="_blank" rel="noopener noreferrer">
            GitHub
          </a>
        </nav>
      </div>
    </header>
  );
}

export function SiteFooter() {
  return (
    <footer className="site-footer">
      <div className="wrap site-footer-row">
        <p>
          Built by <a href={site.authorUrl}>{site.author}</a>. The core loop follows{" "}
          <a href={site.tutorial} target="_blank" rel="noopener noreferrer">
            “How to Build an Agent”
          </a>{" "}
          by Thorsten Ball; everything around it is documented in the{" "}
          <a href={site.repo} target="_blank" rel="noopener noreferrer">
            repository
          </a>
          .
        </p>
        <p className="site-footer-meta">Go · Next.js · Supabase · Railway · Vercel</p>
      </div>
    </footer>
  );
}
