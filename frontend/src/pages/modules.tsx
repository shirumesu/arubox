import { Blocks } from "lucide-react";

import { DashboardService } from "@bindings/core";
import { Empty, Row, Section } from "@/components/settings";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import type { Mutate, State } from "@/lib/dashboard";

export function ModulesPage({ state, mutate }: { state: State; mutate: Mutate }) {
  const modules = state.modules ?? [];

  if (modules.length === 0) {
    return (
      <Empty icon={<Blocks />} title="还没有模块">
        工具模块会列在这里。每个模块单独开关，未启用的模块不占内存、不注册快捷键。
      </Empty>
    );
  }

  return (
    <Section title="全部模块">
      {modules.map((m) => (
        <Row
          key={m.id}
          title={
            <>
              {m.name}
              {!m.supported && <Badge variant="secondary">此系统不可用</Badge>}
            </>
          }
          description={m.description}
          note={m.error}
        >
          <Switch
            checked={m.enabled}
            disabled={!m.supported}
            onCheckedChange={(on) => void mutate(() => DashboardService.SetModuleEnabled(m.id, on))}
          />
        </Row>
      ))}
    </Section>
  );
}
