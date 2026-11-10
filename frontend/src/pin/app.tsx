import { X } from "lucide-react";
import { useEffect, useState, type CSSProperties } from "react";

import { Service } from "@bindings/modules/screenshot";
import { cn } from "cn";

/** Lets the picture move the frameless window. */
const dragRegion = { "--wails-draggable": "drag" } as CSSProperties;
const noDrag = { "--wails-draggable": "no-drag" } as CSSProperties;

const mac = navigator.userAgent.includes("Mac");
/** Each wheel step zooms by this much. */
const zoomStep = 1.1;

/**
 * A picture pinned on the screen. Drag to move it, scroll to zoom,
 * double-click or Esc to close, ⌘C (Ctrl+C) to copy.
 */
export function Pin() {
  const id = Number(new URLSearchParams(window.location.search).get("id"));
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        void Service.ClosePin(id);
      } else if ((mac ? e.metaKey : e.ctrlKey) && e.key.toLowerCase() === "c") {
        e.preventDefault();
        Service.CopyPin(id).then(() => {
          setCopied(true);
        }, console.error);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [id]);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => {
      setCopied(false);
    }, 1200);
    return () => {
      clearTimeout(timer);
    };
  }, [copied]);

  return (
    <div
      className="group relative h-full"
      style={dragRegion}
      onWheel={(e) => {
        void Service.ZoomPin(id, e.deltaY < 0 ? zoomStep : 1 / zoomStep);
      }}
      onDoubleClick={() => void Service.ClosePin(id)}
    >
      <img src={`/screenshot/pin/${String(id)}.png`} alt="" draggable={false} className="size-full" />
      <button
        type="button"
        title="关闭（Esc）"
        aria-label="关闭"
        style={noDrag}
        onClick={() => void Service.ClosePin(id)}
        className="absolute top-1.5 right-1.5 flex size-6 items-center justify-center rounded-full bg-black/60 text-white opacity-0 transition-opacity group-hover:opacity-100 [&_svg]:size-3.5"
      >
        <X />
      </button>
      <span
        className={cn(
          "pointer-events-none absolute bottom-1.5 left-1/2 -translate-x-1/2 rounded bg-black/70 px-2 py-0.5 text-[11px] text-white opacity-0 transition-opacity",
          copied && "opacity-100",
        )}
      >
        已复制
      </span>
    </div>
  );
}
