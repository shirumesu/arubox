import { useCallback, useEffect, useRef, useState } from "react";

import { Service } from "@bindings/modules/translate";
import { errorMessage } from "@/lib/errors";

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
 * Reads text aloud, with the system's voices or the online voices of Edge.
 * Each text has a key, so the button that started it can show it is
 * speaking and stop it again. A reading that fails leaves its message in
 * `error` until the next one starts.
 */
export function useSpeech() {
  const [speaking, setSpeaking] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // The Edge voice is fetched first, then played. Only the latest may play.
  const pending = useRef<{ cancel: () => unknown } | null>(null);
  const audio = useRef<HTMLAudioElement | null>(null);

  // The voice list loads asynchronously; asking for it starts the load.
  useEffect(() => {
    speechSynthesis.getVoices();
  }, []);

  const halt = useCallback(() => {
    speechSynthesis.cancel();
    pending.current?.cancel();
    pending.current = null;
    audio.current?.pause();
    audio.current = null;
  }, []);

  useEffect(() => halt, [halt]);

  const stop = useCallback(() => {
    halt();
    setSpeaking(null);
  }, [halt]);

  const toggle = useCallback(
    (key: string, text: string, lang: string, voice: string) => {
      halt();
      setError(null);
      if (speaking === key) {
        setSpeaking(null);
        return;
      }
      const done = () => {
        setSpeaking((current) => (current === key ? null : current));
      };
      setSpeaking(key);

      if (voice === "edge") {
        const call = Service.Speak(text, lang);
        pending.current = call;
        const fail = (message: string) => {
          setError(message);
          done();
        };
        call.then(
          (mp3) => {
            if (pending.current !== call) return;
            pending.current = null;
            if (mp3 === null) {
              fail("没有收到 Edge 在线朗读的声音");
              return;
            }
            const player = new Audio(`data:audio/mpeg;base64,${mp3}`);
            audio.current = player;
            player.onended = () => {
              audio.current = null;
              done();
            };
            player.onerror = () => {
              if (audio.current !== player) return;
              audio.current = null;
              fail("无法播放 Edge 在线朗读的声音");
            };
            player.play().catch(() => {
              // Stopping before the sound starts rejects it too.
              if (audio.current !== player) return;
              audio.current = null;
              fail("无法播放 Edge 在线朗读的声音");
            });
          },
          (err: unknown) => {
            if (pending.current !== call) return;
            pending.current = null;
            fail(errorMessage(err));
          },
        );
        return;
      }

      const utterance = new SpeechSynthesisUtterance(text);
      utterance.lang = lang;
      const systemVoice = voiceFor(lang);
      if (systemVoice) utterance.voice = systemVoice;
      utterance.onend = done;
      utterance.onerror = done;
      speechSynthesis.speak(utterance);
    },
    [speaking, halt],
  );

  return { speaking, error, toggle, stop };
}
