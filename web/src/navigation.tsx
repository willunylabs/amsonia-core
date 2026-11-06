import type { ReactNode } from "react";
import { useConsoleRoute } from "./routing";

export function PageNavigation({
  label,
  items,
}: {
  label: string;
  items: Array<{ path: string; label: string }>;
}) {
  const { path } = useConsoleRoute();
  return (
    <nav className="page-navigation" aria-label={label}>
      {items.map((item) => (
        <a
          key={item.path}
          href={`#/${item.path}`}
          aria-current={
            path === item.path || path.startsWith(`${item.path}/`)
              ? "page"
              : undefined
          }
        >
          {item.label}
        </a>
      ))}
    </nav>
  );
}

export function FormPage({
  title,
  detail,
  back,
  children,
}: {
  title: string;
  detail: string;
  back: { path: string; label: string };
  children: ReactNode;
}) {
  return (
    <>
      <a className="back-link" href={`#/${back.path}`}>
        ← {back.label}
      </a>
      <header className="page-header">
        <div>
          <span className="eyebrow">Administration</span>
          <h1>{title}</h1>
          <p>{detail}</p>
        </div>
      </header>
      <section className="paper-card form-page">{children}</section>
    </>
  );
}
