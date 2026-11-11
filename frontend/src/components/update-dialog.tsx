import { Loader2 } from "lucide-react";

import { Phase, Service as UpdateService, type State as UpdateState } from "@bindings/update";
import { ReleaseNotes } from "@/components/release-notes";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";

/** Shows the release on offer, installs it once confirmed and follows the installation. */
export function UpdateDialog({
  state,
  open,
  onOpenChange,
}: {
  state: UpdateState;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const release = state.release;
  if (!release) return null;
  const working = [Phase.PhaseDownloading, Phase.PhaseInstalling, Phase.PhaseRestarting].includes(
    state.phase,
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>arubox {release.version}</DialogTitle>
          <DialogDescription>正在使用 {state.version}</DialogDescription>
        </DialogHeader>

        {release.notes && (
          <div className="max-h-80 overflow-y-auto rounded-md border bg-card px-4 py-3">
            <ReleaseNotes notes={release.notes} />
          </div>
        )}

        {state.elevation && (
          <p className="text-[13px] text-muted-foreground">
            文件索引服务会一起更新，安装时会请求管理员授权。
          </p>
        )}

        {state.phase === Phase.PhaseDownloading && (
          <div className="space-y-1.5">
            <Progress value={(state.received / state.total) * 100} />
            <p className="text-[13px] text-muted-foreground tabular-nums">
              正在下载 {megabytes(state.received)} / {megabytes(state.total)} MB
            </p>
          </div>
        )}

        {state.error && <p className="text-[13px] text-destructive">{state.error}</p>}

        <DialogFooter>
          {working ? (
            <Button disabled>
              <Loader2 className="animate-spin" />
              {state.phase === Phase.PhaseRestarting
                ? "正在重启"
                : state.phase === Phase.PhaseInstalling
                  ? "正在安装"
                  : "正在下载"}
            </Button>
          ) : (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  onOpenChange(false);
                }}
              >
                稍后
              </Button>
              <Button
                disabled={state.phase !== Phase.PhaseAvailable}
                onClick={() => {
                  void UpdateService.Install();
                }}
              >
                更新并重启
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function megabytes(bytes: number): string {
  return (bytes / (1 << 20)).toFixed(1);
}
