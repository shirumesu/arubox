import { Events } from "@wailsio/runtime";
import {
  ArrowUpRight,
  Check,
  Circle,
  Copy,
  Download,
  Grid3x3,
  Languages,
  LoaderCircle,
  Pencil,
  Pin,
  Redo2,
  ScanText,
  Square,
  Type,
  Undo2,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from "react";

import { Service, type Start } from "@bindings/modules/screenshot";
import { FrozenScreen, loadFrozenScreen } from "@/components/frozen-screen";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/errors";
import {
  boxFrom,
  clampBox,
  contains,
  drawShape,
  handleAt,
  handleCursors,
  handlePoint,
  handles,
  renderSelection,
  resize,
  type Box,
  type Handle,
  type Point,
  type Shape,
  type Tool,
} from "@/screenshot/shapes";
import { cn } from "cn";

type Drag =
  | { kind: "select"; from: Point; moved: boolean }
  | { kind: "move"; from: Point; start: Box }
  | { kind: "resize"; handle: Handle; from: Point; start: Box }
  | { kind: "shape" };

interface Capture {
  start: Start;
  /** The frozen screen at its full resolution. */
  image: HTMLCanvasElement;
  screen: Box;
  windows: Box[];
}

const colors = ["#f43f5e", "#f59e0b", "#22c55e", "#3b82f6", "#ffffff", "#111111"] as const;
const widths = [2, 4, 8] as const;
/** Text sizes, in points, for the three widths. */
const textSizes: Record<number, number> = { 2: 16, 4: 22, 8: 32 };
const accent = "#3b82f6";
/** A press that moves less than this is a click, which picks a window. */
const clickSlop = 3;
const minSelection = 4;

const mac = navigator.userAgent.includes("Mac");

export function Screenshot() {
  const [capture, setCapture] = useState<Capture | null>(null);
  const [sel, setSel] = useState<Box | null>(null);
  const [selecting, setSelecting] = useState<Box | null>(null);
  const [hover, setHover] = useState<Box | null>(null);
  const [tool, setTool] = useState<Tool | null>(null);
  const [color, setColor] = useState<string>(colors[0]);
  const [width, setWidth] = useState<number>(widths[1]);
  const [shapes, setShapes] = useState<Shape[]>([]);
  const [undone, setUndone] = useState<Shape[]>([]);
  const [draft, setDraft] = useState<Shape | null>(null);
  const [editing, setEditing] = useState<{ at: Point; text: string } | null>(null);
  const [cursor, setCursor] = useState("crosshair");
  // The action under way, whose button spins meanwhile.
  const [pending, setPending] = useState<string | null>(null);
  // The text read from the selection, shown for copying.
  const [text, setText] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const drag = useRef<Drag | null>(null);

  const reset = useCallback(() => {
    setSel(null);
    setSelecting(null);
    setHover(null);
    setTool(null);
    setShapes([]);
    setUndone([]);
    setDraft(null);
    setEditing(null);
    setPending(null);
    setText(null);
    setMessage(null);
    drag.current = null;
  }, []);

  // A capture arrives: load its picture, then show the window once drawn.
  useEffect(() => {
    let latest = 0;
    const load = (start: Start) => {
      latest = start.seq;
      loadFrozenScreen(`/screenshot/capture?seq=${String(start.seq)}`, start.picture).then(
        (image) => {
          if (start.seq !== latest) return;
          reset();
          setCapture({
            start,
            image,
            screen: { x: 0, y: 0, w: start.width, h: start.height },
            windows: (start.windows ?? []).map((r) => ({ x: r.X, y: r.Y, w: r.Width, h: r.Height })),
          });
        },
        (err: unknown) => {
          console.error(err);
          void Service.Cancel(start.seq);
        },
      );
    };
    const offStart = Events.On("screenshot:start", (event) => {
      load(event.data);
    });
    // The window hid: let go of the picture.
    const offEnd = Events.On("screenshot:end", () => {
      latest = 0;
      reset();
      setCapture(null);
    });
    Service.Attach().then((start) => {
      if (start) load(start);
    }, console.error);
    return () => {
      offStart();
      offEnd();
    };
  }, [reset]);

  useLayoutEffect(() => {
    if (capture) void Service.Present(capture.start.seq);
  }, [capture]);

  // Draw the dimmed screen, the selection and the annotations.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !capture) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const { screen, image } = capture;
    const dpr = window.devicePixelRatio;
    if (canvas.width !== Math.round(screen.w * dpr)) {
      canvas.width = Math.round(screen.w * dpr);
      canvas.height = Math.round(screen.h * dpr);
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, screen.w, screen.h);
    const area = selecting ?? sel ?? hover;
    ctx.beginPath();
    ctx.rect(0, 0, screen.w, screen.h);
    if (area) ctx.rect(area.x, area.y, area.w, area.h);
    ctx.fillStyle = "rgba(0, 0, 0, 0.4)";
    ctx.fill("evenodd");
    if (sel) {
      ctx.save();
      ctx.beginPath();
      ctx.rect(sel.x, sel.y, sel.w, sel.h);
      ctx.clip();
      for (const s of shapes) drawShape(ctx, s, image, screen);
      if (draft) drawShape(ctx, draft, image, screen);
      ctx.restore();
    }
    if (area) {
      ctx.strokeStyle = accent;
      ctx.lineWidth = 1;
      ctx.strokeRect(area.x + 0.5, area.y + 0.5, area.w - 1, area.h - 1);
    }
    if (sel && !tool) {
      ctx.fillStyle = "#ffffff";
      ctx.strokeStyle = accent;
      for (const h of handles) {
        const p = handlePoint(sel, h);
        ctx.fillRect(p.x - 3.5, p.y - 3.5, 7, 7);
        ctx.strokeRect(p.x - 3.5, p.y - 3.5, 7, 7);
      }
    }
  }, [capture, sel, selecting, hover, shapes, draft, tool]);

  const point = (e: PointerEvent): Point => ({ x: e.clientX, y: e.clientY });

  const commitText = () => {
    if (editing && editing.text.trim() !== "") {
      setShapes((all) => [
        ...all,
        { kind: "text", at: editing.at, text: editing.text, color, size: textSizes[width] ?? 22 },
      ]);
      setUndone([]);
    }
    setEditing(null);
  };

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (!capture || pending || e.button !== 0) return;
    const p = point(e);
    if (editing) {
      commitText();
      return;
    }
    e.currentTarget.setPointerCapture(e.pointerId);
    if (sel && tool) {
      if (!contains(sel, p)) return;
      if (tool === "text") {
        setEditing({ at: p, text: "" });
        return;
      }
      drag.current = { kind: "shape" };
      setDraft(
        tool === "pen"
          ? { kind: "pen", points: [p], color, width }
          : { kind: tool, from: p, to: p, color, width: tool === "mosaic" ? 0 : width },
      );
      return;
    }
    // Moving or changing the selection makes the text read from it stale.
    setText(null);
    if (sel) {
      const h = handleAt(sel, p);
      if (h) {
        drag.current = { kind: "resize", handle: h, from: p, start: sel };
        return;
      }
      if (contains(sel, p)) {
        drag.current = { kind: "move", from: p, start: sel };
        return;
      }
      // Starting over would lose the annotations.
      if (shapes.length > 0) return;
    }
    drag.current = { kind: "select", from: p, moved: false };
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!capture) return;
    const p = point(e);
    const d = drag.current;
    if (!d) {
      if (!sel) {
        setHover(capture.windows.find((w) => contains(w, p)) ?? null);
        setCursor("crosshair");
      } else if (tool) {
        setCursor(contains(sel, p) ? (tool === "text" ? "text" : "crosshair") : "default");
      } else {
        const h = handleAt(sel, p);
        setCursor(
          h ? handleCursors[h] : contains(sel, p) ? "move" : shapes.length > 0 ? "default" : "crosshair",
        );
      }
      return;
    }
    const { screen } = capture;
    switch (d.kind) {
      case "select":
        if (!d.moved && Math.hypot(p.x - d.from.x, p.y - d.from.y) < clickSlop) return;
        d.moved = true;
        setHover(null);
        setSelecting(boxFrom(d.from, p));
        break;
      case "move":
        setSel(
          clampBox(
            { ...d.start, x: d.start.x + p.x - d.from.x, y: d.start.y + p.y - d.from.y },
            screen.w,
            screen.h,
          ),
        );
        break;
      case "resize":
        setSel(clampBox(resize(d.start, d.handle, p.x - d.from.x, p.y - d.from.y), screen.w, screen.h));
        break;
      case "shape":
        setDraft((s) => {
          if (!s) return s;
          if (s.kind === "pen") return { ...s, points: [...s.points, p] };
          if (s.kind === "text") return s;
          return { ...s, to: p };
        });
        break;
    }
  };

  const onPointerUp = () => {
    const d = drag.current;
    drag.current = null;
    if (!d || !capture) return;
    if (d.kind === "select") {
      const picked = d.moved ? selecting : (hover ?? capture.screen);
      setSelecting(null);
      setHover(null);
      if (picked && picked.w >= minSelection && picked.h >= minSelection) {
        setSel(clampBox(picked, capture.screen.w, capture.screen.h));
        setShapes([]);
        setUndone([]);
      }
      return;
    }
    if (d.kind === "shape" && draft) {
      const kept =
        draft.kind === "pen"
          ? draft.points.length > 1
          : draft.kind === "text" || boxFrom(draft.from, draft.to).w + boxFrom(draft.from, draft.to).h > 3;
      if (kept) {
        setShapes((all) => [...all, draft]);
        setUndone([]);
      }
      setDraft(null);
    }
  };

  // Right click steps back: out of the tool, then the selection, then the capture.
  const onContextMenu = (e: MouseEvent) => {
    e.preventDefault();
    if (!capture || pending) return;
    if (editing) {
      setEditing(null);
    } else if (tool) {
      setTool(null);
    } else if (sel) {
      reset();
    } else {
      void Service.Cancel(capture.start.seq);
    }
  };

  const undo = () => {
    const last = shapes.at(-1);
    if (!last) return;
    setShapes(shapes.slice(0, -1));
    setUndone([...undone, last]);
  };

  const redo = () => {
    const next = undone.at(-1);
    if (!next) return;
    setUndone(undone.slice(0, -1));
    setShapes([...shapes, next]);
  };

  /** Runs an action, keeping the editor open with the error if it fails. */
  const run = (name: string, action: () => Promise<unknown>) => {
    if (pending) return;
    setPending(name);
    setMessage(null);
    action().then(
      () => {
        setPending(null);
      },
      (err: unknown) => {
        setPending(null);
        setMessage(errorMessage(err));
      },
    );
  };

  const finished = async () => {
    if (!capture || !sel) throw new Error("没有选区");
    return renderSelection(capture.image, capture.screen, sel, shapes);
  };

  /** The selection in the capture's pixels, for reading its text. */
  const pixels = () => {
    if (!capture || !sel) return { x: 0, y: 0, w: 0, h: 0 };
    const scale = capture.image.width / capture.screen.w;
    return {
      x: Math.round(sel.x * scale),
      y: Math.round(sel.y * scale),
      w: Math.round(sel.w * scale),
      h: Math.round(sel.h * scale),
    };
  };

  const seq = capture?.start.seq ?? 0;
  const copy = () => {
    run("copy", async () => Service.Copy(seq, await finished()));
  };
  const save = () => {
    run("save", async () => Service.Save(seq, await finished()));
  };
  const pin = () => {
    run("pin", async () => {
      if (!sel) return;
      await Service.Pin(
        seq,
        await finished(),
        Math.round(sel.x),
        Math.round(sel.y),
        Math.round(sel.w),
        Math.round(sel.h),
      );
    });
  };
  const readText = () => {
    const r = pixels();
    run("read", async () => {
      setText(await Service.ReadText(seq, r.x, r.y, r.w, r.h));
    });
  };
  const copyText = () => {
    if (text !== null) run("copyText", () => Service.CopyText(seq, text));
  };
  const translate = () => {
    const r = pixels();
    run("translate", () => Service.Translate(seq, r.x, r.y, r.w, r.h));
  };

  useEffect(() => {
    if (!capture) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (editing) return;
      // Typing in the text panel keeps its keys, except Esc.
      if (e.target instanceof HTMLTextAreaElement && e.key !== "Escape") return;
      const mod = mac ? e.metaKey : e.ctrlKey;
      const key = e.key.toLowerCase();
      if (e.key === "Escape") {
        e.preventDefault();
        void Service.Cancel(capture.start.seq);
      } else if (pending || !sel) {
        return;
      } else if (mod && key === "z") {
        e.preventDefault();
        if (e.shiftKey) redo();
        else undo();
      } else if ((mod && key === "c") || e.key === "Enter") {
        e.preventDefault();
        copy();
      } else if (mod && key === "s") {
        e.preventDefault();
        save();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  });

  if (!capture) return null;
  const { screen } = capture;
  const scale = capture.image.width / screen.w;
  const shown = selecting ?? sel;

  return (
    <div className="relative h-full overflow-hidden select-none">
      <FrozenScreen canvas={capture.image} />
      <canvas ref={canvasRef} className="pointer-events-none absolute inset-0 size-full" />
      <div
        className="absolute inset-0"
        style={{ cursor }}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onContextMenu={onContextMenu}
        // The press would move the focus here, out of the text being typed.
        onMouseDown={(e) => {
          e.preventDefault();
        }}
        onDoubleClick={(e) => {
          if (sel && !tool && contains(sel, { x: e.clientX, y: e.clientY })) copy();
        }}
      />

      {!shown && !hover && (
        <p className="pointer-events-none absolute top-16 left-1/2 -translate-x-1/2 rounded-full bg-black/70 px-4 py-1.5 text-[13px] text-white">
          拖动框选区域，或点击选择窗口；Esc 取消
        </p>
      )}

      {shown && (
        <span
          className="pointer-events-none absolute rounded bg-black/70 px-1.5 py-0.5 text-[11px] text-white tabular-nums"
          style={{ left: shown.x, top: Math.max(shown.y - 22, 2) }}
        >
          {Math.round(shown.w * scale)} × {Math.round(shown.h * scale)}
        </span>
      )}

      {editing && (
        <TextInput
          at={editing.at}
          value={editing.text}
          color={color}
          size={textSizes[width] ?? 22}
          onChange={(text) => {
            setEditing({ ...editing, text });
          }}
          onCommit={commitText}
          onCancel={() => {
            setEditing(null);
          }}
        />
      )}

      {sel && !selecting && (
        <Toolbar sel={sel} screen={screen} room={text === null ? 110 : 320}>
          <div className="flex items-center gap-0.5 rounded-lg border bg-background p-1 text-foreground shadow-lg">
            <ToolButton
              icon={Square}
              label="矩形"
              active={tool === "rect"}
              onClick={() => {
                setTool(tool === "rect" ? null : "rect");
              }}
            />
            <ToolButton
              icon={Circle}
              label="椭圆"
              active={tool === "ellipse"}
              onClick={() => {
                setTool(tool === "ellipse" ? null : "ellipse");
              }}
            />
            <ToolButton
              icon={ArrowUpRight}
              label="箭头"
              active={tool === "arrow"}
              onClick={() => {
                setTool(tool === "arrow" ? null : "arrow");
              }}
            />
            <ToolButton
              icon={Pencil}
              label="画笔"
              active={tool === "pen"}
              onClick={() => {
                setTool(tool === "pen" ? null : "pen");
              }}
            />
            <ToolButton
              icon={Type}
              label="文字"
              active={tool === "text"}
              onClick={() => {
                setTool(tool === "text" ? null : "text");
              }}
            />
            <ToolButton
              icon={Grid3x3}
              label="马赛克"
              active={tool === "mosaic"}
              onClick={() => {
                setTool(tool === "mosaic" ? null : "mosaic");
              }}
            />
            <Divider />
            <ToolButton
              icon={Undo2}
              label={`撤销（${mac ? "⌘Z" : "Ctrl+Z"}）`}
              disabled={shapes.length === 0}
              onClick={undo}
            />
            <ToolButton
              icon={Redo2}
              label={`重做（${mac ? "⇧⌘Z" : "Ctrl+Shift+Z"}）`}
              disabled={undone.length === 0}
              onClick={redo}
            />
            <Divider />
            <ToolButton
              icon={ScanText}
              label="识别文字"
              active={text !== null}
              spin={pending === "read"}
              onClick={readText}
            />
            {capture.start.translate && (
              <ToolButton
                icon={Languages}
                label="翻译文字"
                spin={pending === "translate"}
                onClick={translate}
              />
            )}
            <ToolButton icon={Pin} label="贴在屏幕上" spin={pending === "pin"} onClick={pin} />
            <ToolButton
              icon={Download}
              label={`保存（${mac ? "⌘S" : "Ctrl+S"}）`}
              spin={pending === "save"}
              onClick={save}
            />
            <Divider />
            <ToolButton icon={X} label="取消（Esc）" onClick={() => void Service.Cancel(seq)} />
            <ToolButton
              icon={Check}
              label={`复制（${mac ? "⌘C" : "Ctrl+C"}）`}
              primary
              spin={pending === "copy"}
              onClick={copy}
            />
          </div>
          {tool && tool !== "mosaic" && (
            <div className="flex items-center gap-1.5 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
              {widths.map((w) => (
                <button
                  key={w}
                  type="button"
                  title={tool === "text" ? "字号" : "粗细"}
                  onClick={() => {
                    setWidth(w);
                  }}
                  className={cn(
                    "flex size-6 items-center justify-center rounded-md hover:bg-accent",
                    width === w && "bg-accent",
                  )}
                >
                  <span className="rounded-full bg-foreground" style={{ width: w + 2, height: w + 2 }} />
                </button>
              ))}
              <Divider />
              {colors.map((c) => (
                <button
                  key={c}
                  type="button"
                  title="颜色"
                  onClick={() => {
                    setColor(c);
                  }}
                  className={cn(
                    "size-5 rounded-full border border-black/20",
                    color === c && "ring-2 ring-ring ring-offset-1 ring-offset-background",
                  )}
                  style={{ background: c }}
                />
              ))}
            </div>
          )}
          {text !== null && (
            <div className="flex w-80 flex-col gap-1.5 rounded-lg border bg-background p-1.5 text-foreground shadow-lg">
              <textarea
                value={text}
                aria-label="识别出的文字"
                spellCheck={false}
                onChange={(e) => {
                  setText(e.target.value);
                }}
                className="h-44 resize-none rounded-md bg-muted/60 px-2 py-1.5 text-[13px] leading-relaxed outline-none select-text"
              />
              <div className="flex items-center justify-between gap-2 pl-1">
                <span className="text-[11px] text-muted-foreground">可以先修改再复制</span>
                <Button size="sm" onClick={copyText} disabled={pending !== null}>
                  {pending === "copyText" ? <LoaderCircle className="animate-spin" /> : <Copy />}
                  复制文字
                </Button>
              </div>
            </div>
          )}
          {message && (
            <p className="rounded-lg bg-destructive px-3 py-1.5 text-[12px] text-white shadow-lg">
              {message}
            </p>
          )}
        </Toolbar>
      )}
    </div>
  );
}

