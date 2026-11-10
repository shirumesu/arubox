import { FolderOpen } from "lucide-react";
import { toast } from "sonner";

import { DashboardService } from "@bindings/core";
import { Row, Section } from "@/components/settings";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import type { Mutate, State } from "@/lib/dashboard";
import { errorMessage } from "@/lib/errors";

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

      <Section title="故障排查">
        <Row title="日志" description="运行记录保存在日志目录中，出问题时可以从这里查看。">
          <Button variant="outline" size="sm" onClick={openLogs}>
            <FolderOpen />
            打开日志目录
          </Button>
        </Row>
      </Section>

      <Section title="关于">
        <Row title="版本">
          <span className="text-muted-foreground tabular-nums">{state.version}</span>
        </Row>
      </Section>
    </>
  );
}
