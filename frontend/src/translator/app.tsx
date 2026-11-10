import { Clipboard, Events } from "@wailsio/runtime";
import { ArrowUpDown, Check, ChevronDown, Copy, LoaderCircle, Pin, Square, Volume2, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
} from "react";

import { Service, type Request, type Settings, type Translation } from "@bindings/modules/translate";
import { errorMessage } from "@/lib/errors";
import { languageName, languages } from "@/modules/translate/languages";
import { useSpeech } from "@/translator/speech";
import { cn } from "cn";

type Result =
  | { kind: "empty" }
  | { kind: "loading" }
  | { kind: "done"; translation: Translation }
  | { kind: "error"; message: string };

/** Lets the area move the frameless window. */
const dragRegion = { "--wails-draggable": "drag" } as CSSProperties;

/** The textarea grows with its text up to this height, then scrolls. */
const maxSourceHeight = 168;

export function Translator() {
  const [request, setRequest] = useState<Request | null>(null);
  // Bumped for every request, so the popup is presented once per open.
  const [opened, setOpened] = useState(0);
  const [text, setText] = useState("");
  const [result, setResult] = useState<Result>({ kind: "empty" });
  const [pinned, setPinned] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const speech = useSpeech();
  const stopSpeech = speech.stop;
  const contentRef = useRef<HTMLDivElement>(null);
  // Numbers the height reports, which can reach Go out of order.
  const heightReports = useRef(0);
  const sourceRef = useRef<HTMLTextAreaElement>(null);
  // Only the latest translation may show; older ones are cancelled.
  const pending = useRef<{ cancel: () => unknown } | null>(null);

  const translate = useCallback((source: string) => {
    pending.current?.cancel();
    pending.current = null;
    if (source.trim() === "") {
      setResult({ kind: "empty" });
      return;
    }
    setResult({ kind: "loading" });
    const call = Service.Translate(source);
    pending.current = call;
    call.then(
      (translation) => {
        if (pending.current !== call) return;
        pending.current = null;
        setResult({ kind: "done", translation });
      },
      (err: unknown) => {
        if (pending.current !== call) return;
        pending.current = null;
        setResult({ kind: "error", message: errorMessage(err) });
      },
    );
  }, []);

  const show = useCallback(
    (req: Request) => {
      stopSpeech();
      setRequest(req);
      setText(req.text);
      setPinned(false);
      setOpened((n) => n + 1);
      translate(req.text);
      // The dashboard may have changed them since the last open.
      Service.State().then((state) => {
        setSettings(state.settings);
      }, console.error);
    },
    [stopSpeech, translate],
  );

  // Listen for requests first, then collect the one that may have arrived
  // while the page was loading.
  useEffect(() => {
    const offOpen = Events.On("translate:open", (event) => {
      show(event.data);
    });
    const offHidden = Events.On("translate:hidden", () => {
      stopSpeech();
      pending.current?.cancel();
      pending.current = null;
    });
    Service.Attach().then((req) => {
      if (req) show(req);
    }, console.error);
    return () => {
      offOpen();
      offHidden();
    };
  }, [show, stopSpeech]);

  // Grow the textarea with its text.
  useLayoutEffect(() => {
    const source = sourceRef.current;
    if (!source) return;
    source.style.height = "auto";
    source.style.height = `${String(Math.min(source.scrollHeight, maxSourceHeight))}px`;
  }, [text, opened]);

  // Once a request is drawn, show the window at the content's height. This
  // runs after the textarea has grown, so the height includes it.
  useLayoutEffect(() => {
    if (opened === 0 || !contentRef.current) return;
    const source = sourceRef.current;
    if (source) {
      source.focus();
      source.setSelectionRange(source.value.length, source.value.length);
    }
    void Service.Present(
      ++heightReports.current,
      Math.ceil(contentRef.current.getBoundingClientRect().height),
    );
  }, [opened]);

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

  const hide = () => {
    void Service.Hide();
  };

  const togglePin = () => {
    void Service.SetPinned(!pinned);
    setPinned(!pinned);
  };

  const changeSettings = (
    patch: Pick<Partial<Settings>, "source" | "target" | "engine">,
    nextText = text,
  ) => {
    if (!settings) return;
    Service.SetSettings({ ...settings, ...patch }).then((state) => {
      setSettings(state.settings);
      translate(nextText);
    }, console.error);
  };

  const translation = result.kind === "done" ? result.translation : null;

  // Swapping turns the translation into the text to translate back. A
  // detected source language swaps as the language that was detected.
  const swapTarget = settings?.source === "auto" ? translation?.source : settings?.source;
  const swap = () => {
    if (!settings || !swapTarget) return;
    const nextText = translation ? translation.text : text;
    setText(nextText);
    changeSettings(
      { source: translation ? translation.target : settings.target, target: swapTarget },
      nextText,
    );
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      hide();
    }
  };

  const onSourceKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter that confirms an input method's candidate is not a submit.
    // Safari marks that Enter only with keyCode 229, not with isComposing.
    // eslint-disable-next-line @typescript-eslint/no-deprecated
    const confirmsCandidate = e.nativeEvent.isComposing || e.keyCode === 229;
    if (e.key === "Enter" && !e.shiftKey && !confirmsCandidate) {
      e.preventDefault();
      translate(text);
    }
  };

  return (
    <div ref={contentRef} onKeyDown={onKeyDown} className="flex flex-col bg-background">
      <header className="flex h-10 shrink-0 items-center gap-1 pr-1.5 pl-3" style={dragRegion}>
        <span className="flex-1 text-xs text-muted-foreground">翻译</span>
        <IconButton
          label={pinned ? "取消固定" : "固定，点击别处时不关闭"}
          active={pinned}
          onClick={togglePin}
        >
          <Pin className={cn(pinned && "fill-current")} />
        </IconButton>
        <IconButton label="关闭（Esc）" onClick={hide}>
          <X />
        </IconButton>
      </header>

      {request?.notice && (
        <div className="mx-2.5 mb-2 flex items-center gap-2 rounded-lg bg-accent px-3 py-2 text-[13px] text-accent-foreground">
          <span className="flex-1">{request.notice}</span>
          {request.permission && (
            <button
              type="button"
              onClick={() => void Service.OpenPermissionSettings(request.permission ?? "")}
              className="shrink-0 font-medium text-primary hover:underline"
            >
              打开系统设置
            </button>
          )}
        </div>
      )}

      <div className="mx-2.5 rounded-lg border bg-card">
        <textarea
          ref={sourceRef}
          value={text}
          rows={1}
          placeholder="输入要翻译的文字，按回车翻译"
          onChange={(e) => {
            setText(e.target.value);
          }}
          onKeyDown={onSourceKeyDown}
          className="block w-full resize-none bg-transparent px-3 pt-2.5 text-[15px] leading-relaxed outline-none select-text placeholder:text-muted-foreground"
        />
        <div className="flex h-9 items-center gap-0.5 pr-1.5 pl-1.5">
          {settings && (
            <LanguagePicker
              label="原文语言"
              allowAuto
              value={settings.source}
              detected={settings.source === "auto" ? translation?.source : undefined}
              onChange={(source) => {
                changeSettings({ source });
              }}
            />
          )}
          <span className="flex-1" />
          {translation && (
            <SpeakButton
              speaking={speech.speaking === "source"}
              onClick={() => {
                speech.toggle("source", text, translation.source);
              }}
            />
          )}
        </div>
      </div>

      <div className="relative z-10 h-2">
        <button
          type="button"
          title={swapTarget ? "互换语言" : "自动检测出原文语言后才能互换"}
          aria-label="互换语言"
          disabled={!swapTarget}
          onClick={swap}
          className="absolute top-1/2 left-1/2 flex size-7 -translate-1/2 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-xs transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-40 [&_svg]:size-3.5"
        >
          <ArrowUpDown />
        </button>
      </div>

      <section className="mx-2.5 rounded-lg border bg-card">
        <div className="flex h-9 items-center gap-2 px-1.5">
          {settings && (
            <LanguagePicker
              label="译文语言"
              value={translation?.target ?? settings.target}
              onChange={(target) => {
                changeSettings({ target });
              }}
            />
          )}
          <span className="flex-1" />
          {result.kind === "loading" && (
            <LoaderCircle className="size-3.5 animate-spin text-muted-foreground" />
          )}
          {settings &&
            (settings.deeplKey === "" ? (
              <span className="px-1.5 text-xs text-muted-foreground">Google 翻译</span>
            ) : (
              <Picker
                label="翻译服务"
                value={settings.engine}
                options={engines}
                onChange={(engine) => {
                  changeSettings({ engine });
                }}
              />
            ))}
        </div>
        {result.kind === "error" && (
          <p className="px-3 pb-3 text-[13px] text-destructive">{result.message}</p>
        )}
        {result.kind !== "error" && !translation && (
          <p className="px-3 pb-3 text-[15px] leading-relaxed text-muted-foreground/60">译文</p>
        )}
        {translation && (
          <>
            <p className="max-h-80 overflow-y-auto px-3 text-[15px] leading-relaxed whitespace-pre-wrap select-text">
              {translation.text}
            </p>
            <div className="flex h-8 items-center justify-end gap-0.5 px-1.5">
              <SpeakButton
                speaking={speech.speaking === "result"}
                onClick={() => {
                  speech.toggle("result", translation.text, translation.target);
                }}
              />
              <CopyButton text={translation.text} />
            </div>
          </>
        )}
      </section>
      <div className="h-2.5 shrink-0" />
    </div>
  );
}

