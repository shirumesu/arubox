import { Events } from "@wailsio/runtime";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";

import { Service as UpdateService, type State as UpdateState } from "@bindings/update";
import { UpdateDialog } from "@/components/update-dialog";
import { errorMessage } from "@/lib/errors";

export type { UpdateState };

interface Update {
  state: UpdateState;
  /** Opens the dialog of the release on offer. */
  show: () => void;
}

const UpdateContext = createContext<Update | null>(null);

/** The tray's update item opens a new dashboard at this address. */
const showHash = "#update";

/**
 * Follows the updater's state, which arrives as update:state events, and
 * holds the update dialog, which the tray can open on any page.
 */
export function UpdateProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<UpdateState | null>(null);
  const [open, setOpen] = useState(() => window.location.hash === showHash);

  useEffect(() => {
    if (window.location.hash === showHash) history.replaceState(null, "", window.location.pathname);
    const offState = Events.On("update:state", (event) => {
      setState(event.data);
    });
    const offShow = Events.On("update:show", () => {
      setOpen(true);
    });
    // An event that arrived meanwhile is newer than this answer.
    UpdateService.State().then(
      (s) => {
        setState((current) => current ?? s);
      },
      (err: unknown) => toast.error(errorMessage(err)),
    );
    return () => {
      offState();
      offShow();
    };
  }, []);

  if (!state) return children;
  return (
    <UpdateContext
      value={{
        state,
        show: () => {
          setOpen(true);
        },
      }}
    >
      {children}
      <UpdateDialog state={state} open={open && !!state.release} onOpenChange={setOpen} />
    </UpdateContext>
  );
}

/** The updater's state, or null until it has loaded. */
export function useUpdate(): Update | null {
  return useContext(UpdateContext);
}
