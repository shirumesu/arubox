import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { DashboardService, type State } from "@bindings/core";
import { errorMessage } from "@/lib/errors";

export type { State };
export type ModuleStatus = NonNullable<State["modules"]>[number];
export type HotkeyStatus = NonNullable<State["hotkeys"]>[number];

/**
 * Loads the dashboard state and applies mutations. Every mutation returns the
 * new state; when one fails its error is shown and the state reloaded.
 */
export function useDashboard() {
  const [state, setState] = useState<State | null>(null);

  const reload = useCallback(async () => {
    try {
      setState(await DashboardService.State());
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    DashboardService.State().then(setState, (err: unknown) => toast.error(errorMessage(err)));
  }, []);

  const mutate = useCallback(
    async (call: () => Promise<State>): Promise<boolean> => {
      try {
        setState(await call());
        return true;
      } catch (err) {
        toast.error(errorMessage(err));
        await reload();
        return false;
      }
    },
    [reload],
  );

  return { state, mutate };
}

export type Mutate = ReturnType<typeof useDashboard>["mutate"];
