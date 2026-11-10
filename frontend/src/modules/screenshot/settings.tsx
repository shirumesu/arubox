import { ExternalLink } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { DashboardService } from "@bindings/core";
import { Service, type Settings, type SettingsState } from "@bindings/modules/screenshot";
import { KeepReadySelect, Row, Section } from "@/components/settings";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import type { Mutate, State } from "@/lib/dashboard";
import { errorMessage } from "@/lib/errors";

export function ScreenshotSettingsPage({ state, mutate }: { state: State; mutate: Mutate }) {
  const status = state.modules?.find((m) => m.id === "screenshot");
  const [own, setOwn] = useState<SettingsState | null>(null);

  const reload = useCallback(() => {
    Service.State().then(setOwn, (err: unknown) => toast.error(errorMessage(err)));
  }, []);

  // The permission is granted in System Settings; check again on return.
  useEffect(() => {
    reload();
    window.addEventListener("focus", reload);
    return () => {
      window.removeEventListener("focus", reload);
    };
  }, [reload]);

  if (!status || !own) return null;

  const save = (patch: Partial<Settings>) => {
    Service.SetSettings({ ...own.settings, ...patch }).then(setOwn, (err: unknown) => {
      toast.error(errorMessage(err));
    });
  };

  return (
    <>
      <Section title="模块">
        <Row title="启用截图" description={status.description} note={status.error}>
          <Switch
            checked={status.enabled}
            disabled={!status.supported}
            onCheckedChange={(on) => void mutate(() => DashboardService.SetModuleEnabled(status.id, on))}
          />
        </Row>
      </Section>

      {own.platform === "darwin" && (
        <Section title="权限">
          <Row title="屏幕录制" description="截图需要它读取屏幕画面。授权后需要重新打开 arubox 才生效。">
            {own.screenCapture ? (
              <Badge variant="secondary">已授权</Badge>
            ) : (
              <Button variant="outline" size="sm" onClick={() => void Service.OpenScreenCaptureSettings()}>
                <ExternalLink />
                去系统设置授权
              </Button>
            )}
          </Row>
        </Section>
      )}

      <Section title="编辑器">
        <Row
          title="隐藏后保持就绪"
          description="启动后或截图编辑器关闭后，在后台保留这么久，期间随叫随到；之后释放内存，下次截图需要重新加载。"
        >
          <KeepReadySelect
            minutes={own.settings.keepMinutes}
            onChange={(keepMinutes) => {
              save({ keepMinutes });
            }}
          />
        </Row>
      </Section>
    </>
  );
}
