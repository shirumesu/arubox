import { useEffect, useState } from "react";
import { toast } from "sonner";

import { DashboardService } from "@bindings/core";
import { Service, type Settings, type SettingsState } from "@bindings/modules/launcher";
import { KeepReadySelect, Row, Section } from "@/components/settings";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { Mutate, State } from "@/lib/dashboard";
import { errorMessage } from "@/lib/errors";

export function LauncherSettingsPage({ state, mutate }: { state: State; mutate: Mutate }) {
  const status = state.modules?.find((m) => m.id === "launcher");
  const [own, setOwn] = useState<SettingsState | null>(null);

  useEffect(() => {
    Service.State().then(setOwn, (err: unknown) => toast.error(errorMessage(err)));
  }, []);

  if (!status || !own) return null;

  const save = (patch: Partial<Settings>) => {
    Service.SetSettings({ ...own.settings, ...patch }).then(setOwn, (err: unknown) => {
      toast.error(errorMessage(err));
    });
  };

  return (
    <>
      <Section title="模块">
        <Row title="启用启动器" description={status.description} note={status.error}>
          <Switch
            checked={status.enabled}
            disabled={!status.supported}
            onCheckedChange={(on) => void mutate(() => DashboardService.SetModuleEnabled(status.id, on))}
          />
        </Row>
      </Section>

      <Section title="搜索">
        {own.platform === "darwin" && (
          <Row
            title="搜索文件和文件夹"
            description="用聚焦搜索的索引按名称查找个人文件夹里的文件，最近打开过的排在前面。"
          >
            <Switch
              checked={own.settings.files}
              onCheckedChange={(files) => {
                save({ files });
              }}
            />
          </Row>
        )}
        <Row
          title="本币"
          description="输入“100 usd”这样的金额时换算成这种货币，也会给出美元或欧元。汇率来自欧洲央行，每个工作日更新。"
        >
          <Select
            value={own.settings.currency}
            onValueChange={(currency) => {
              save({ currency });
            }}
          >
            <SelectTrigger size="sm" className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {own.currencies?.map((c) => (
                <SelectItem key={c} value={c}>
                  {c}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
      </Section>

      <Section title="面板">
        <Row
          title="隐藏后保持就绪"
          description="面板隐藏后在后台保留这么久，期间随叫随到；之后释放内存，下次打开需要重新加载。"
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
