// Accelerators use the Go side's canonical syntax: modifiers in the order
// Ctrl, Alt, Shift, Super, then one key, joined by "+". Super is Command on
// macOS and the Windows key on Windows.

export type Platform = "darwin" | "windows";

const MODIFIERS = ["Ctrl", "Alt", "Shift", "Super"] as const;
export type Modifier = (typeof MODIFIERS)[number];

const NAMED_CODES: Record<string, string> = {
  Minus: "-",
  Equal: "=",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Slash: "/",
  Backquote: "`",
  Space: "space",
  Enter: "return",
  NumpadEnter: "return",
  Tab: "tab",
  Backspace: "backspace",
  Delete: "delete",
  Escape: "escape",
  Home: "home",
  End: "end",
  PageUp: "page up",
  PageDown: "page down",
  ArrowLeft: "left",
  ArrowRight: "right",
  ArrowUp: "up",
  ArrowDown: "down",
};

export function modifiersOf(e: KeyboardEvent): Modifier[] {
  const held: Record<Modifier, boolean> = {
    Ctrl: e.ctrlKey,
    Alt: e.altKey,
    Shift: e.shiftKey,
    Super: e.metaKey,
  };
  return MODIFIERS.filter((m) => held[m]);
}

/** The non-modifier key of a key event, or null for modifier keys and keys a hotkey cannot use. */
export function keyOf(e: KeyboardEvent): string | null {
  // Letters follow the active keyboard layout, as the system registers them;
  // with Option held macOS reports a symbol instead, so fall back to position.
  if (/^[a-z]$/i.test(e.key)) return e.key.toLowerCase();
  const { code } = e;
  if (/^Key[A-Z]$/.test(code)) return code.slice(3).toLowerCase();
  if (/^Digit\d$/.test(code)) return code.slice(5);
  if (/^F([1-9]|1\d|20)$/.test(code)) return code.toLowerCase();
  return NAMED_CODES[code] ?? null;
}

const MAC_MODIFIERS: Record<Modifier, string> = { Ctrl: "⌃", Alt: "⌥", Shift: "⇧", Super: "⌘" };
const WIN_MODIFIERS: Record<Modifier, string> = { Ctrl: "Ctrl", Alt: "Alt", Shift: "Shift", Super: "Win" };

const MAC_KEYS: Record<string, string> = {
  space: "Space",
  return: "↩",
  tab: "⇥",
  backspace: "⌫",
  delete: "⌦",
  escape: "Esc",
  home: "↖",
  end: "↘",
  "page up": "⇞",
  "page down": "⇟",
  left: "←",
  right: "→",
  up: "↑",
  down: "↓",
};
const WIN_KEYS: Record<string, string> = {
  space: "Space",
  return: "Enter",
  tab: "Tab",
  backspace: "Backspace",
  delete: "Delete",
  escape: "Esc",
  home: "Home",
  end: "End",
  "page up": "PgUp",
  "page down": "PgDn",
  left: "←",
  right: "→",
  up: "↑",
  down: "↓",
};

/** Splits an accelerator into the labels shown on individual keycaps. */
export function keycaps(accelerator: string, platform: Platform): string[] {
  const parts = accelerator.split("+");
  const key = parts.pop() ?? "";
  return [...parts.map((m) => formatModifier(m as Modifier, platform)), formatKey(key, platform)];
}

export function formatModifier(m: Modifier, platform: Platform): string {
  return (platform === "darwin" ? MAC_MODIFIERS : WIN_MODIFIERS)[m];
}

function formatKey(key: string, platform: Platform): string {
  return (platform === "darwin" ? MAC_KEYS : WIN_KEYS)[key] ?? key.toUpperCase();
}
