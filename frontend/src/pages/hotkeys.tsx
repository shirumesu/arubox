import { RotateCcw, X } from "lucide-react";
import type { ReactNode } from "react";

import { DashboardService } from "@bindings/core";
import { HotkeyRecorder } from "@/components/hotkey-recorder";
import { Row, Section } from "@/components/settings";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { Platform } from "@/lib/accelerator";
import type { HotkeyStatus, Mutate, State } from "@/lib/dashboard";

export function HotkeysPage({ state, mutate }: { state: State; mutate: Mutate }) {
  const platform = state.platform as Platform;
  const groups = new Map<string, HotkeyStatus[]>();
  for (const h of state.hotkeys ?? []) {
    groups.set(h.ownerName, [...(groups.get(h.ownerName) ?? []), h]);
  }

  return (
    <>
      <p className="px-1 text-[13px] text-muted-foreground">
        全局快捷键在任何程序里都能用。点击一个快捷键，按下新的组合即可修改，按 Esc 取消。
      </p>
      {[...groups].map(([owner, hotkeys]) => (
        <Section key={owner} title={owner}>
          {hotkeys.map((h) => (
            <HotkeyRow key={h.id} hotkey={h} platform={platform} mutate={mutate} />
          ))}
        </Section>
      ))}
    </>
  );
}

function HotkeyRow({
  hotkey: h,
  platform,
  mutate,
}: {
  hotkey: HotkeyStatus;
  platform: Platform;
  mutate: Mutate;
}) {
  const set = (accelerator: string, force = false): Promise<boolean> =>
    mutate(
      () => DashboardService.SetHotkey(h.id, accelerator, force),
      (err) => {
        const c = conflictOf(err);
        if (!c || force) return undefined;
        const run = () => void set(accelerator, true);
        return c.kind === "hotkey"
          ? { label: "改用这里", description: `「${c.name}」的快捷键会被清除。`, run }
          : {
              label: "仍然使用",
              description: `「${c.name}」在所有程序里都会失效；系统抢先处理的组合可能仍然触发不了。`,
              run,
            };
      },
    );

  return (
    <Row
      title={
        <>
          {h.label}
          {!h.active && <Badge variant="secondary">模块未启用</Badge>}
        </>
      }
      note={h.error}
    >
      <HotkeyRecorder value={h.accelerator} platform={platform} onRecord={(accel) => set(accel)} />
      <IconAction
        label="恢复默认"
        hidden={h.accelerator === h.default}
        onClick={() => void mutate(() => DashboardService.ResetHotkey(h.id))}
      >
        <RotateCcw />
      </IconAction>
      <IconAction label="清除" hidden={h.accelerator === ""} onClick={() => void set("")}>
        <X />
      </IconAction>
    </Row>
  );
}

/**
 * The hotkey or system shortcut that already uses a combination, from the
 * hotkey.Conflict a failed SetHotkey carries as its cause.
 */
function conflictOf(err: unknown): { kind: "hotkey" | "system"; name: string } | null {
  if (!(err instanceof Error) || typeof err.cause !== "object" || err.cause === null) return null;
  const c = err.cause as { hotkey?: string; system?: string };
  if (c.hotkey) return { kind: "hotkey", name: c.hotkey };
  if (c.system) return { kind: "system", name: c.system };
  return null;
}

/** An icon button that keeps its space when hidden, so rows stay aligned. */
function IconAction({
  label,
  hidden,
  onClick,
  children,
}: {
  label: string;
  hidden: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  if (hidden) return <div className="size-8" />;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={label} onClick={onClick}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
