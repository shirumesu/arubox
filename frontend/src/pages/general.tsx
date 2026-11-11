import { FolderOpen, Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { DashboardService } from "@bindings/core";
import { Phase, Service as UpdateService } from "@bindings/update";
import { Row, Section } from "@/components/settings";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import type { Mutate, State } from "@/lib/dashboard";
import { errorMessage } from "@/lib/errors";
import { useUpdate, type UpdateState } from "@/lib/update";

export function GeneralPage({ state, mutate }: { state: State; mutate: Mutate }) {
  const openLogs = () => {
    DashboardService.OpenLogFolder().catch((err: unknown) => toast.error(errorMessage(err)));
  };

  return (
    <>
      <Section title="启动">
        <Row title="登录时启动" description="登录系统后自动在后台运行。" note={state.autostart.error}>
          <Switch
            checked={state.autostart.enabled}
            onCheckedChange={(on) => void mutate(() => DashboardService.SetAutostart(on))}
          />
        </Row>
      </Section>

      <UpdateSettings />

      <Section title="故障排查">
        <Row title="日志" description="运行记录保存在日志目录中，出问题时可以从这里查看。">
          <Button variant="outline" size="sm" onClick={openLogs}>
            <FolderOpen />
            打开日志目录
          </Button>
        </Row>
      </Section>
    </>
  );
}

function UpdateSettings() {
  const update = useUpdate();
  if (!update) return null;
  const { state, show } = update;
  const offered = [
    Phase.PhaseAvailable,
    Phase.PhaseDownloading,
    Phase.PhaseInstalling,
    Phase.PhaseRestarting,
  ];
  const note = [state.error, state.leftover].filter(Boolean).join("；");

  return (
    <Section title="更新">
      <Row
        title={<span className="tabular-nums">arubox {state.version}</span>}
        description={updateStatus(state)}
        note={note}
      >
        {state.unavailable ? null : offered.includes(state.phase) ? (
          <Button size="sm" onClick={show}>
            查看新版本
          </Button>
        ) : (
          <Button
            variant="outline"
            size="sm"
            disabled={state.phase === Phase.PhaseChecking}
            onClick={() => void UpdateService.Check()}
          >
            {state.phase === Phase.PhaseChecking ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            检查更新
          </Button>
        )}
      </Row>
      <Row
        title="接收测试版"
        description="除了正式版，也提示 alpha、beta 等测试版本。测试版更早带来新功能，但可能不够稳定。"
      >
        <Switch
          checked={state.prereleases}
          disabled={!!state.unavailable}
          onCheckedChange={(on) => {
            UpdateService.SetPrereleases(on).catch((err: unknown) => toast.error(errorMessage(err)));
          }}
        />
      </Row>
    </Section>
  );
}

function updateStatus(state: UpdateState): string {
  if (state.unavailable) return state.unavailable;
  const release = state.release?.version;
  switch (state.phase) {
    case Phase.PhaseChecking:
      return "正在检查更新…";
    case Phase.PhaseLatest:
      return `已是最新版本（${checkedAt(state.checkedAt)} 检查）`;
    case Phase.PhaseAvailable:
      return `有新版本 ${release}`;
    case Phase.PhaseDownloading:
      return `正在下载 ${release}…`;
    case Phase.PhaseInstalling:
      return `正在安装 ${release}…`;
    case Phase.PhaseRestarting:
      return `${release} 已安装，正在重启…`;
    default:
      return "启动时和之后每隔几小时自动检查一次。";
  }
}

/** When a check finished, as "今天 14:02" or "11月10日 14:02". */
function checkedAt(ms: number): string {
  const at = new Date(ms);
  const time = at.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  if (at.toDateString() === new Date().toDateString()) return `今天 ${time}`;
  return `${at.getMonth() + 1}月${at.getDate()}日 ${time}`;
}
