/** The languages offered as translation targets, by Google language code. */
export const languages = [
  { code: "zh-CN", name: "简体中文" },
  { code: "zh-TW", name: "繁体中文" },
  { code: "en", name: "英语" },
  { code: "ja", name: "日语" },
  { code: "ko", name: "韩语" },
  { code: "fr", name: "法语" },
  { code: "de", name: "德语" },
  { code: "es", name: "西班牙语" },
  { code: "it", name: "意大利语" },
  { code: "pt", name: "葡萄牙语" },
  { code: "ru", name: "俄语" },
  { code: "uk", name: "乌克兰语" },
  { code: "pl", name: "波兰语" },
  { code: "nl", name: "荷兰语" },
  { code: "tr", name: "土耳其语" },
  { code: "ar", name: "阿拉伯语" },
  { code: "th", name: "泰语" },
  { code: "vi", name: "越南语" },
  { code: "id", name: "印尼语" },
  { code: "ms", name: "马来语" },
] as const;

/** Names a language code, including detected ones outside the list. */
export function languageName(code: string): string {
  const exact = languages.find((l) => l.code.toLowerCase() === code.toLowerCase());
  if (exact) return exact.name;
  if (code.toLowerCase() === "zh") return "中文";
  return code.toUpperCase();
}