/** Places its rows below the selection, or above it when there is no room, aligned to its right edge. */
function Toolbar({
  sel,
  screen,
  room,
  children,
}: {
  sel: Box;
  screen: Box;
  room: number;
  children: ReactNode;
}) {
  const below = sel.y + sel.h + room < screen.h;
  const above = sel.y > room;
  const style = below
    ? { top: sel.y + sel.h + 8 }
    : above
      ? { bottom: screen.h - sel.y + 8 }
      : { bottom: screen.h - (sel.y + sel.h) + 8 };
  return (
    <div
      className={cn("absolute flex items-end gap-1.5", below ? "flex-col" : "flex-col-reverse")}
      style={{ ...style, right: Math.max(screen.w - (sel.x + sel.w), 8) }}
      onPointerDown={(e) => {
        e.stopPropagation();
      }}
    >
      {children}
    </div>
  );
}

function ToolButton({
  icon: Icon,
  label,
  active,
  disabled,
  primary,
  spin,
  onClick,
}: {
  icon: LucideIcon;
  label: string;
  active?: boolean;
  disabled?: boolean;
  primary?: boolean;
  spin?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={active}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-35 disabled:hover:bg-transparent [&_svg]:size-4",
        active && "bg-accent text-primary",
        primary && "text-primary",
      )}
    >
      {spin ? <LoaderCircle className="animate-spin" /> : <Icon />}
    </button>
  );
}

