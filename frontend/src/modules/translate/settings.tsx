import { Browser } from "@wailsio/runtime";
import { ExternalLink } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { DashboardService } from "@bindings/core";
import { Service, type Settings, type SettingsState } from "@bindings/modules/translate";
import { KeepReadySelect, Row, Section } from "@/components/settings";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { Mutate, State } from "@/lib/dashboard";
import { errorMessage } from "@/lib/errors";
import { languageName, languages } from "@/modules/translate/languages";

export function TranslateSettingsPage({ state, mutate }: { state: State; mutate: Mutate }) {
  const status = state.modules?.find((m) => m.id === "translate");
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
      reload();
    });
  };

  return (
    <>
      <Section title="模块">
        <Row title="启用翻译" description={status.description} note={status.error}>
          <Switch
            checked={status.enabled}
            disabled={!status.supported}
            onCheckedChange={(on) => void mutate(() => DashboardService.SetModuleEnabled(status.id, on))}
          />
        </Row>
      </Section>

      <Section title="语言">
        <Row title="原文语言" description="一般选自动检测。浮窗里也可以随时切换。">
          <LanguageSelect
            allowAuto
            value={own.settings.source}
            onChange={(source) => {
              save({ source });
            }}
          />
        </Row>
        <Row title="目标语言" description="译成这种语言。">
          <LanguageSelect
            value={own.settings.target}
            onChange={(target) => {
              save({ target });
            }}
          />
        </Row>
        <Row title="第二目标语言" description="自动检测到原文已经是目标语言时，改译成这种语言。">
          <LanguageSelect
            value={own.settings.second}
            onChange={(second) => {
              save({ second });
            }}
          />
        </Row>
      </Section>

      <Section title="翻译服务">
        <Row title="翻译服务" description="浮窗里也可以随时切换。填写 DeepL 密钥后才能选 DeepL。">
          <Select
            value={own.settings.engine}
            onValueChange={(engine) => {
              save({ engine });
            }}
          >
            <SelectTrigger size="sm" className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="google">Google 翻译</SelectItem>
              <SelectItem value="deepl" disabled={own.settings.deeplKey === ""}>
                DeepL
              </SelectItem>
            </SelectContent>
          </Select>
        </Row>
        <Row
          title="DeepL API 密钥"
          description="注册 DeepL API Free 即可免费获取，每月 50 万字符。密钥只保存在本机的配置文件里。"
        >
          {/* Keyed by the saved key, so the field starts over after each save. */}
          <DeepLKeyInput
            key={own.settings.deeplKey}
            value={own.settings.deeplKey}
            onSave={(deeplKey) => {
              // Without a key DeepL cannot stay the engine.
              save(deeplKey === "" ? { deeplKey, engine: "google" } : { deeplKey });
            }}
          />
          <Button
            variant="outline"
            size="sm"
            onClick={() => void Browser.OpenURL("https://www.deepl.com/pro-api")}
          >
            <ExternalLink />
            获取
          </Button>
        </Row>
      </Section>

      {own.platform === "darwin" && (
        <Section title="权限">
          <Row title="辅助功能" description="划词翻译用它读取其他程序里选中的文字。">
            <PermissionState granted={own.accessibility} permission="accessibility" />
          </Row>
          <Row
            title="屏幕录制"
            description="截图翻译用它读取屏幕上的文字。授权后需要重新打开 arubox 才生效。"
          >
            <PermissionState granted={own.screenCapture} permission="screen" />
          </Row>
        </Section>
      )}

      <Section title="浮窗">
        <Row
          title="隐藏后保持就绪"
          description="启动后或浮窗隐藏后，在后台保留这么久，期间随叫随到；之后释放内存，下次打开需要重新加载。"
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

function PermissionState({ granted, permission }: { granted: boolean; permission: string }) {
  if (granted) return <Badge variant="secondary">已授权</Badge>;
  return (
    <Button variant="outline" size="sm" onClick={() => void Service.OpenPermissionSettings(permission)}>
      <ExternalLink />
      去系统设置授权
    </Button>
  );
}

/** Edits the key and saves it when the field loses focus or Enter is pressed. */
function DeepLKeyInput({ value, onSave }: { value: string; onSave: (key: string) => void }) {
  const [draft, setDraft] = useState(value);
  const commit = () => {
    const key = draft.trim();
    if (key !== value) onSave(key);
  };
  return (
    <Input
      type="password"
      value={draft}
      placeholder="未填写"
      spellCheck={false}
      autoComplete="off"
      onChange={(e) => {
        setDraft(e.target.value);
      }}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
      }}
      className="h-8 w-56 font-mono text-xs"
    />
  );
}

function LanguageSelect({
  allowAuto,
  value,
  onChange,
}: {
  /** Offers automatic detection, for the source language. */
  allowAuto?: boolean;
  value: string;
  onChange: (code: string) => void;
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger size="sm" className="w-32">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {allowAuto && <SelectItem value="auto">自动检测</SelectItem>}
        {value !== "auto" && !languages.some((l) => l.code === value) && (
          <SelectItem value={value}>{languageName(value)}</SelectItem>
        )}
        {languages.map((l) => (
          <SelectItem key={l.code} value={l.code}>
            {l.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
