import { useEffect, useRef, useState, type KeyboardEvent } from "react";

import { DashboardService } from "@bindings/core";
import { formatModifier, keycaps, keyOf, modifiersOf, type Platform } from "@/lib/accelerator";
import { cn } from "cn";

export function Keycaps({ caps, className }: { caps: string[]; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1", className)}>
      {caps.map((cap, i) => (
        <kbd
          key={i}
          className="inline-flex h-6 min-w-6 items-center justify-center rounded-md border border-b-2 bg-background px-1.5 font-sans text-xs font-medium"
        >
          {cap}
        </kbd>
      ))}
    </span>
  );
}

/**
 * Shows a hotkey and records a new one: click it, press a combination, and
 * onRecord receives the accelerator. Escape or clicking elsewhere cancels.
 * While recording, all global hotkeys are released so pressing a bound one
 * records it instead of triggering it.
 */
export function HotkeyRecorder({
  value,
  platform,
  onRecord,
}: {
  value: string;
  platform: Platform;
  onRecord: (accelerator: string) => Promise<unknown>;
}) {
  const [recording, setRecording] = useState(false);
  const [held, setHeld] = useState<string[]>([]);
  const recordingRef = useRef(false);
  // The keys held down, by code. Once a combination is recorded, the hotkeys
  // stay released until all of them are let go: re-registered while still
  // held, the next key repeat would trigger the hotkey on Windows.
  const pressed = useRef(new Set<string>());
  // The save of a recorded combination whose keys are still held.
  const saving = useRef<Promise<unknown> | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const start = () => {
    if (recordingRef.current || saving.current) return;
    // WebKit on macOS does not focus a button that is clicked, and the key
    // events would go to <body> instead.
    buttonRef.current?.focus();
    recordingRef.current = true;
    setRecording(true);
    void DashboardService.BeginHotkeyRecording();
  };

  const stop = () => {
    if (!recordingRef.current) return;
    recordingRef.current = false;
    setRecording(false);
    setHeld([]);
    pressed.current.clear();
    void DashboardService.EndHotkeyRecording();
  };

  // The recorded combination's keys are released: register the hotkeys again
  // once the new binding is saved.
  const released = () => {
    const save = saving.current;
    if (!save) return;
    saving.current = null;
    pressed.current.clear();
    void save.finally(() => void DashboardService.EndHotkeyRecording());
  };

  // Never leave the hotkeys released if the row disappears mid-recording.
  useEffect(
    () => () => {
      if (recordingRef.current || saving.current) void DashboardService.EndHotkeyRecording();
    },
    [],
  );

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (!recordingRef.current && !saving.current) return;
    e.preventDefault();
    e.stopPropagation();
    pressed.current.add(e.code);
    if (saving.current) return;
    const mods = modifiersOf(e.nativeEvent);
    const key = keyOf(e.nativeEvent);
    if (key === "escape" && mods.length === 0) {
      stop();
      return;
    }
    if (!key) {
      setHeld(mods.map((m) => formatModifier(m, platform)));
      return;
    }
    // Stop recording, but keep the hotkeys released until the new binding is
    // saved and the keys are let go: the Go side then resumes them with the
    // new binding in place.
    recordingRef.current = false;
    setRecording(false);
    setHeld([]);
    saving.current = onRecord([...mods, key].join("+"));
  };

  const onKeyUp = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (!recordingRef.current && !saving.current) return;
    pressed.current.delete(e.code);
    if (recordingRef.current) {
      setHeld(modifiersOf(e.nativeEvent).map((m) => formatModifier(m, platform)));
    } else if (pressed.current.size === 0) {
      released();
    }
  };

  return (
    <button
      ref={buttonRef}
      type="button"
      onClick={start}
      onBlur={() => {
        stop();
        released();
      }}
      onKeyDown={onKeyDown}
      onKeyUp={onKeyUp}
      className={cn(
        "flex h-8 min-w-36 items-center justify-center rounded-md border px-2 transition-colors outline-none",
        recording
          ? "border-primary bg-primary/5 ring-3 ring-ring"
          : "bg-secondary/60 hover:bg-secondary focus-visible:ring-3 focus-visible:ring-ring",
      )}
    >
      {recording ? (
        held.length > 0 ? (
          <Keycaps caps={held} />
        ) : (
          <span className="text-[13px] text-primary">按下快捷键…</span>
        )
      ) : value ? (
        <Keycaps caps={keycaps(value, platform)} />
      ) : (
        <span className="text-[13px] text-muted-foreground">未设置</span>
      )}
    </button>
  );
}
