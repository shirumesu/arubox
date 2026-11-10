import { useEffect, useState } from "react";
import { toast } from "sonner";

import { DashboardService } from "@bindings/core";
import { Service, type Settings, type SettingsState } from "@bindings/modules/desktop";
import { Row, Section } from "@/components/settings";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { Mutate, State } from "@/lib/dashboard";
import { errorMessage } from "@/lib/errors";

const iconSizes = [32, 40, 48, 56, 64, 72, 80, 96];
const opacities = [10, 20, 30, 40, 45, 50, 60, 70, 80, 90];

export function DesktopSettingsPage({ state, mutate }: { state: State; mutate: Mutate }) {
  const status = state.modules?.find((m) => m.id === "desktop");
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
        <Row title="启用桌面整理" description={status.description} note={status.error}>
          <Switch
            checked={status.enabled}
            disabled={!status.supported}
            onCheckedChange={(on) => void mutate(() => DashboardService.SetModuleEnabled(status.id, on))}
          />
        </Row>
      </Section>

      <Section title="外观">
        <Row title="图标大小" description="也可以在桌面上按住 Ctrl 滚动滚轮调整。">
          <Select
            value={String(own.settings.iconSize)}
            onValueChange={(v) => {
              save({ iconSize: Number(v) });
            }}
          >
            <SelectTrigger className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {iconSizes.map((s) => (
                <SelectItem key={s} value={String(s)}>
                  {s} 像素
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
        <Row title="盒子不透明度" description="盒子背景的不透明程度，越低越能透出壁纸。">
          <Select
            value={String(own.settings.opacity)}
            onValueChange={(v) => {
              save({ opacity: Number(v) });
            }}
          >
            <SelectTrigger className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {opacities.map((o) => (
                <SelectItem key={o} value={String(o)}>
                  {o}%
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
      </Section>

      <Section title="行为">
        <Row title="双击桌面隐藏图标" description="在桌面空白处双击，隐藏或重新显示所有图标和盒子。">
          <Switch
            checked={own.settings.doubleClickHide}
            onCheckedChange={(doubleClickHide) => {
              save({ doubleClickHide });
            }}
          />
        </Row>
        <Row title="对齐吸附" description="移动或调整盒子时，贴齐屏幕边缘和其他盒子。">
          <Switch
            checked={own.settings.snap}
            onCheckedChange={(snap) => {
              save({ snap });
            }}
          />
        </Row>
        <Row title="悬停展开" description="鼠标移到卷起的盒子标题上时临时展开，移开后收起。">
          <Switch
            checked={own.settings.hoverExpand}
            onCheckedChange={(hoverExpand) => {
              save({ hoverExpand });
            }}
          />
        </Row>
      </Section>
    </>
  );
}
