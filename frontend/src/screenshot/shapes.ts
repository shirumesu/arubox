/** A rectangle in points, relative to the captured screen. */
export interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface Point {
  x: number;
  y: number;
}

export type Tool = "rect" | "ellipse" | "arrow" | "pen" | "text" | "mosaic";

/** An annotation, in points relative to the captured screen. */
export type Shape =
  | { kind: "rect" | "ellipse" | "arrow" | "mosaic"; from: Point; to: Point; color: string; width: number }
  | { kind: "pen"; points: Point[]; color: string; width: number }
  | { kind: "text"; at: Point; text: string; color: string; size: number };

/** The box spanned by two corners, whichever way they were dragged. */
export function boxFrom(a: Point, b: Point): Box {
  return { x: Math.min(a.x, b.x), y: Math.min(a.y, b.y), w: Math.abs(b.x - a.x), h: Math.abs(b.y - a.y) };
}

export function contains(b: Box, p: Point): boolean {
  return p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h;
}

/** Keeps a box inside the screen. */
export function clampBox(b: Box, width: number, height: number): Box {
  const w = Math.min(b.w, width);
  const h = Math.min(b.h, height);
  return { x: Math.min(Math.max(b.x, 0), width - w), y: Math.min(Math.max(b.y, 0), height - h), w, h };
}

/** The selection's resize handles: corners and edge midpoints. */
export type Handle = "nw" | "n" | "ne" | "e" | "se" | "s" | "sw" | "w";

export const handles: Handle[] = ["nw", "n", "ne", "e", "se", "s", "sw", "w"];

export function handlePoint(b: Box, h: Handle): Point {
  const x = h.includes("w") ? b.x : h.includes("e") ? b.x + b.w : b.x + b.w / 2;
  const y = h.includes("n") ? b.y : h.includes("s") ? b.y + b.h : b.y + b.h / 2;
  return { x, y };
}

/** The handle within reach of p, if any. */
export function handleAt(b: Box, p: Point, reach = 6): Handle | null {
  return (
    handles.find((h) => {
      const q = handlePoint(b, h);
      return Math.abs(q.x - p.x) <= reach && Math.abs(q.y - p.y) <= reach;
    }) ?? null
  );
}

/** Moves the edges a handle controls by the drag from start, from the box at drag start. */
export function resize(b: Box, h: Handle, dx: number, dy: number): Box {
  let x0 = b.x;
  let y0 = b.y;
  let x1 = b.x + b.w;
  let y1 = b.y + b.h;
  if (h.includes("w")) x0 += dx;
  if (h.includes("e")) x1 += dx;
  if (h.includes("n")) y0 += dy;
  if (h.includes("s")) y1 += dy;
  return boxFrom({ x: x0, y: y0 }, { x: x1, y: y1 });
}

export const handleCursors: Record<Handle, string> = {
  nw: "nwse-resize",
  se: "nwse-resize",
  ne: "nesw-resize",
  sw: "nesw-resize",
  n: "ns-resize",
  s: "ns-resize",
  e: "ew-resize",
  w: "ew-resize",
};

/** Mosaic blocks are this many points wide. */
const mosaicBlock = 8;

/**
 * Draws a shape on a context whose units are points. img is the capture,
 * drawn at the screen's size in points, which mosaics pixelate.
 */
export function drawShape(ctx: CanvasRenderingContext2D, s: Shape, img: HTMLImageElement, screen: Box) {
  ctx.save();
  if (s.kind === "text") {
    ctx.fillStyle = s.color;
    ctx.font = `${String(s.size)}px -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif`;
    ctx.textBaseline = "top";
    s.text.split("\n").forEach((line, i) => {
      ctx.fillText(line, s.at.x, s.at.y + i * s.size * 1.25);
    });
    ctx.restore();
    return;
  }
  ctx.strokeStyle = s.color;
  ctx.fillStyle = s.color;
  ctx.lineWidth = s.width;
  ctx.lineCap = "round";
  ctx.lineJoin = "round";
  if (s.kind === "pen") {
    ctx.beginPath();
    s.points.forEach((p, i) => {
      if (i === 0) ctx.moveTo(p.x, p.y);
      else ctx.lineTo(p.x, p.y);
    });
    ctx.stroke();
    ctx.restore();
    return;
  }
  const b = boxFrom(s.from, s.to);
  switch (s.kind) {
    case "rect":
      ctx.strokeRect(b.x, b.y, b.w, b.h);
      break;
    case "ellipse":
      ctx.beginPath();
      ctx.ellipse(b.x + b.w / 2, b.y + b.h / 2, b.w / 2, b.h / 2, 0, 0, Math.PI * 2);
      ctx.stroke();
      break;
    case "arrow":
      drawArrow(ctx, s.from, s.to, s.width);
      break;
    case "mosaic":
      drawMosaic(ctx, img, b, screen);
      break;
  }
  ctx.restore();
}

function drawArrow(ctx: CanvasRenderingContext2D, from: Point, to: Point, width: number) {
  const angle = Math.atan2(to.y - from.y, to.x - from.x);
  const head = Math.max(10, width * 4);
  // The shaft stops inside the head, so its round cap does not poke out.
  const shaftEnd = { x: to.x - Math.cos(angle) * head * 0.6, y: to.y - Math.sin(angle) * head * 0.6 };
  ctx.beginPath();
  ctx.moveTo(from.x, from.y);
  ctx.lineTo(shaftEnd.x, shaftEnd.y);
  ctx.stroke();
  ctx.beginPath();
  ctx.moveTo(to.x, to.y);
  ctx.lineTo(to.x - head * Math.cos(angle - Math.PI / 7), to.y - head * Math.sin(angle - Math.PI / 7));
  ctx.lineTo(to.x - head * Math.cos(angle + Math.PI / 7), to.y - head * Math.sin(angle + Math.PI / 7));
  ctx.closePath();
  ctx.fill();
}

/** Pixelates the capture inside b by drawing it small, then large without smoothing. */
function drawMosaic(ctx: CanvasRenderingContext2D, img: HTMLImageElement, b: Box, screen: Box) {
  if (b.w < 1 || b.h < 1) return;
  const scale = img.naturalWidth / screen.w;
  const cols = Math.max(1, Math.round(b.w / mosaicBlock));
  const rows = Math.max(1, Math.round(b.h / mosaicBlock));
  const small = document.createElement("canvas");
  small.width = cols;
  small.height = rows;
  const sctx = small.getContext("2d");
  if (!sctx) return;
  sctx.drawImage(img, b.x * scale, b.y * scale, b.w * scale, b.h * scale, 0, 0, cols, rows);
  ctx.imageSmoothingEnabled = false;
  ctx.drawImage(small, 0, 0, cols, rows, b.x, b.y, b.w, b.h);
}

/**
 * Renders the selection with its annotations at the capture's full
 * resolution, as a PNG in base64.
 */
export async function renderSelection(
  img: HTMLImageElement,
  screen: Box,
  sel: Box,
  shapes: Shape[],
): Promise<string> {
  const scale = img.naturalWidth / screen.w;
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(sel.w * scale));
  canvas.height = Math.max(1, Math.round(sel.h * scale));
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("canvas");
  ctx.scale(scale, scale);
  ctx.translate(-sel.x, -sel.y);
  ctx.drawImage(img, 0, 0, screen.w, screen.h);
  for (const s of shapes) drawShape(ctx, s, img, screen);
  const blob = await new Promise<Blob | null>((resolve) => {
    canvas.toBlob(resolve, "image/png");
  });
  if (!blob) throw new Error("png");
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}
