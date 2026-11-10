import { Events } from "@wailsio/runtime";
import { useEffect, useEffectEvent, useRef, useState, type KeyboardEvent } from "react";

import { DashboardService } from "@bindings/core";
import type { KeyPress } from "@bindings/platform";
import { formatModifier, keycaps, keyOf, modifiersOf, type Modifier, type Platform } from "@/lib/accelerator";
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

  const active = () => recordingRef.current || saving.current !== null;

  const press = (mods: Modifier[], key: string | null) => {
    if (saving.current) return;
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

  /** A key went up; idle means none is held any more. */
  const lift = (mods: Modifier[], idle: boolean) => {
    if (recordingRef.current) {
      setHeld(mods.map((m) => formatModifier(m, platform)));
    } else if (idle) {
      released();
    }
  };

  // On Windows the Go side takes the keyboard while recording, so that the
  // system and other programs do not act on the keys, and sends them here.
  const onCapturedKey = useEffectEvent((k: KeyPress) => {
    if (!active()) return;
    const mods = k.modifiers as Modifier[];
    if (k.down) press(mods, k.key || null);
    else lift(mods, k.idle);
  });
  useEffect(
    () =>
      Events.On("hotkey:key", (event) => {
        onCapturedKey(event.data);
      }),
    [],
  );

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (!active()) return;
    e.preventDefault();
    e.stopPropagation();
    pressed.current.add(e.code);
    press(modifiersOf(e.nativeEvent), keyOf(e.nativeEvent));
  };

  const onKeyUp = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (!active()) return;
    pressed.current.delete(e.code);
    lift(modifiersOf(e.nativeEvent), pressed.current.size === 0);
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
