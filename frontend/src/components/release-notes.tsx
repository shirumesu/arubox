import type { ReactNode } from "react";

/**
 * Renders release notes. They follow the CHANGELOG.md convention: "##" and
 * "###" headings over "- " bullets, one line each; any other line is a
 * paragraph.
 */
export function ReleaseNotes({ notes }: { notes: string }) {
  const blocks: ReactNode[] = [];
  let bullets: string[] = [];
  const endList = () => {
    if (bullets.length === 0) return;
    blocks.push(
      <ul key={blocks.length} className="list-disc space-y-1 pl-5">
        {bullets.map((b, i) => (
          <li key={i}>{b}</li>
        ))}
      </ul>,
    );
    bullets = [];
  };
  for (const line of notes.split("\n")) {
    const text = line.trim();
    if (text.startsWith("- ")) {
      bullets.push(text.slice(2));
      continue;
    }
    endList();
    if (text === "") continue;
    const heading = /^(#{2,3}) (.+)$/.exec(text);
    if (heading?.[1] === "##") {
      blocks.push(
        <h3 key={blocks.length} className="pt-2 font-semibold first:pt-0">
          {heading[2]}
        </h3>,
      );
    } else if (heading) {
      blocks.push(
        <h4 key={blocks.length} className="pt-1 font-medium text-muted-foreground">
          {heading[2]}
        </h4>,
      );
    } else {
      blocks.push(<p key={blocks.length}>{text}</p>);
    }
  }
  endList();
  return <div className="space-y-2 text-[13px] leading-relaxed">{blocks}</div>;
}
