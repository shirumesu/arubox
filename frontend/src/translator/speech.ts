import { useCallback, useEffect, useState } from "react";

/** Picks a system voice for a language code such as "en" or "zh-CN". */
function voiceFor(lang: string): SpeechSynthesisVoice | undefined {
  const voices = speechSynthesis.getVoices();
  const wanted = lang.toLowerCase();
  const base = wanted.split("-")[0] ?? wanted;
  return (
    voices.find((v) => v.lang.toLowerCase() === wanted && v.localService) ??
    voices.find((v) => v.lang.toLowerCase() === wanted) ??
    voices.find((v) => v.lang.toLowerCase().split("-")[0] === base)
  );
}

/**
 * Reads text aloud with the system's voices. Each text has a key, so the
 * button that started it can show it is speaking and stop it again.
 */
export function useSpeech() {
  const [speaking, setSpeaking] = useState<string | null>(null);

  // The voice list loads asynchronously; asking for it starts the load.
  useEffect(() => {
    speechSynthesis.getVoices();
    return () => {
      speechSynthesis.cancel();
    };
  }, []);

  const stop = useCallback(() => {
    speechSynthesis.cancel();
    setSpeaking(null);
  }, []);

  const toggle = useCallback(
    (key: string, text: string, lang: string) => {
      speechSynthesis.cancel();
      if (speaking === key) {
        setSpeaking(null);
        return;
      }
      const utterance = new SpeechSynthesisUtterance(text);
      utterance.lang = lang;
      const voice = voiceFor(lang);
      if (voice) utterance.voice = voice;
      const done = () => {
        setSpeaking((current) => (current === key ? null : current));
      };
      utterance.onend = done;
      utterance.onerror = done;
      setSpeaking(key);
      speechSynthesis.speak(utterance);
    },
    [speaking],
  );

  return { speaking, toggle, stop };
}
