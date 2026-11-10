import { Pencil, Plus, RotateCcw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { DashboardService } from "@bindings/core";
import {
  Service,
  type FileIndexStatus,
  type Keyword,
  type Settings,
  type SettingsState,
} from "@bindings/modules/launcher";
import { KeepReadySelect, Row, Section } from "@/components/settings";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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

  /** Saves a change, resolving to whether it was accepted. */
  const save = (patch: Partial<Settings>) =>
    Service.SetSettings({ ...own.settings, ...patch }).then(
      (next) => {
        setOwn(next);
        return true;
      },
      (err: unknown) => {
        toast.error(errorMessage(err));
        return false;
      },
    );

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
        <Row
          title="搜索文件和文件夹"
          description={
            own.platform === "darwin"
              ? "用聚焦搜索的索引按名称查找个人文件夹里的文件，最近打开过的排在前面。"
              : "按名称查找所有硬盘上的文件，支持 Everything 的搜索语法，如 *.pdf、ext:docx、folder:、!排除词、a | b。"
          }
        >
          <Switch
            checked={own.settings.files}
            onCheckedChange={(files) => {
              void save({ files });
            }}
          />
        </Row>
        <Row
          title="本币"
          description="输入“100 usd”这样的金额时换算成这种货币，也会给出美元或欧元。汇率来自欧洲央行，每个工作日更新。"
        >
          <Select
            value={own.settings.currency}
            onValueChange={(currency) => {
              void save({ currency });
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

      {own.platform === "windows" && <FileIndexSettings own={own} save={save} />}

      <KeywordSettings own={own} save={save} />

      <Section title="面板">
        <Row
          title="隐藏后保持就绪"
          description="启动后或面板隐藏后，在后台保留这么久，期间随叫随到；之后释放内存，下次打开需要重新加载。"
        >
          <KeepReadySelect
            minutes={own.settings.keepMinutes}
            onChange={(keepMinutes) => {
              void save({ keepMinutes });
            }}
          />
        </Row>
      </Section>
    </>
  );
}

type Save = (patch: Partial<Settings>) => Promise<boolean>;

/** The Windows file index: its service and the folders it leaves out. */
function FileIndexSettings({ own, save }: { own: SettingsState; save: Save }) {
  const [status, setStatus] = useState<FileIndexStatus | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    const poll = () => {
      Service.FileIndex().then(
        (next) => {
          if (live) setStatus(next);
        },
        () => undefined,
      );
    };
    poll();
    const timer = setInterval(poll, 1500);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, []);

  const change = (install: boolean) => {
    setBusy(true);
    (install ? Service.InstallFileIndex() : Service.RemoveFileIndex())
      .then(setStatus, (err: unknown) => toast.error(errorMessage(err)))
      .finally(() => {
        setBusy(false);
      });
  };

  if (!status) return null;
  const files = status.files.toLocaleString();
  const drives = (status.volumes ?? []).join("  ");
  const needsInstall =
    status.state === "missing" || status.state === "stopped" || status.state === "outdated";
  const description = {
    off: "启用启动器后开始索引。",
    missing: "索引服务以管理员权限读取各硬盘的文件表并跟随变更，只提供文件名。安装时需要授权一次。",
    stopped: "索引服务已安装但没有运行，重新安装可以修复。",
    outdated: "索引服务和当前版本不一致，需要重新安装。",
    connecting: "正在连接索引服务……",
    indexing: `正在建立索引，已收录 ${files} 项。`,
    ready: `已索引 ${files} 个文件和文件夹，变更实时更新。${drives && `（${drives}）`}`,
  }[status.state];

  return (
    <Section title="文件索引">
      <Row title="索引服务" description={description} note={status.message}>
        {needsInstall && (
          <Button
            size="sm"
            disabled={busy}
            onClick={() => {
              change(true);
            }}
          >
            {status.state === "missing" ? "安装" : "重新安装"}
          </Button>
        )}
        {(status.state === "ready" || status.state === "indexing" || status.state === "connecting") && (
          <Button
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => {
              change(false);
            }}
          >
            移除
          </Button>
        )}
      </Row>
      <ExcludeList
        value={own.settings.fileExcludes ?? []}
        onSave={(fileExcludes) => save({ fileExcludes })}
      />
    </Section>
  );
}

