import { Blocks, Keyboard, Settings2, type LucideIcon } from "lucide-react";
import { useState, type CSSProperties } from "react";

import appIcon from "../../build/icons/appicon.svg";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useDashboard } from "@/lib/dashboard";
import { GeneralPage } from "@/pages/general";
import { HotkeysPage } from "@/pages/hotkeys";
import { ModulesPage } from "@/pages/modules";
import { cn } from "cn";

const pages = [
  { id: "general", title: "通用", icon: Settings2, Page: GeneralPage },
  { id: "modules", title: "模块", icon: Blocks, Page: ModulesPage },
  { id: "hotkeys", title: "快捷键", icon: Keyboard, Page: HotkeysPage },
] as const satisfies readonly { id: string; title: string; icon: LucideIcon; Page: unknown }[];

type Page = (typeof pages)[number];

/** Lets the area act as the window's title bar on macOS, where the system one is hidden. */
const dragRegion = { "--wails-draggable": "drag" } as CSSProperties;

export function App() {
  const { state, mutate } = useDashboard();
  const [page, setPage] = useState<Page>(pages[0]);

  if (!state) return null;
  // macOS draws the traffic lights over the content; leave room for them.
  const titleBar = state.platform === "darwin" ? "h-13" : "h-4";

  return (
    <TooltipProvider delayDuration={400}>
      <div className="flex h-full">
        <aside className="flex w-52 shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground">
          <div className={titleBar} style={dragRegion} />
          <div className="flex items-center gap-2.5 px-4 pb-4">
            <img src={appIcon} alt="" className="-m-1 size-8" />
            <span className="text-[15px] font-semibold text-foreground">arubox</span>
          </div>
          <nav className="flex flex-col gap-0.5 px-2">
            {pages.map((p) => (
              <NavItem
                key={p.id}
                icon={p.icon}
                title={p.title}
                active={p.id === page.id}
                onClick={() => {
                  setPage(p);
                }}
              />
            ))}
          </nav>
        </aside>

        <main className="flex min-w-0 flex-1 flex-col">
          <header className={cn("shrink-0", titleBar)} style={dragRegion} />
          <div className="flex-1 overflow-y-auto">
            <div className="mx-auto max-w-2xl space-y-6 px-8 pb-10">
              <h1 className="text-xl font-semibold">{page.title}</h1>
              <page.Page state={state} mutate={mutate} />
            </div>
          </div>
        </main>
      </div>
      <Toaster position="bottom-center" />
    </TooltipProvider>
  );
}

function NavItem({
  icon: Icon,
  title,
  active,
  onClick,
}: {
  icon: LucideIcon;
  title: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex h-8 items-center gap-2.5 rounded-md px-2.5 text-left transition-colors",
        active
          ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
          : "hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
      )}
    >
      <Icon className={cn("size-4", active && "text-primary")} />
      {title}
    </button>
  );
}
