import type { ReactNode } from "react";

/** A titled group of settings rows. */
export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h2 className="px-1 text-xs font-medium text-muted-foreground">{title}</h2>
      <div className="divide-y rounded-lg border bg-card">{children}</div>
    </section>
  );
}

/** One setting: a label and description on the left, its control on the right. */
export function Row({
  title,
  description,
  note,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  /** Shown under the description in the destructive colour, for errors. */
  note?: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex min-h-14 items-center gap-4 px-4 py-3">
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="flex items-center gap-2 font-medium">{title}</div>
        {description && <p className="text-[13px] text-muted-foreground">{description}</p>}
        {note && <p className="text-[13px] text-destructive">{note}</p>}
      </div>
      {children && <div className="flex shrink-0 items-center gap-1.5">{children}</div>}
    </div>
  );
}

/** Placeholder content for a section with nothing to list. */
export function Empty({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed px-6 py-12 text-center">
      <div className="text-muted-foreground [&_svg]:size-8">{icon}</div>
      <p className="font-medium">{title}</p>
      <p className="max-w-sm text-[13px] text-muted-foreground">{children}</p>
    </div>
  );
}