/** Edits the excluded folders, one per line, saving when the field loses focus. */
function ExcludeList({ value, onSave }: { value: string[]; onSave: (list: string[]) => Promise<boolean> }) {
  const text = value.join("\n");
  const [draft, setDraft] = useState(text);
  const commit = () => {
    const list = draft
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
    if (list.join("\n") === text) return;
    void onSave(list).then((ok) => {
      if (!ok) setDraft(text);
    });
  };
  return (
    <div className="space-y-2 px-4 py-3">
      <div className="space-y-0.5">
        <div className="font-medium">排除的文件夹</div>
        <p className="text-[13px] text-muted-foreground">
          每行一个。完整路径只排除那个文件夹，单独的名称排除所有同名文件夹。修改后会重新建立索引。
        </p>
      </div>
      <textarea
        value={draft}
        rows={Math.max(3, value.length + 1)}
        spellCheck={false}
        onChange={(e) => {
          setDraft(e.target.value);
        }}
        onBlur={commit}
        className="block w-full resize-y rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
      />
    </div>
  );
}

/** The keywords: built-in ones, the default search and the websites. */
function KeywordSettings({ own, save }: { own: SettingsState; save: Save }) {
  const s = own.settings;
  const sites = s.keywords ?? [];
  // The row being edited: an index into sites, or -1 for a new one.
  const [editing, setEditing] = useState<number | null>(null);

  /** Saves the websites; the default search follows its keyword when renamed or removed. */
  const saveSites = (next: Keyword[], from: string, to: string) => {
    let defaultSearch = s.defaultSearch;
    if (from !== "" && defaultSearch.toLowerCase() === from.toLowerCase()) defaultSearch = to;
    return save({ keywords: next, defaultSearch });
  };

  // The user's own websites stay; the ones the launcher came with come back as they were.
  const restore = () => {
    const defaults = own.defaultKeywords ?? [];
    const words = new Set(defaults.map((k) => k.keyword.toLowerCase()));
    setEditing(null);
    void save({
      keywords: [...defaults, ...sites.filter((k) => !words.has(k.keyword.toLowerCase()))],
      defaultSearch: s.defaultSearch || "gg",
    });
  };

  return (
    <>
      <Section title="关键词">
        <Row title="默认搜索" description="任何输入的最后一行都提供用它搜索，回车在浏览器里打开。">
          <Select
            value={s.defaultSearch || "none"}
            onValueChange={(v) => {
              void save({ defaultSearch: v === "none" ? "" : v });
            }}
          >
            <SelectTrigger size="sm" className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">不提供</SelectItem>
              {sites.map((k) => (
                <SelectItem key={k.keyword} value={k.keyword}>
                  {k.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
        <Row
          title="翻译"
          description={`输入“${s.translateKeyword || "fy"} 文字”，总是把后面的内容翻译成目标语言。留空关闭。`}
        >
          <WordInput value={s.translateKeyword} onSave={(translateKeyword) => save({ translateKeyword })} />
        </Row>
        <Row
          title="搜索文件"
          description={`输入“${s.filesKeyword || "f"} 名称”，只列出文件和文件夹，最多 20 个；关掉上面的自动搜索文件时也能用。留空关闭。`}
        >
          <WordInput value={s.filesKeyword} onSave={(filesKeyword) => save({ filesKeyword })} />
        </Row>
      </Section>

      <Section title="网站">
        <p className="px-4 pt-3 pb-1 text-[13px] text-muted-foreground">
          在启动器里输入关键词和空格，关键词变成标签，再输入内容、回车，就在默认浏览器里搜索；不输入内容直接回车打开网站首页。
        </p>
        {sites.map((k, i) =>
          editing === i ? (
            <KeywordForm
              key={k.keyword}
              initial={k}
              onCancel={() => {
                setEditing(null);
              }}
              onSave={async (next) => {
                if (
                  await saveSites(
                    sites.map((old, j) => (j === i ? next : old)),
                    k.keyword,
                    next.keyword,
                  )
                )
                  setEditing(null);
              }}
            />
          ) : (
            <Row
              key={k.keyword}
              title={
                <>
                  {k.name}
                  <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] font-normal text-muted-foreground">
                    {k.keyword}
                  </span>
                </>
              }
              description={<span className="block truncate font-mono text-[12px]">{k.url}</span>}
            >
              <Button
                variant="ghost"
                size="icon-sm"
                title="编辑"
                aria-label={`编辑${k.name}`}
                onClick={() => {
                  setEditing(i);
                }}
              >
                <Pencil />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                title="删除"
                aria-label={`删除${k.name}`}
                onClick={() => {
                  setEditing(null);
                  void saveSites(
                    sites.filter((_, j) => j !== i),
                    k.keyword,
                    "",
                  );
                }}
              >
                <Trash2 />
              </Button>
            </Row>
          ),
        )}
        {editing === -1 ? (
          <KeywordForm
            initial={{ keyword: "", name: "", url: "" }}
            onCancel={() => {
              setEditing(null);
            }}
            onSave={async (next) => {
              if (await saveSites([...sites, next], "", "")) setEditing(null);
            }}
          />
        ) : (
          <div className="flex items-center gap-2 px-4 py-2.5">
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setEditing(-1);
              }}
            >
              <Plus />
              添加网站
            </Button>
            <span className="flex-1" />
            <Button variant="ghost" size="sm" title="自己添加的网站会保留" onClick={restore}>
              <RotateCcw />
              恢复默认网站
            </Button>
          </div>
        )}
      </Section>
    </>
  );
}

/** Edits a website keyword. Enter saves, Esc cancels. */
function KeywordForm({
  initial,
  onSave,
  onCancel,
}: {
  initial: Keyword;
  onSave: (k: Keyword) => Promise<void>;
  onCancel: () => void;
}) {
  const [k, setK] = useState(initial);
  const field = (name: keyof Keyword, label: string, placeholder: string, className: string) => (
    <label className={className}>
      <span className="mb-1 block text-[12px] text-muted-foreground">{label}</span>
      <Input
        value={k[name]}
        placeholder={placeholder}
        spellCheck={false}
        autoComplete="off"
        onChange={(e) => {
          setK({ ...k, [name]: e.target.value });
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") void onSave(k);
          else if (e.key === "Escape") onCancel();
        }}
        className="h-8 text-[13px]"
      />
    </label>
  );
  return (
    <div className="space-y-3 bg-muted/30 px-4 py-3">
      <div className="flex gap-2">
        {field("name", "名称", "Google", "w-40")}
        {field("keyword", "关键词", "gg", "w-32")}
      </div>
      {field(
        "url",
        "搜索网址，用 {query} 标出搜索内容的位置",
        "https://www.google.com/search?q={query}",
        "block",
      )}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          取消
        </Button>
        <Button size="sm" onClick={() => void onSave(k)}>
          保存
        </Button>
      </div>
    </div>
  );
}

/** Edits a one-word keyword, saving it when the field loses focus or Enter is pressed. */
function WordInput({ value, onSave }: { value: string; onSave: (word: string) => Promise<boolean> }) {
  const [draft, setDraft] = useState(value);
  const commit = () => {
    const word = draft.trim();
    if (word === value) return;
    void onSave(word).then((ok) => {
      if (!ok) setDraft(value);
    });
  };
  return (
    <Input
      value={draft}
      placeholder="已关闭"
      spellCheck={false}
      autoComplete="off"
      onChange={(e) => {
        setDraft(e.target.value);
      }}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
      }}
      className="h-8 w-24 font-mono text-xs"
    />
  );
}
