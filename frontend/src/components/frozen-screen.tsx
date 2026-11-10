import { useLayoutEffect, useRef } from "react";

import type { Picture } from "@bindings/platform";

/**
 * Loads a frozen screen onto a canvas of its full size. The Go side serves it
 * in strips at url with &part=N appended, as RGBA rows: fetched all at once,
 * they arrive about twice as fast as one response of the whole picture, and
 * go onto the canvas without decoding an image file.
 */
export async function loadFrozenScreen(url: string, picture: Picture): Promise<HTMLCanvasElement> {
  const canvas = document.createElement("canvas");
  canvas.width = picture.width;
  canvas.height = picture.height;
  canvas.className = "absolute inset-0 size-full";
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("canvas");
  const rows = Math.ceil(picture.height / picture.strips);
  await Promise.all(
    Array.from({ length: picture.strips }, async (_, part) => {
      const response = await fetch(`${url}&part=${String(part)}`);
      if (!response.ok) throw new Error(`frozen screen ${String(response.status)}`);
      const pixels = new Uint8ClampedArray(await response.arrayBuffer());
      ctx.putImageData(new ImageData(pixels, picture.width), 0, part * rows);
    }),
  );
  return canvas;
}

/** Shows a canvas from loadFrozenScreen over the whole of its parent. */
export function FrozenScreen({ canvas }: { canvas: HTMLCanvasElement }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const host = ref.current;
    if (!host) return;
    host.replaceChildren(canvas);
    return () => {
      host.replaceChildren();
    };
  }, [canvas]);
  return <div ref={ref} className="absolute inset-0" />;
}
