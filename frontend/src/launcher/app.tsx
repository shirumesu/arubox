import { Clipboard, Events } from "@wailsio/runtime";
import { Calculator, Search, SquareTerminal } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";

import { Service, type Result } from "@bindings/modules/launcher";
import { cn } from "cn";

/** Waits this long after typing stops before searching files, which is slower. */
const fileDelay = 120;

export function Launcher() {
  const [query, setQuery] = useState("");
  const [instant, setInstant] = useState<Result[]>([]);
  const [files, setFiles] = useState<Result[]>([]);
  const [selected, setSelected] = useState(0);
  const [mac, setMac] = useState(true);
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
  const fileSearch = useRef<{ timer?: ReturnType<typeof setTimeout>; call?: { cancel: () => unknown } }>({});

  const results = [...instant, ...files];

  const search = useCallback((q: string) => {
    const n = ++searches.current;
    clearTimeout(fileSearch.current.timer);
    fileSearch.current.call?.cancel();
    fileSearch.current = {};
    Service.Search(q).then((r) => {
      if (n !== searches.current) return;
      setInstant(r ?? []);
      setFiles([]);
      setSelected(0);
    }, console.error);
    fileSearch.current.timer = setTimeout(() => {
      const call = Service.SearchFiles(q);
      fileSearch.current.call = call;
      call.then((r) => {
        if (n === searches.current) setFiles(r ?? []);
      }, console.error);
    }, fileDelay);
  }, []);

  useEffect(() => {
    const off = Events.On("launcher:open", () => {
      setOpened((n) => n + 1);
    });
    Service.Attach().then((pending) => {
      if (pending) setOpened((n) => n + 1);
    }, console.error);
    Service.State().then((s) => {
      setMac(s.platform === "darwin");
    }, console.error);
    return off;
  }, []);

  // On every open: select the last query, so typing replaces it, refresh its
  // results and show the panel.
  useLayoutEffect(() => {
    if (opened === 0 || !contentRef.current) return;
    inputRef.current?.focus();
    inputRef.current?.select();
    search(inputRef.current?.value ?? "");
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
    Service.Activate(r, reveal).catch(console.error);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    // Enter that confirms an input method's candidate is not a submit.
    // Safari marks that Enter only with keyCode 229, not with isComposing.
    // eslint-disable-next-line @typescript-eslint/no-deprecated
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    setCopied(false);
    // ⌘C (Ctrl+C) copies a calculation's result and keeps the panel open,
    // so it can be pasted back to calculate on. With text selected in the
    // field, it copies that text as usual.
    const field = e.currentTarget;
    const copy = (mac ? e.metaKey : e.ctrlKey) && e.key.toLowerCase() === "c";
    if (copy && current?.kind === "calc" && field.selectionStart === field.selectionEnd) {
      e.preventDefault();
      Clipboard.SetText(current.key).then(() => {
        setCopied(true);
      }, console.error);
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
        search("");
      } else {
        void Service.Hide();
      }
    }
  };

  return (
    <div ref={contentRef} className="flex flex-col bg-background">
      <div className="flex h-14 shrink-0 items-center gap-3 px-4">
        <Search className="size-5 shrink-0 text-muted-foreground" />
        <input
          ref={inputRef}
          value={query}
          placeholder="搜索应用和文件，输入算式，或用 ! 开头运行命令"
          spellCheck={false}
          autoComplete="off"
          onChange={(e) => {
            setQuery(e.target.value);
            search(e.target.value);
          }}
          onKeyDown={onKeyDown}
          className="h-full flex-1 bg-transparent text-lg outline-none placeholder:text-muted-foreground"
        />
      </div>

      {query.trim() !== "" && (
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
              {current.kind === "calc" && <span>{copied ? "已复制" : mac ? "⌘C 复制" : "Ctrl+C 复制"}</span>}
              <span>↵ {actionName(current)}</span>
              {isPath && <span>{mac ? "⌘↵ 在访达中显示" : "Ctrl+↵ 在资源管理器中显示"}</span>}
              <span className="flex-1" />
              <span>Esc {query ? "清空" : "关闭"}</span>
            </footer>
          )}
        </>
      )}
    </div>
  );
}

function actionName(r: Result): string {
  switch (r.kind) {
    case "calc":
      return "复制并关闭";
    case "command":
      return "运行";
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
  return (
    <button
      type="button"
      onMouseMove={onHover}
      onClick={(e) => {
        onClick(mac ? e.metaKey : e.ctrlKey);
      }}
      className={cn(
        "flex h-11 w-full items-center gap-3 rounded-md px-2.5 text-left outline-none",
        selected && "bg-accent text-accent-foreground",
      )}
    >
      <ResultIcon result={result} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[14px]">{result.title}</span>
        <span className="block truncate text-[11px] text-muted-foreground">{result.subtitle}</span>
      </span>
    </button>
  );
}

function ResultIcon({ result }: { result: Result }) {
  if (result.icon) return <img src={result.icon} alt="" className="size-7 shrink-0" draggable={false} />;
  const Icon = result.kind === "calc" ? Calculator : SquareTerminal;
  return (
    <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
      <Icon className="size-4" />
    </span>
  );
}
