import { Clipboard, Events } from "@wailsio/runtime";
import {
  Banknote,
  Calculator,
  Clock,
  FolderSearch,
  Globe,
  Languages,
  Search,
  SquareTerminal,
  Tag,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";

import { Service, type Result, type SettingsState } from "@bindings/modules/launcher";
import { languageName } from "@/modules/translate/languages";
import { cn } from "cn";

/** Waits this long after typing stops before the slower search for files and rates. */
const moreDelay = 120;
/** Translates only once typing has paused, so a sentence is not sent word by word. */
const translateDelay = 450;

interface Delayed {
  timer?: ReturnType<typeof setTimeout>;
  call?: { cancel: () => unknown };
}

/**
 * A keyword in use, shown as a tag before the query: a website to search,
 * or translating or searching files whatever the query looks like.
 */
interface Chip {
  kind: "web" | "translate" | "files";
  word: string;
  name: string;
}

const chipIcons: Record<Chip["kind"], LucideIcon> = { web: Globe, translate: Languages, files: FolderSearch };

/** Every keyword the settings define, website ones last. */
function keywords(own: SettingsState | null): Chip[] {
  const s = own?.settings;
  if (!s) return [];
  const all: Chip[] = [];
  if (s.translateKeyword) all.push({ kind: "translate", word: s.translateKeyword, name: "翻译" });
  // Windows has no file index yet.
  if (s.filesKeyword && own.platform === "darwin") {
    all.push({ kind: "files", word: s.filesKeyword, name: "搜索文件" });
  }
  for (const k of s.keywords ?? []) all.push({ kind: "web", word: k.keyword, name: k.name });
  return all;
}

function findChip(own: SettingsState | null, word: string): Chip | undefined {
  const w = word.toLowerCase();
  return keywords(own).find((c) => c.word.toLowerCase() === w);
}

/** The row that searches a website for query, or opens it when query is empty. */
function webRow(chip: Chip, query: string): Result {
  const q = query.trim();
  return {
    kind: "web",
    key: chip.word,
    title: q,
    subtitle: q ? `用 ${chip.name} 搜索` : `打开 ${chip.name}`,
  };
}

/** Rows offering the keywords that start with what is typed, to pick with Tab. */
function suggestions(own: SettingsState | null, query: string): Result[] {
  const q = query.trim().toLowerCase();
  if (q.length < 2 || /\s/.test(q)) return [];
  return keywords(own)
    .filter((c) => c.word.toLowerCase().startsWith(q))
    .slice(0, 3)
    .map((c) => ({ kind: "keyword", key: c.word, title: c.name, subtitle: `关键词 ${c.word}` }));
}

const chipPlaceholders: Record<Chip["kind"], string> = {
  web: "输入要搜索的内容",
  translate: "输入要翻译的文字",
  files: "输入文件或文件夹的名称",
};

export function Launcher() {
  const [query, setQuery] = useState("");
  const [instant, setInstant] = useState<Result[]>([]);
  const [more, setMore] = useState<Result[]>([]);
  const [translation, setTranslation] = useState<Result[]>([]);
  const [selected, setSelected] = useState(0);
  const [own, setOwn] = useState<SettingsState | null>(null);
  const [chip, setChip] = useState<Chip | null>(null);
  // Shows that a calculation was copied, until the next keystroke.
  const [copied, setCopied] = useState(false);
  // Bumped for every open, so the panel is presented once per open.
  const [opened, setOpened] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  // Numbers the height reports, which can reach Go out of order.
  const heightReports = useRef(0);
  // Only the latest query's results may show.
  const searches = useRef(0);
  const delayed = useRef<Delayed[]>([]);
  // The chip, for reopening, which should not wait for a render.
  const chipRef = useRef<Chip | null>(null);

  const mac = (own?.platform ?? "darwin") === "darwin";
  const q = query.trim();
  const fallbackKeyword =
    !chip && q !== "" && !q.startsWith("!") ? findChip(own, own?.settings.defaultSearch ?? "") : undefined;
  const results = [
    ...instant,
    ...(chip ? [] : suggestions(own, query)),
    ...translation,
    ...more,
    ...(fallbackKeyword ? [webRow(fallbackKeyword, q)] : []),
  ];

  const search = useCallback((q: string, chip: Chip | null) => {
    const n = ++searches.current;
    for (const d of delayed.current) {
      clearTimeout(d.timer);
      d.call?.cancel();
    }
    delayed.current = [];
    // The slower searches run once typing pauses, and a newer query cancels them.
    const later = (
      delay: number,
      fetch: (q: string) => Promise<Result[] | null> & { cancel: () => unknown },
      set: (r: Result[]) => void,
    ) => {
      const d: Delayed = {};
      d.timer = setTimeout(() => {
        const call = fetch(q);
        d.call = call;
        call.then((r) => {
          if (n === searches.current) set(r ?? []);
        }, console.error);
      }, delay);
      delayed.current.push(d);
    };
    if (chip) {
      setInstant(chip.kind === "web" ? [webRow(chip, q)] : []);
      setMore([]);
      setTranslation([]);
      setSelected(0);
      if (chip.kind === "translate" && q.trim() !== "") {
        later(translateDelay, (q) => Service.Translate(q, true), setTranslation);
      } else if (chip.kind === "files" && q.trim() !== "") {
        later(moreDelay, Service.Files, setMore);
      }
      return;
    }
    Service.Search(q).then((r) => {
      if (n !== searches.current) return;
      setInstant(r ?? []);
      setMore([]);
      setTranslation([]);
      setSelected(0);
    }, console.error);
    later(moreDelay, Service.SearchMore, setMore);
    later(translateDelay, (q) => Service.Translate(q, false), setTranslation);
  }, []);

  const applyChip = (c: Chip | null, rest: string) => {
    chipRef.current = c;
    setChip(c);
    setQuery(rest);
    search(rest, c);
  };

  useEffect(() => {
    const off = Events.On("launcher:open", () => {
      setOpened((n) => n + 1);
    });
    Service.Attach().then((pending) => {
      if (pending) setOpened((n) => n + 1);
    }, console.error);
    return off;
  }, []);

  // On every open: select the last query, so typing replaces it, refresh its
  // results and show the panel.
  useLayoutEffect(() => {
    if (opened === 0 || !contentRef.current) return;
    // The keywords may have changed in the dashboard.
    Service.State().then(setOwn, console.error);
    inputRef.current?.focus();
    inputRef.current?.select();
    search(inputRef.current?.value ?? "", chipRef.current);
    void Service.Present(
      ++heightReports.current,
      Math.ceil(contentRef.current.getBoundingClientRect().height),
    );
  }, [opened, search]);

  // Keep the window as tall as the content while it changes.
  useEffect(() => {
    const content = contentRef.current;
    if (!content) return;
    const observer = new ResizeObserver(() => {
      void Service.Resize(++heightReports.current, Math.ceil(content.getBoundingClientRect().height));
    });
    observer.observe(content);
    return () => {
      observer.disconnect();
    };
  }, []);

  // Keep the selected row in view as the arrow keys move it.
  useEffect(() => {
    listRef.current?.children[selected]?.scrollIntoView({ block: "nearest" });
  }, [selected]);

  const current = results.at(selected);
  const isPath = current?.kind === "file" || current?.kind === "folder";

  const activate = (r: Result | undefined, reveal: boolean) => {
    if (!r) return;
    if (r.kind === "keyword") {
      const c = findChip(own, r.key);
      if (c) applyChip(c, "");
      inputRef.current?.focus();
      return;
    }
    Service.Activate(r, reveal).catch(console.error);
  };

  const onChange = (value: string) => {
    // A keyword and a space turn into the keyword's tag.
    const m = chip ? null : /^(\S+)\s(.*)$/s.exec(value);
    const c = m?.[1] === undefined ? undefined : findChip(own, m[1]);
    if (c) {
      applyChip(c, m?.[2] ?? "");
      return;
    }
    setQuery(value);
    search(value, chip);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    // Enter that confirms an input method's candidate is not a submit.
    // Safari marks that Enter only with keyCode 229, not with isComposing.
    // eslint-disable-next-line @typescript-eslint/no-deprecated
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    setCopied(false);
    // ⌘C (Ctrl+C) copies a calculation's result, a conversion or a time and
    // keeps the panel open,
    // so it can be pasted back to calculate on. With text selected in the
    // field, it copies that text as usual.
    const field = e.currentTarget;
    const copy = (mac ? e.metaKey : e.ctrlKey) && e.key.toLowerCase() === "c";
    if (copy && current && copyable(current) && field.selectionStart === field.selectionEnd) {
      e.preventDefault();
      Clipboard.SetText(copyText(current)).then(() => {
        setCopied(true);
      }, console.error);
      return;
    }
    if (e.key === "Tab") {
      // Tab takes the keyword offered, or the keyword typed.
      e.preventDefault();
      const typed = chip ? undefined : findChip(own, q);
      if (current?.kind === "keyword") activate(current, false);
      else if (typed) applyChip(typed, "");
      return;
    }
    if (e.key === "Backspace" && chip && field.selectionStart === 0 && field.selectionEnd === 0) {
      // Deleting before the query gives the keyword back as text.
      if (query === "") {
        e.preventDefault();
        applyChip(null, chip.word);
      }
      return;
    }
    const down = e.key === "ArrowDown" || (e.ctrlKey && e.key === "n");
    const up = e.key === "ArrowUp" || (e.ctrlKey && e.key === "p");
    if (down || up) {
      e.preventDefault();
      if (results.length > 0) {
        setSelected((i) => (i + (down ? 1 : -1) + results.length) % results.length);
      }
    } else if (e.key === "Enter") {
      e.preventDefault();
      activate(results[selected], mac ? e.metaKey : e.ctrlKey);
    } else if (e.key === "Escape") {
      e.preventDefault();
      if (query !== "") {
        setQuery("");
        search("", chip);
      } else if (chip) {
        applyChip(null, "");
      } else {
        void Service.Hide();
      }
    }
  };

  return (
    <div ref={contentRef} className="flex flex-col bg-background">
      <div className="flex h-14 shrink-0 items-center gap-3 px-4">
        <Search className="size-5 shrink-0 text-muted-foreground" />
        {chip && <KeywordTag chip={chip} />}
        <input
          ref={inputRef}
          value={query}
          placeholder={chip ? chipPlaceholders[chip.kind] : "搜索应用和文件，输入算式，或用 ! 开头运行命令"}
          spellCheck={false}
          autoComplete="off"
          onChange={(e) => {
            onChange(e.target.value);
          }}
          onKeyDown={onKeyDown}
          className="h-full flex-1 bg-transparent text-lg outline-none placeholder:text-muted-foreground"
        />
      </div>

      {(q !== "" || chip?.kind === "web") && (
        <>
          <div className="border-t" />
          {results.length === 0 ? (
            <p className="px-4 py-3 text-[13px] text-muted-foreground">没有结果</p>
          ) : (
            <div ref={listRef} className="max-h-[396px] overflow-y-auto p-1.5">
              {results.map((r, i) => (
                <Row
                  key={`${r.kind}:${r.key}`}
                  result={r}
                  selected={i === selected}
                  onHover={() => {
                    setSelected(i);
                  }}
                  onClick={(reveal) => {
                    activate(r, reveal);
                  }}
                  mac={mac}
                />
              ))}
            </div>
          )}
          {current && (
            <footer className="flex h-8 shrink-0 items-center gap-4 border-t px-4 text-[11px] text-muted-foreground">
              <span>↑↓ 选择</span>
              {copyable(current) && <span>{copied ? "已复制" : mac ? "⌘C 复制" : "Ctrl+C 复制"}</span>}
              <span>↵ {actionName(current)}</span>
              {isPath && <span>{mac ? "⌘↵ 在访达中显示" : "Ctrl+↵ 在资源管理器中显示"}</span>}
              {current.kind === "translation" && current.key !== "" && (
                <span>{mac ? "⌘↵ 在翻译窗口中打开" : "Ctrl+↵ 在翻译窗口中打开"}</span>
              )}
              {current.kind === "keyword" && <span>Tab 使用</span>}
              <span className="flex-1" />
              <span>Esc {query ? "清空" : chip ? "取消关键词" : "关闭"}</span>
            </footer>
          )}
        </>
      )}
    </div>
  );
}

/** Calculations, conversions, times and translations are values to copy. */
function copyable(r: Result): boolean {
  return ["calc", "currency", "time", "translation"].includes(r.kind) && r.key !== "";
}

/** A translation copies its translated text; the other values their key. */
function copyText(r: Result): string {
  return r.kind === "translation" ? r.title : r.key;
}

const kindIcons: Record<string, LucideIcon> = {
  calc: Calculator,
  currency: Banknote,
  time: Clock,
  translation: Languages,
  web: Globe,
  keyword: Tag,
};

function actionName(r: Result): string {
  if (copyable(r)) return "复制并关闭";
  switch (r.kind) {
    case "command":
      return "运行";
    case "web":
      return r.title ? "在浏览器中搜索" : "在浏览器中打开";
    case "keyword":
      return "使用";
    default:
      return "打开";
  }
}

function Row({
  result,
  selected,
  onHover,
  onClick,
  mac,
}: {
  result: Result;
  selected: boolean;
  onHover: () => void;
  onClick: (reveal: boolean) => void;
  mac: boolean;
}) {
  const translation = result.kind === "translation" && result.key !== "";
  // A website opened without a query says so in its title.
  const home = result.kind === "web" && result.title === "";
  return (
    <button
      type="button"
      onMouseMove={onHover}
      onClick={(e) => {
        onClick(mac ? e.metaKey : e.ctrlKey);
      }}
      className={cn(
        "flex min-h-11 w-full items-center gap-3 rounded-md px-2.5 py-1.5 text-left outline-none",
        selected && "bg-accent text-accent-foreground",
      )}
    >
      <ResultIcon result={result} />
      <span className="min-w-0 flex-1">
        {/* A translation shows a few lines; other titles fit on one. */}
        <span className={cn("block text-[14px]", translation ? "line-clamp-3" : "truncate")}>
          {home ? result.subtitle : result.title}
        </span>
        <span className="block truncate text-[11px] text-muted-foreground">
          {translation
            ? `${languageName(result.source ?? "")} → ${languageName(result.target ?? "")} · ${result.subtitle}`
            : home
              ? "输入内容后按 ↵ 搜索"
              : result.subtitle}
        </span>
      </span>
    </button>
  );
}

function ResultIcon({ result }: { result: Result }) {
  if (result.icon) return <img src={result.icon} alt="" className="size-7 shrink-0" draggable={false} />;
  const Icon = kindIcons[result.kind] ?? SquareTerminal;
  return (
    <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
      <Icon className="size-4" />
    </span>
  );
}

function KeywordTag({ chip }: { chip: Chip }) {
  const Icon = chipIcons[chip.kind];
  return (
    <span className="flex shrink-0 items-center gap-1 rounded-md bg-primary/10 px-2 py-1 text-[13px] font-medium text-primary">
      <Icon className="size-3.5" />
      {chip.name}
    </span>
  );
}
