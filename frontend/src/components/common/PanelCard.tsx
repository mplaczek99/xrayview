import type { PropsWithChildren, ReactNode } from "react";

interface PanelCardProps extends PropsWithChildren {
  eyebrow?: string;
  title: string;
  description?: string;
  actions?: ReactNode;
  className?: string;
}

// Shared card chrome keeps headings, actions, and body spacing consistent
// across inspector-style panels without forcing each caller to rebuild it.
export function PanelCard({
  eyebrow,
  title,
  description,
  actions,
  className,
  children,
}: PanelCardProps) {
  return (
    <section className={`panel-card ${className ?? ""}`.trim()}>
      <header className="panel-card__header">
        <div>
          {eyebrow ? <div className="panel-card__eyebrow">{eyebrow}</div> : null}
          <h2 className="panel-card__title">{title}</h2>
          {description ? <p className="panel-card__description">{description}</p> : null}
        </div>
        {actions ? <div>{actions}</div> : null}
      </header>
      <div className="panel-card__body">{children}</div>
    </section>
  );
}