const noDrag = { "--wails-draggable": "no-drag" } as CSSProperties;

const engines = [
  { value: "google", label: "Google 翻译" },
  { value: "deepl", label: "DeepL" },
];

/**
 * A language menu. The source picker offers automatic detection and then
 * names the language it detected.
 */
function LanguagePicker({
  label,
  allowAuto,
  value,
  detected,
  onChange,
}: {
  label: string;
  /** Offers automatic detection, for the source language. */
  allowAuto?: boolean;
  value: string;
  /** The detected language, named next to the automatic choice. */
  detected?: string;
  onChange: (code: string) => void;
}) {
  const options: { value: string; label: string }[] = languages.map((l) => ({
    value: l.code,
    label: l.name,
  }));
  if (value !== "auto" && !languages.some((l) => l.code === value)) {
    options.unshift({ value, label: languageName(value) });
  }
  if (allowAuto) {
    options.unshift({
      value: "auto",
      label: detected ? `自动检测（${languageName(detected)}）` : "自动检测",
    });
  }
  return <Picker label={label} value={value} options={options} onChange={onChange} />;
}

/** A menu drawn as plain text. */
function Picker({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <label className="relative flex h-7 items-center rounded-md text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground has-focus-visible:ring-2 has-focus-visible:ring-ring">
      <select
        aria-label={label}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
        className="h-full appearance-none bg-transparent pr-6 pl-1.5 outline-none"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-1.5 size-3" />
    </label>
  );
}

function IconButton({
  label,
  active,
  onClick,
  children,
}: {
  label: string;
  active?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={active}
      onClick={onClick}
      style={noDrag}
      className={cn(
        "flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-3.5",
        active && "text-primary hover:text-primary",
      )}
    >
      {children}
    </button>
  );
}

function SpeakButton({ speaking, onClick }: { speaking: boolean; onClick: () => void }) {
  return (
    <IconButton label={speaking ? "停止朗读" : "朗读"} active={speaking} onClick={onClick}>
      {speaking ? <Square className="fill-current" /> : <Volume2 />}
    </IconButton>
  );
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => {
      setCopied(false);
    }, 1200);
    return () => {
      clearTimeout(timer);
    };
  }, [copied]);

  return (
    <IconButton
      label="复制译文"
      active={copied}
      onClick={() => {
        Clipboard.SetText(text).then(() => {
          setCopied(true);
        }, console.error);
      }}
    >
      {copied ? <Check /> : <Copy />}
    </IconButton>
  );
}