function Divider() {
  return <span className="mx-0.5 h-5 w-px bg-border" />;
}

/** Types an annotation in place. Enter commits; Shift+Enter starts a new line. */
function TextInput({
  at,
  value,
  color,
  size,
  onChange,
  onCommit,
  onCancel,
}: {
  at: Point;
  value: string;
  color: string;
  size: number;
  onChange: (text: string) => void;
  onCommit: () => void;
  onCancel: () => void;
}) {
  const onKeyDown = (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    // Enter that confirms an input method's candidate is not a commit.
    // Safari marks that Enter only with keyCode 229, not with isComposing.
    // eslint-disable-next-line @typescript-eslint/no-deprecated
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      onCommit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
    }
  };
  const lines = value.split("\n");
  return (
    <textarea
      autoFocus
      value={value}
      rows={lines.length}
      onChange={(e) => {
        onChange(e.target.value);
      }}
      onKeyDown={onKeyDown}
      onBlur={onCommit}
      onPointerDown={(e) => {
        e.stopPropagation();
      }}
      className="absolute resize-none overflow-hidden border border-dashed border-white/70 bg-transparent p-0 outline-none"
      style={{
        left: at.x,
        top: at.y,
        color,
        fontSize: size,
        lineHeight: 1.25,
        minWidth: size * 2,
        width: `${String(Math.max(...lines.map((l) => l.length), 2) + 1)}em`,
      }}
    />
  );
}
