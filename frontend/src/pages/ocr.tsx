import { Download, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { Service, type Settings, type State } from "@bindings/ocr";
import { KeepReadySelect, Row, Section } from "@/components/settings";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { errorMessage } from "@/lib/errors";

/** How often the page asks for progress while a download runs. */
const pollMs = 500;

/** Bytes as megabytes, the way the model sites state them. */
function megabytes(bytes: number) {
  return `${(bytes / 1e6).toFixed(1)} MB`;
}

/** The OCR engine shared by screenshot translation and the screenshot module. */
export function OCRPage() {
  const [own, setOwn] = useState<State | null>(null);

  const call = useCallback((fn: () => Promise<State>) => {
    fn().then(setOwn, (err: unknown) => {
      toast.error(errorMessage(err));
      Service.State().then(setOwn, (e: unknown) => toast.error(errorMessage(e)));
    });
  }, []);

  useEffect(() => {
    call(() => Service.State());
  }, [call]);

  const running = own?.download?.running === true;
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => {
      call(() => Service.State());
    }, pollMs);
    return () => {
      clearInterval(timer);
    };
  }, [running, call]);

  if (!own) return null;

  const save = (patch: Partial<Settings>) => {
    call(() => Service.SetSettings({ ...own.settings, ...patch }));
  };
  const system = own.platform === "darwin" ? "系统（Vision）" : "系统（Windows OCR）";

  return (
    <>
      <Section title="识别引擎">
        <Row title="识别引擎" description="截图翻译和截图里的“识别文字”都用它。下载 PP-OCRv6 后才能选它。">
          <Select
            value={own.settings.engine}
            onValueChange={(engine) => {
              save({ engine });
            }}
          >
            <SelectTrigger size="sm" className="w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="system">{system}</SelectItem>
              <SelectItem value="ppocrv6" disabled={!own.installed}>
                PP-OCRv6
              </SelectItem>
            </SelectContent>
          </Select>
        </Row>
      </Section>

      <Section title="PP-OCRv6">
        {own.supported ? (
          <ModelRow own={own} call={call} />
        ) : (
          <Row
            title="PP-OCRv6 small"
            description="ONNX Runtime 没有适用于这台电脑的版本，无法使用 PP-OCRv6。"
          />
        )}
        {own.supported && <FileRows own={own} />}
        {own.supported && (
          <Row
            title="用完后保持加载"
            description="PP-OCRv6 在单独的进程里运行。用过后保留这么久，期间随用随识别；之后结束进程，释放它占用的内存（约 50–150 MB），下次识别时重新加载。"
          >
            {own.loaded && <Badge variant="secondary">已加载</Badge>}
            <KeepReadySelect
              minutes={own.settings.keepMinutes}
              onChange={(keepMinutes) => {
                save({ keepMinutes });
              }}
            />
          </Row>
        )}
      </Section>
    </>
  );
}

type Call = (fn: () => Promise<State>) => void;

/** The model as a whole: what it reads, and download or delete. */
function ModelRow({ own, call }: { own: State; call: Call }) {
  const files = own.files ?? [];
  const download = files.reduce((sum, f) => sum + f.downloadSize, 0);
  const disk = files.reduce((sum, f) => sum + f.size, 0);
  const progress = own.download;
  const running = progress?.running === true;

  let description = `百度飞桨的开源文字识别模型，一个模型识别简体中文、繁体中文、英文、日文和拉丁字母语言，不需要系统语言包，不识别韩文。下载 ${megabytes(download)}，占用磁盘 ${megabytes(disk)}。`;
  if (own.platform === "windows") {
    description += "需要系统装有 Microsoft Visual C++ 运行库。";
  }

  return (
    <div className="space-y-3 px-4 py-3">
      <div className="flex min-h-8 items-center gap-4">
        <div className="min-w-0 flex-1 space-y-0.5">
          <div className="flex items-center gap-2 font-medium">
            PP-OCRv6 small
            {own.installed && <Badge variant="secondary">已下载</Badge>}
          </div>
          <p className="text-[13px] text-muted-foreground">{description}</p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {running && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                call(() => Service.CancelDownload());
              }}
            >
              <X />
              取消
            </Button>
          )}
          {!running && !own.installed && (
            <Button
              size="sm"
              onClick={() => {
                call(() => Service.Download());
              }}
            >
              <Download />
              下载
            </Button>
          )}
          {!running && own.installed && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                call(() => Service.Delete());
              }}
            >
              <Trash2 />
              删除
            </Button>
          )}
        </div>
      </div>

      {running && (
        <div className="space-y-1.5">
          <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
            <div
              className="h-full rounded-full bg-primary transition-[width]"
              style={{ width: `${progress.total > 0 ? (progress.done / progress.total) * 100 : 0}%` }}
            />
          </div>
          <p className="text-[13px] text-muted-foreground tabular-nums">
            正在从 {progress.source} 下载{progress.file}，{megabytes(progress.done)} /{" "}
            {megabytes(progress.total)}
          </p>
        </div>
      )}

      {progress?.failures?.map((f, i) => (
        <p key={i} className="text-[13px] text-muted-foreground">
          {f.file}：从 {f.source} 下载失败（{f.error}）
        </p>
      ))}
      {progress?.error && <p className="text-[13px] text-destructive">{progress.error}</p>}
    </div>
  );
}

/** The files the model needs, with their licence and where they come from, in the order tried. */
function FileRows({ own }: { own: State }) {
  return (
    <>
      {(own.files ?? []).map((f) => (
        <Row
          key={f.label}
          title={<span className="text-[13px] font-normal">{f.label}</span>}
          description={`${f.license} · 来源依次为 ${(f.sources ?? []).map((s) => s.name).join(" → ")}`}
        >
          <span className="text-[13px] text-muted-foreground tabular-nums">{megabytes(f.downloadSize)}</span>
        </Row>
      ))}
    </>
  );
}
