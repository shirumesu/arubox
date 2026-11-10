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

  const start = () => {
    if (recordingRef.current) return;
    recordingRef.current = true;
    setRecording(true);
    void DashboardService.BeginHotkeyRecording();
  };

  const stop = () => {
    if (!recordingRef.current) return;
    recordingRef.current = false;
    setRecording(false);
    setHeld([]);
    void DashboardService.EndHotkeyRecording();
  };

  // Never leave the hotkeys released if the row disappears mid-recording.
  useEffect(
    () => () => {
      if (recordingRef.current) void DashboardService.EndHotkeyRecording();
    },
    [],
  );

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (!recordingRef.current) return;
    e.preventDefault();
    e.stopPropagation();
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
    // Stop listening, but keep the hotkeys released until the new binding is
    // saved: the Go side resumes them with the new binding in place.
    recordingRef.current = false;
    setRecording(false);
    setHeld([]);
    void onRecord([...mods, key].join("+")).finally(() => void DashboardService.EndHotkeyRecording());
  };

  const onKeyUp = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (recordingRef.current) {
      setHeld(modifiersOf(e.nativeEvent).map((m) => formatModifier(m, platform)));
    }
  };

  return (
    <button
      type="button"
      onClick={start}
      onBlur={stop}
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
