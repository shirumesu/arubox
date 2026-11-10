import { Events } from "@wailsio/runtime";
import { useEffect, useLayoutEffect, useState, type PointerEvent } from "react";

import { Service, type Frame } from "@bindings/modules/translate";

interface Drag {
  x0: number;
  y0: number;
  x1: number;
  y1: number;
}

/** A frozen screen whose picture has been decoded. */
interface Shown {
  seq: number;
  url: string;
}

/** Drags shorter than this on either side are taken as stray clicks. */
const minSize = 4;

function area(d: Drag) {
  return {
    x: Math.min(d.x0, d.x1),
    y: Math.min(d.y0, d.y1),
    width: Math.abs(d.x1 - d.x0),
    height: Math.abs(d.y1 - d.y0),
  };
}

/**
 * Shows a frozen picture of the screen so the user can drag out the area
 * whose text is translated. Esc or the right button cancels.
 */
export function Capture() {
  const [shown, setShown] = useState<Shown | null>(null);
  const [drag, setDrag] = useState<Drag | null>(null);

  // A capture arrives: load its picture, then show the window once drawn.
  useEffect(() => {
    let latest = 0;
    const load = (frame: Frame) => {
      latest = frame.seq;
      fetch(`/translate/capture.bmp?seq=${String(frame.seq)}`)
        .then((response) => {
          if (!response.ok) throw new Error(`capture ${String(response.status)}`);
          return response.blob();
        })
        .then(async (blob) => {
          const url = URL.createObjectURL(blob);
          const image = new Image();
          image.src = url;
          await image.decode();
          if (frame.seq !== latest) {
            URL.revokeObjectURL(url);
            return;
          }
          setDrag(null);
          setShown({ seq: frame.seq, url });
        })
        .catch((err: unknown) => {
          console.error(err);
          void Service.CancelCapture();
        });
    };
    const offStart = Events.On("translate:capture", (event) => {
      load(event.data);
    });
    // The window hid: let go of the picture.
    const offEnd = Events.On("translate:capture-end", () => {
      latest = 0;
      setDrag(null);
      setShown(null);
    });
    Service.AttachCapture().then((frame) => {
      if (frame) load(frame);
    }, console.error);
    return () => {
      offStart();
      offEnd();
    };
  }, []);

  useEffect(() => {
    if (!shown) return;
    return () => {
      URL.revokeObjectURL(shown.url);
    };
  }, [shown]);

  useLayoutEffect(() => {
    if (shown) void Service.PresentCapture(shown.seq);
  }, [shown]);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setDrag(null);
        void Service.CancelCapture();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  if (!shown) return null;

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button === 2) {
      setDrag(null);
      void Service.CancelCapture();
      return;
    }
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    setDrag({ x0: e.clientX, y0: e.clientY, x1: e.clientX, y1: e.clientY });
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!drag) return;
    setDrag({ ...drag, x1: e.clientX, y1: e.clientY });
  };

  const onPointerUp = () => {
    if (!drag) return;
    const a = area(drag);
    setDrag(null);
    if (a.width < minSize || a.height < minSize) return;
    void Service.FinishCapture(
      shown.seq,
      Math.round(a.x),
      Math.round(a.y),
      Math.round(a.width),
      Math.round(a.height),
    );
  };

  const selected = drag ? area(drag) : null;
  return (
    <div
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onContextMenu={(e) => {
        e.preventDefault();
      }}
      className="relative h-full cursor-crosshair overflow-hidden"
    >
      <img src={shown.url} alt="" draggable={false} className="absolute inset-0 size-full" />
      {selected ? (
        <>
          <div
            className="absolute border border-white bg-white/[0.01] shadow-[0_0_0_200vmax_rgba(0,0,0,0.35)]"
            style={{ left: selected.x, top: selected.y, width: selected.width, height: selected.height }}
          />
          <span
            className="absolute rounded bg-black/70 px-1.5 py-0.5 text-[11px] text-white tabular-nums"
            style={{ left: selected.x, top: Math.max(selected.y - 22, 2) }}
          >
            {Math.round(selected.width)} × {Math.round(selected.height)}
          </span>
        </>
      ) : (
        <div className="absolute inset-0 bg-black/35">
          <p className="absolute top-16 left-1/2 -translate-x-1/2 rounded-full bg-black/70 px-4 py-1.5 text-[13px] text-white">
            拖动框选要翻译的文字，Esc 或右键取消
          </p>
        </div>
      )}
    </div>
  );
}
