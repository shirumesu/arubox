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
      <HotkeyRecorder
        value={h.accelerator}
        platform={platform}
        onRecord={(accel) => mutate(() => DashboardService.SetHotkey(h.id, accel))}
      />
      <IconAction
        label="恢复默认"
        hidden={h.accelerator === h.default}
        onClick={() => void mutate(() => DashboardService.ResetHotkey(h.id))}
      >
        <RotateCcw />
      </IconAction>
      <IconAction
        label="清除"
        hidden={h.accelerator === ""}
        onClick={() => void mutate(() => DashboardService.SetHotkey(h.id, ""))}
      >
        <X />
      </IconAction>
    </Row>
  );
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
